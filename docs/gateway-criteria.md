# Tiêu chí cho API Gateway production-grade

Checklist để implement dần. Mỗi mục: **việc cần làm** + **lý do** + **trạng thái**.

Ký hiệu trạng thái: `[x]` đã có · `[~]` có nhưng chưa đủ · `[ ]` chưa làm.

---

## 0. Trạng thái repo hiện tại (2026-09-12, sau khi implement v1)

Mục 1 (v1) đã xong, trừ auth (1.7) và phần deploy (1.14). Code liên quan:

| File | Vai trò |
|---|---|
| `cmd/server/main.go` | Dial downstream, dựng middleware chain, health mux, drain + graceful shutdown |
| `internal/middleware/middleware.go` | RequestID, AccessLog, Recovery, StripIdentityHeaders, MaxBodyBytes, Timeout |
| `internal/gatewaymux/mux.go` | Header allowlist, forward trace ID xuống gRPC metadata |
| `internal/gatewaymux/error.go` | Map gRPC code → HTTP status, che lỗi nội bộ, routing error 404/405 |
| `internal/health/health.go` | `/healthz`, `/readyz` + readiness flag cho drain |
| `internal/apierror/apierror.go` | Một shape JSON error duy nhất cho mọi lỗi do gateway sinh ra |
| `configs/config.go` | Block `server:` chứa toàn bộ timeout và limit |

Đã dọn (2026-09-13): `copyhelper` + `httpclienthelper` khỏi `internal/diregistry/diregistry.go` và block `http_client` khỏi config — gateway không convert pb↔DTO và không gọi HTTP upstream; `cmd/client/` (client gRPC thử gọi thẳng gm-product, không qua gateway). Còn sót: `internal/constants/constants.go` rỗng.

---

## 1. v1 — Bản tối thiểu để chạy được

Mục tiêu: một ingress HTTP đúng đắn, có thể để chạy thật mà không tự bắn vào chân. Không có resilience nâng cao, không rate limit, không metrics.

### 1.1 Routing & proxy

- `[x]` Route sinh từ `google.api.http` annotation trong `proto/`, một `RegisterXHandler` cho mỗi downstream service.
- `[x]` Upstream address lấy từ config/env, không hardcode trong code.
- `[x]` Versioning path (`/v1/...`) quyết định ngay từ proto annotation — đổi sau là breaking change cho client.

### 1.2 Stateless

- `[x]` Bỏ GORM, `database_postgres` khỏi gateway (`go mod tidy` kéo theo: gorm, pgx, prometheus, jwt rời khỏi `go.mod`).
  **Lý do:** gateway có DB là bước đầu để business logic bò vào; stateless mới scale ngang và restart tự do được.

### 1.3 Timeout (quan trọng nhất trong nhóm resilience, rẻ nhất để làm)

- `[x]` `http.Server`: `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, `IdleTimeout`, tất cả từ config `server:`.
- `[x]` `middleware.Timeout` bọc mỗi request bằng `context.WithTimeout` → grpc-gateway truyền context đó xuống gRPC call nên deadline lan tới downstream. Verified: upstream blackhole trả 504 đúng tại mốc `request_timeout`.
- `[x]` `request_timeout` (10s) nằm giữa timeout downstream và `write_timeout` (30s), ghi rõ lý do trong comment của `ServerConfig`.
  **Lý do:** không có timeout thì một service treo sẽ giữ hết connection của gateway và kéo sập toàn hệ thống. Đây là thứ duy nhất trong mục 2 không được hoãn.

### 1.4 Request ID / trace ID

- `[x]` `middleware.RequestID` đọc `X-Request-ID` của client, không có thì sinh UUID; lưu vào context dưới key `commonhelper.TRACE_ID` (đúng key `loghelper.WithContext` đọc); echo lại trên response; `gatewaymux.forwardTraceID` đẩy xuống gRPC metadata, khớp với `grpcmiddlewarehelper.SetIDUnaryServerInterceptor` phía service.
  **Lý do:** không có correlation ID thì không thể lần một request qua gateway → service → service.

### 1.5 Access log có cấu trúc

- `[x]` `middleware.AccessLog`: method, path, status, bytes, latency_ms, remote_ip, user_agent, traceId.
- `[x]` Không log body, token, cookie. Health probe nằm ngoài chain nên không làm ngập log.
  **Lý do:** đây là nguồn dữ liệu debug duy nhất trước khi có metrics và tracing.
  **Chưa có:** field `upstream`/route pattern — chỉ có ý nghĩa khi đã nhiều hơn một downstream service.

### 1.6 Trust boundary của header (bắt buộc, dù chưa làm auth)

- `[x]` `middleware.StripIdentityHeaders` xóa `X-User-ID`, `X-User-Email`, `X-Roles`, `X-Scopes`, `X-Tenant-ID` và mọi header `X-Internal-*` do client gửi. Có unit test.
- `[x]` `gatewaymux.incomingHeaderMatcher` là allowlist: chỉ `Authorization`, `Cookie`, `Content-Type`, `Accept`, `Accept-Language`, `User-Agent` được qua. Tiền tố `grpc-metadata-` của grpc-gateway bị chặn, vì nó cho client tự inject metadata tùy ý — đúng lỗ hổng mà mục này muốn bịt.
  **Lý do:** nếu downstream tin `X-User-ID` mà gateway không strip, bất kỳ client nào cũng mạo danh được user khác. Đây là lỗ hổng nghiêm trọng nhất trong danh sách và tốn vài dòng code.

### 1.7 Auth (làm ngay nếu đã có endpoint cần bảo vệ)

- `[ ]` Middleware verify JWT: signature, `exp`, `iss`, `aud`; public route theo whitelist.
- `[ ]` Verify xong, inject identity xuống downstream qua header/metadata; downstream **tin** gateway, không verify lại.
- `[ ]` AuthZ thô theo route (role nào gọi được endpoint nào) ở gateway; authZ theo resource ở service.
  **Đang hoãn vì:** `gm-core` chưa phát hành token. 1.6 đã làm xong nên trust boundary không hở trong lúc chờ. Hiện `Authorization` được forward thô xuống downstream; khi làm 1.7 thì gateway verify rồi gửi identity, không gửi credential nữa.

### 1.8 Che lỗi nội bộ

- `[x]` `gatewaymux.errorHandler`: bảng map gRPC code → HTTP status. Code 5xx (`Internal`, `Unknown`, `DataLoss`, `Unavailable`, `DeadlineExceeded`, `Unimplemented`) trả message cố định; code do client gây ra (`InvalidArgument`, `NotFound`, `PermissionDenied`, `Unauthenticated`…) mới cho message của downstream đi qua, vì đó là thông tin client cần để sửa request.
- `[x]` `gatewaymux.routingErrorHandler`: giữ nguyên status của routing. Default của grpc-gateway biến 405 thành `Unimplemented` → publish ra 501, sai.
  **Lý do:** grpc-gateway mặc định đẩy nguyên `err.Error()` của downstream ra ngoài, rất dễ lộ query/stack/tên internal service. Verified: downstream chết → client nhận `503 {"code":"UNAVAILABLE","message":"Service temporarily unavailable"}`, còn `dial tcp 127.0.0.1:50060: connect: connection refused` chỉ nằm trong log.

### 1.9 Giới hạn input cơ bản

- `[x]` `middleware.MaxBodyBytes`: `Content-Length` vượt hạn mức → 413 ngay, `http.MaxBytesReader` là lưới đỡ cho request chunked (trường hợp đó grpc-gateway báo 400, chấp nhận được).
- `[x]` `http.Server.MaxHeaderBytes` từ config.
  **Lý do:** chặn request khổng lồ làm cạn memory, một dòng config.

### 1.10 Panic recovery ở HTTP layer

- `[x]` `middleware.Recovery`: log panic + stack, trả 500 generic, không để detail ra client. Có unit test.
  **Lý do:** `grpcmiddlewarehelper.recovery` chỉ bảo vệ gRPC server của service, không bảo vệ middleware chain của gateway.

### 1.11 Health endpoints

- `[x]` `/healthz` (liveness): 200 khi process còn sống, kể cả đang drain.
- `[~]` `/readyz` (readiness): 200 chỉ sau khi route đã register, 503 ngay khi nhận SIGTERM.
  **Lý do:** thiếu readiness thì rolling update sẽ route traffic vào pod chưa sẵn sàng.
  **TODO:** readiness chưa phản ánh downstream. `grpc.NewClient` dial lười nên `/readyz` vẫn 200 khi gm-product chết, request thật nhận 503 `UNAVAILABLE`. Cần quyết định trước khi làm: fail readiness khi *một* downstream chết sẽ rút *toàn bộ* gateway khỏi traffic, kể cả route của service còn sống — nên có thể chỉ cần đưa trạng thái `ClientConn` vào log/metrics thay vì vào `/readyz`. Comment TODO ở `readiness.SetReady()` trong `cmd/server/main.go`.

### 1.12 Graceful shutdown

- `[x]` `drainAndShutdown` trong `main.go`: SIGTERM → readiness fail → chờ `shutdown_drain_delay` (vẫn phục vụ request) → `httpServer.Shutdown` với `shutdown_timeout` → đóng `grpc.ClientConn`.
  **Lý do:** K8s xóa pod khỏi endpoint list không đồng bộ; shutdown ngay lập tức vẫn làm rơi request đang được route tới. Khi deploy, `terminationGracePeriodSeconds` phải lớn hơn `shutdown_drain_delay + shutdown_timeout`.
  *Không dùng `serverhelper.HandleGracefulShutdownHttpServer` nữa vì nó không biết readiness và gọi `log.Fatal` khi shutdown lỗi.*

### 1.13 CORS

- `[x]` Xử lý tập trung tại gateway bằng `rs/cors`, origin từ config; `X-Request-ID` nằm trong `AllowedHeaders` và `ExposedHeaders` để client đọc được correlation ID.

### 1.14 Không SPOF

- `[ ]` Khi deploy: ≥2 replica + PodDisruptionBudget. Điều kiện để làm được là 1.2 (stateless).

**Tóm lại v1 = routing đã có + stateless + timeout + request ID thật + access log + strip header identity + error handler che lỗi + health/readyz + shutdown tử tế + giới hạn size + recovery.** Đã implement xong, còn lại 1.7 (auth, chờ `gm-core`) và 1.14 (deploy).

### Cách smoke test v1

```bash
HTTP_PORT=18080 go run ./cmd/server/main.go &

curl -i localhost:18080/healthz                      # 200
curl -i localhost:18080/readyz                       # 200
curl -i localhost:18080/nope                         # 404 NOT_FOUND
curl -i -X PUT localhost:18080/v1/products           # 405 METHOD_NOT_ALLOWED (không phải 501)
curl -i localhost:18080/v1/products                  # 503 UNAVAILABLE khi chưa có gm-product
curl -i -H 'X-Request-ID: test-123' localhost:18080/v1/products   # X-Request-ID echo lại đúng test-123

# Timeout: trỏ upstream vào IP không route được
HTTP_PORT=18081 GRPC_SERVICES__PRODUCT__HOST=10.255.255.1 SERVER__REQUEST_TIMEOUT=800ms \
  go run ./cmd/server/main.go &
curl -i localhost:18081/v1/products                  # 504 DEADLINE_EXCEEDED sau ~0.8s

# Drain: kill -TERM <pid> rồi gọi ngay
curl -o /dev/null -w '%{http_code}\n' localhost:18080/readyz   # 503 ngay, server vẫn phục vụ tiếp 5s
```

`go test ./internal/middleware/...` phủ phần strip header identity, request ID, body limit và recovery.

---

## 2. v2 — Resilience

Làm khi đã có ≥2 downstream service thật và traffic thật. Trước đó chỉ là code chết.

- `[ ]` **Circuit breaker** per upstream: lỗi liên tục → mở circuit, fail fast.
  *Hoãn vì:* cần biết ngưỡng lỗi thực tế mới set threshold đúng; set bừa sẽ tự cắt traffic lành.
- `[ ]` **Retry có kiểm soát**: chỉ GET và PUT có idempotency key; retry budget + exponential backoff + jitter.
  *Hoãn vì:* retry sai là nguồn gây bão traffic và duplicate order/payment. Không có thì chỉ là lỗi trả về client; có mà sai thì là sự cố.
- `[ ]` **Bulkhead**: giới hạn concurrent request / connection pool theo từng upstream.
- `[ ]` **Load shedding**: quá tải thì reject sớm (503) thay vì queue đến chết.
- `[ ]` **Rate limiting**: per-client (API key/user/IP), per-route, global; token bucket hoặc sliding window; state ở Redis (`gopkg/redisclienthelper`) khi chạy nhiều replica; trả 429 + `Retry-After`.
  *Hoãn vì:* cần Redis và cần biết baseline RPS. Nếu cần chặn sớm, bản in-memory per-replica là tạm được nhưng phải ghi rõ là tạm.

---

## 3. v3 — Observability đầy đủ

- `[ ]` **Metrics Prometheus** (`gopkg/prometheushelper`): RPS, latency p50/p95/p99 per route, error rate 4xx/5xx per upstream, circuit breaker state, connection pool usage. Nên là mục làm ngay sau v1 — rẻ và có giá trị lập tức.
- `[ ]` **Distributed tracing** OpenTelemetry, W3C Trace Context; gateway là span gốc.
  *Hoãn vì:* cần collector (Jaeger/Tempo) chạy trước. Phần propagate `traceparent` thì nên gộp luôn vào 1.4 để không phải sửa lại.

---

## 4. v4 — Security & vận hành nâng cao

- `[ ]` TLS termination + cert rotation tự động (cert-manager). *Trên K8s có thể để Ingress/LB làm, gateway nhận plaintext nội bộ.*
- `[ ]` mTLS nội bộ giữa gateway và service (hiện `grpcclienthelper` mặc định `insecure`, chấp nhận được khi traffic không rời mạng nội bộ).
- `[ ]` IP allowlist/denylist, WAF phía trước (Cloud Armor nếu trên GCP).
- `[ ]` Service discovery: DNS-based qua K8s Service; gRPC cần `dns:///` + `round_robin` service config để load balance đúng, vì connection gRPC là long-lived nên LB ở tầng L4 sẽ dính một pod.
- `[ ]` Load balancing: round-robin → least-conn → consistent hashing (chỉ khi cần sticky).
- `[ ]` Config declarative + hot-reload route/rate limit không cần restart.
  *Hoãn vì:* với số route ít, rolling restart đã đủ; hot-reload thêm một nguồn bug khó tái hiện.
- `[ ]` Canary / traffic splitting theo % hoặc header.
- `[ ]` Response compression, response caching cho dữ liệu tĩnh/ít đổi.

---

## 5. Không nhét vào gateway

- Business logic.
- Orchestration nhiều service trong một request (việc của BFF hoặc aggregator service riêng).
- Transformation phức tạp giữa request và response.

Gateway càng mỏng càng dễ vận hành. Mọi logic nghiệp vụ ở đây đều thành bottleneck và điểm coupling chung của mọi team.

---

## 6. Anti-patterns cần tránh

| Anti-pattern | Hậu quả |
|---|---|
| Gateway thành "God service" chứa business logic | Deploy gateway phải coordinate với mọi team |
| Không set timeout | Một service treo kéo sập toàn bộ gateway |
| Retry mù quáng request non-idempotent | Duplicate order / payment |
| Không strip header identity từ client | Mạo danh user — lỗ hổng nghiêm trọng |
| Verify lại JWT ở mọi service phía sau | Tốn CPU vô ích, trust boundary mờ |
| Gateway 1 replica | SPOF |
| Gateway có DB riêng | Mất tính stateless, không scale ngang được |

---

## 7. Helper có sẵn trong `gopkg`

| Nhu cầu | Package |
|---|---|
| Dial downstream gRPC | `grpcclienthelper` |
| Graceful shutdown HTTP | `serverhelper` |
| Structured log (zap) | `loghelper` |
| JWT / auth middleware | `authhelper` |
| Metrics Prometheus | `prometheushelper` |
| Rate limit state | `redisclienthelper` |
| Response envelope | `responsehelper` |
| Trace ID context key | `commonhelper.TRACE_ID` |

Trước khi viết helper mới, đọc `gopkg/README.md`. Code cross-cutting thuộc về `gopkg`, không thuộc về gateway.
