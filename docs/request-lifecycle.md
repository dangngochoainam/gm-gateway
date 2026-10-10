# Vòng đời một request qua gm-gateway

Mô tả một request HTTP đi từ lúc tới socket cho đến khi response rời gateway: qua những tầng
nào, mỗi tầng làm gì, và request có thể dừng ở đâu. Viết ngày 2026-09-13, khớp với
`cmd/server/main.go` tại thời điểm đó. Đổi thứ tự middleware hay thêm tầng thì sửa file này.

## Đọc trước: `http.Handler`

Đó là một interface chỉ có một method:

```go
type Handler interface {
    ServeHTTP(w http.ResponseWriter, r *http.Request)
}
```

Nghĩa của nó: "đưa tôi một request, tôi sẽ viết response". Bất kỳ kiểu nào có method `ServeHTTP`
đều là một `Handler`.

Mục đích: mọi thành phần HTTP cùng nói một ngôn ngữ. `http.Server` chỉ biết đúng một thứ là
`Handler` của nó:

```go
httpServer := &http.Server{Handler: root}
```

Mỗi request đến, server gọi `root.ServeHTTP(w, r)` và không cần biết `root` là gì.

Ví dụ xuyên suốt: `GET /v1/products?limit=10` kèm header `X-Request-ID: abc`.

## Ba tầng

| Tầng | Code | Việc | Không làm |
|---|---|---|---|
| ① Socket | `net.Listen` + `http.Server` | Mở port, accept kết nối, đọc request trong giới hạn timeout/size | Không biết route |
| ② Phân luồng | `root` = `http.NewServeMux()` | Tách probe khỏi traffic API | Không dịch, không có chính sách |
| ③ Dịch | `mux` = `gatewaymux.New()` (`runtime.ServeMux`) | REST → protobuf → gRPC → JSON | Không biết probe, không log access |

Giữa ② và ③ là middleware chain: chính sách chung áp cho mọi API.

```
client
  │ TCP
  ▼
① net.Listen(:50051) ─► http.Server.Serve ─► 1 goroutine / kết nối
  │ root.ServeHTTP
  ▼
② root (http.ServeMux)
  ├── /healthz, /readyz ─────────────► health handler ─► trả luôn (không log, không middleware)
  └── /  (mọi path khác)
        │
        ▼
     middleware chain (ngoài → trong)
     RequestID → AccessLog → Recovery → StripIdentityHeaders → MaxBodyBytes → Timeout → CORS
        │
        ▼
③ mux (runtime.ServeMux)
     khớp route → annotate context → dựng protobuf → gọi gRPC → marshal JSON / errorHandler
        │ gRPC (productConn)
        ▼
     gm-product
```

## Vì sao tách ba tầng

**`net.Listen` tách khỏi `Serve`.** `Serve` phải chạy trong goroutine vì `main` còn chờ SIGTERM.
Nếu dùng `go httpServer.ListenAndServe()` thì `main` chạy tới `readiness.SetReady()` trước khi
port chắc chắn đã mở, và lỗi bind nổ trong goroutine. Bind đồng bộ trong `main` đảm bảo port lỗi
thì process chết ngay, và `/readyz` chỉ trả 200 khi socket đã mở. `listenerHttp.Addr()` còn cho địa
chỉ thật, hữu ích khi `HTTP_PORT=0`.

**`root` tách khỏi `mux`.** Middleware chain bọc quanh `mux`, nên mọi thứ đăng ký trên `mux` đều đi
qua chain. Probe được kubelet gọi vài giây một lần: qua `AccessLog` thì ngập log, và probe không
cần CORS, body limit hay timeout. Đặt probe trên `root`, ngoài chain, là cách tách rẻ nhất — không
middleware nào phải biết path của probe.

## Chi tiết từng bước

### ① Socket — `http.Server`

1. `httpServer.Serve(listenerHttp)` lặp `Accept()`; mỗi kết nối TCP chạy trong goroutine riêng.
2. Đọc header trong `server.read_header_timeout` (5s), tối đa `server.max_header_bytes` (8KB).
3. Gọi `root.ServeHTTP(w, r)`.
4. Sau response, kết nối keep-alive được giữ tối đa `server.idle_timeout` (60s) chờ request kế.

Request bị chặn ở tầng này (header quá lớn, đọc quá chậm) **không tới code của gateway**, nên
không có access log và không có request ID.

### ② Phân luồng — `root`

- `/healthz` → luôn 200 `ok`.
- `/readyz` → 200 `ready` sau `readiness.SetReady()`, 503 `not ready` từ lúc nhận SIGTERM.
- Mọi path khác khớp pattern `/` → `handler` (middleware chain).

Khớp chính xác: `/healthz/` không phải probe, nó rơi vào `/` và nhận 404 từ `mux`. Proto nào khai
báo path `/healthz` hay `/readyz` sẽ bị `root` che mất.

### Middleware chain — `internal/middleware`

Lắp bằng `middleware.Chain(corsHandler, ...)`: phần tử đầu là ngoài cùng, `corsHandler` (đã bọc
`mux`) là trong cùng. Mỗi middleware làm việc chiều vào, gọi `next.ServeHTTP`, rồi làm việc chiều ra.

| # | Middleware | Chiều vào | Chiều ra |
|---|---|---|---|
| 1 | `RequestID` | Lấy `X-Request-ID` (không có thì UUID), gắn vào `ctx` dưới `commonhelper.TRACE_ID`, đặt header response | |
| 2 | `AccessLog` | Ghi thời điểm bắt đầu, bọc `w` bằng `recorder` | Log `http access`: method, path, status, bytes, latency_ms, remote_ip, user_agent, traceId |
| 3 | `Recovery` | `defer recover()` | Panic ở tầng dưới → 500 `INTERNAL` |
| 4 | `StripIdentityHeaders` | Xóa `X-User-ID`, `X-User-Email`, `X-Roles`, `X-Scopes`, `X-Tenant-ID`, `X-Internal-*` | |
| 5 | `MaxBodyBytes` | `Content-Length` > limit → **413, dừng**. Không thì bọc body bằng `MaxBytesReader` | |
| 6 | `Timeout` | `ctx` có deadline `server.request_timeout` (10s) | `cancel()` |
| 7 | CORS (`rs/cors`) | Preflight `OPTIONS` → **204, dừng**. Request thường: thêm `Access-Control-*` nếu origin hợp lệ | |

Lý do thứ tự:
- `RequestID` ngoài cùng để mọi dòng log bên dưới có cùng correlation ID.
- `AccessLog` trên `Recovery` để panic đã recover vẫn được log thành 500.
- `Timeout` sát `mux` để deadline chỉ tính cho phần dịch và gọi downstream.

Mọi request dừng giữa chain (413, preflight, panic) vẫn đi ngược qua `AccessLog`, nên vẫn có log.

CORS **không phải kiểm soát truy cập**: origin không hợp lệ vẫn được forward xuống downstream, chỉ
thiếu header `Access-Control-*` nên trình duyệt không cho JavaScript đọc response. Client ngoài
trình duyệt không bị CORS chặn.

### ③ Dịch — `runtime.ServeMux`

Code chạy ở đây là handler sinh ra trong
`gm-core/application/<service>/v<n>/service/*.pb.gw.go` cộng với tùy biến trong `internal/gatewaymux`.

1. **Khớp route** theo method + path pattern sinh từ `google.api.http`.
   Không path nào khớp → 404; path khớp nhưng sai method → 405 (`routingErrorHandler`).
2. **Annotate context** (`runtime.AnnotateContext`):
   - `incomingHeaderMatcher`: chỉ `Authorization`, `Cookie`, `Content-Type`, `Accept`,
     `Accept-Language`, `User-Agent` thành gRPC metadata; header khác bị bỏ.
   - `forwardTraceID`: đọc request ID từ `ctx`, gắn metadata `traceId` — key mà
     `grpcmiddlewarehelper.SetIDUnaryServerInterceptor` phía service đọc.
   - grpc-gateway tự thêm `x-forwarded-for`, `x-forwarded-host`.
3. **Dựng message protobuf**: path param + query (`?limit=10` → `ListProductsRequest{Limit: 10}`);
   route có `body` thì decode JSON body. Parse lỗi, hoặc body chunked vượt `MaxBytesReader` → 400.
4. **Gọi gRPC** qua `productConn` với `ctx` ở trên. Deadline từ `Timeout` đi xuống downstream thành
   `grpc-timeout`.
5. **Trả kết quả**:
   - Thành công: header gRPC qua `outgoingHeaderMatcher` (chỉ `Set-Cookie`, `Location`),
     `forwardLocationHeader` đổi thành 303 nếu downstream gửi `location`, marshal JSON, 200.
   - Lỗi: `errorHandler` map gRPC code → HTTP status. Lỗi do client gây ra giữ message của
     downstream; lỗi phía server trả message cố định, lỗi gốc chỉ nằm trong log.

### Chiều về

JSON ghi vào `w` (là `recorder` của `AccessLog`) → stack trả ngược: `Timeout` cancel, `Recovery`
không có gì để bắt, `AccessLog` ghi log → `http.Server` flush response, giữ kết nối để dùng lại.
Toàn bộ phần ghi response phải xong trong `server.write_timeout` (30s).

## Request dừng ở đâu

| Nơi dừng | Điều kiện | Response | Có access log |
|---|---|---|---|
| `http.Server` | Header > `max_header_bytes` | 431 | Không |
| `http.Server` | Đọc header/body quá `read_header_timeout`/`read_timeout` | Đóng kết nối | Không |
| `root` | `/healthz`, `/readyz` | 200 / 503 text | Không |
| `MaxBodyBytes` | `Content-Length` > `max_request_body_bytes` | 413 `REQUEST_TOO_LARGE` | Có |
| CORS | Preflight `OPTIONS` | 204 | Có |
| `Recovery` | Panic trong `StripIdentityHeaders` trở xuống | 500 `INTERNAL` | Có |
| `mux` routing | Không khớp path / sai method | 404 / 405 | Có |
| `mux` decode | Query/body không hợp lệ | 400 `INVALID_ARGUMENT` | Có |
| Downstream | Service chết / không kết nối được | 503 `UNAVAILABLE` | Có |
| Downstream | Quá `request_timeout` | 504 `DEADLINE_EXCEEDED` | Có |
| Downstream | Client ngắt kết nối giữa chừng | 499, không body | Có |
| Downstream | Trả lỗi nghiệp vụ | Theo bảng `mappings` trong `internal/gatewaymux/error.go` | Có |

`RequestID` và `AccessLog` đứng trên `Recovery`, nên panic trong hai middleware đó không được
`Recovery` bắt; `net/http` sẽ recover, log ra stderr và đóng kết nối.

## Các timeout trên đường đi

| Config | Mặc định | Tính từ → đến |
|---|---|---|
| `server.read_header_timeout` | 5s | Accept → đọc xong header |
| `server.read_timeout` | 15s | Accept → đọc xong body |
| `server.request_timeout` | 10s | Middleware `Timeout` → xong lời gọi downstream |
| `server.write_timeout` | 30s | Đọc xong header → ghi xong response |
| `server.idle_timeout` | 60s | Kết nối keep-alive chờ request kế |

`request_timeout` phải nhỏ hơn `write_timeout`, không thì gateway không kịp ghi response 504; và
lớn hơn timeout của downstream một chút, để gateway nhận lỗi của downstream thay vì tự cắt.

## Thêm thứ mới vào đâu

- **Downstream service mới**: thêm `grpc_services.<name>` vào config, dial trong `main.go`, gọi
  `<svc>svcv1.Register<Service>ServiceHandler(ctx, mux, conn)` trên cùng `mux`. Không đụng tới
  `root` hay chain.
- **Middleware áp cho mọi API** (ví dụ auth — mục 1.7 của `gateway-criteria.md`): thêm vào
  `middleware.Chain` trong `main.go`. Auth nên đứng sau `StripIdentityHeaders` và trước `Timeout`,
  để identity do gateway gắn không bị strip và request bị từ chối không chiếm deadline.
- **Endpoint vận hành không phải API** (ví dụ `/metrics`): đăng ký trên `root`, cạnh probe.
- **Header mới cần tới downstream**: thêm vào `incomingHeaderMatcher`, không mở tiền tố
  `grpc-metadata-`.
