# CLAUDE.md

File này hướng dẫn Claude Code (claude.ai/code) khi làm việc với code trong repo này.

## Stack

- Module: `github.com/dangngochoainam/gm-gateway` · Go 1.24
- gRPC v1.80 + grpc-gateway v2.27 · Uber Dig v1.19 · rs/cors v1.11 · logging qua `gopkg/loghelper`
- Không GORM, không driver database, không message broker. Xem mục "Không có database" bên dưới.

## Lệnh

```bash
# Môi trường dev bằng Docker
make start              # docker compose -f docker-compose.debug.yaml up -d
make attach             # Vào shell của container debug đang chạy
make mod                # go mod tidy && go mod vendor

# Chạy
make run                # go run cmd/server/main.go
make run-watch          # Live reload qua CompileDaemon (Delve ở port 2302)

# Sinh code (proto nằm ở gm-core, không nằm ở đây — repo này không có target gen-proto)
cd ../gm-core && make gen-proto   # chạy trên host: cần buf, image này không mang theo
make mod                # quay lại đây, vendor lại để code sinh ra mới được nhận

# Kiểm tra (config lint ở .golangci.yml; lỗi lint có sẵn ghi trong NOTES.md)
make fmt                # sửa format: gofmt + goimports
make lint               # golangci-lint v2.14.0 — chỉ chặn lỗi mới so với origin/master
make build              # go build ./...
make verify             # lint + build, không chạy test
make test               # go test ./... — unit test, không tag
```

## Kiến trúc

Gateway là một bộ dịch, không phải một ứng dụng phân tầng. Không có gì kết thúc tại đây:

```
client → HTTP :50051 → chuỗi middleware → grpc-gateway mux → gRPC → domain service
```

**Một listener.** `cmd/server/main.go` khởi động đúng một HTTP server trên `http_port`. Ở đây không
có gRPC server; gRPC là thứ gateway *nói ra bên ngoài* với các domain service liệt kê dưới
`grpc_services` trong `configs/config.go`.

**Chuỗi middleware**, từ ngoài vào trong, theo đúng cách `middleware.Chain` lắp trong `cmd/server/main.go`:
1. `RequestID` — correlation ID
2. `AccessLog` — nằm trên Recovery, để một panic đã được recover vẫn được log thành 500
3. `Recovery`
4. `StripIdentityHeaders` — xóa header identity (`X-User-ID`, `X-Roles`, `X-Internal-*`…) do client tự gửi
5. `MaxBodyBytes`
6. `Timeout` — để deadline của nó chỉ bao lời gọi downstream
7. CORS (`rs/cors`, origin lấy từ `allowed_origins`) — bọc trực tiếp mux, vì `Chain` nhận nó làm handler trong cùng

Allowlist header đi xuống gRPC metadata (chặn client tự bịa trace ID hay metadata) không phải
middleware mà là `incomingHeaderMatcher` trong `internal/gatewaymux/mux.go`.

`/healthz` và `/readyz` mount trên `http.ServeMux` gốc, nằm ngoài chuỗi này.

Vòng đời đầy đủ của một request (listener → mux gốc → middleware → grpc-gateway mux → downstream),
nơi request có thể dừng và các timeout trên đường đi: `docs/request-lifecycle.md`. Đổi thứ tự
middleware hay thêm tầng thì cập nhật file đó.

### Trách nhiệm từng layer

Gateway không có layer API, use case, repository hay entity. Nó không sở hữu logic nghiệp vụ và
không giữ state: nó kết thúc HTTP, dịch sang gRPC, rồi forward.

| Layer | Vị trí | Vai trò |
|-------|--------|---------|
| Entry point | `cmd/server/main.go` | Dựng DI container, khởi động HTTP listener, graceful shutdown |
| Mux | `internal/gatewaymux/` | Dựng grpc-gateway ServeMux: allowlist header của client, forward correlation ID, map lỗi downstream sang HTTP status |
| Middleware | `internal/middleware/` | Chuỗi HTTP bọc quanh mux; thứ tự do `Chain` trong `cmd/server/main.go` quyết định |
| Probes | `internal/health/` | `/healthz` và `/readyz`; readiness tắt khi nhận SIGTERM để pod drain trước |
| Errors | `internal/apierror/` | Một shape lỗi duy nhất mà client nhìn thấy; chi tiết upstream chỉ nằm trong log |

### Package quan trọng

Helper dùng chung nằm ở repo ngoài [`gopkg`](https://github.com/dangngochoainam/gopkg) (mount ở local qua `replace` trong `go.mod`). Xem repo đó để có tài liệu package mới nhất.

### Quy trình protobuf → Go

Repo này **không có `proto/` và không có `pb/`**. Mọi contract downstream nằm trong `gm-core` tại
`proto/<service>/v<n>/`, chia thành một package `dto` và một package `service`:

```go
productsvcv1 "github.com/dangngochoainam/gm-core/application/product/v1/service"  // đăng ký route
productdtov1 "github.com/dangngochoainam/gm-core/application/product/v1/dto"      // message, chỉ ở nơi cần
```

`cmd/server/main.go` đăng ký route nên chỉ import `service`.

1. Sửa contract tại `../gm-core/proto/<service>/v1/dto/<service>.dto.proto` hoặc
   `service/<service>.service.proto`
2. `cd ../gm-core && make gen-proto` → buf regenerate ở đó. Repo này không có target `gen-proto`:
   không bao giờ generate gì vào đây
3. `make mod` ở đây → tidy và vendor lại
4. Đăng ký bộ route trong `cmd/server/main.go`: mỗi downstream service connection một lời gọi
   `<service>svcv1.Register<Service>ServiceHandler(ctx, mux, conn)`

Khi một service phát hành `v2`, gateway có thể register cả `v1` lẫn `v2` trên cùng một mux trong lúc
caller migrate, miễn là đường dẫn HTTP của chúng khác nhau.

Gateway là process duy nhất chạy grpc-gateway mux, nên annotation `google.api.http` trên các proto
đó tồn tại là vì nó. `gm-core` được dùng theo path
(`replace github.com/dangngochoainam/gm-core => ../gm-core`) và bind-mount tại `/gm-core` trong
container debug. Xem `../gm-core/CLAUDE.md` để biết các quy ước.

### Không có database

Không có, và đó là chủ ý: một gateway stateless scale ngang và restart thoải mái, còn state thuộc
về các domain service phía sau. `internal/diregistry` ghi rõ điều đó ngay tại chỗ lẽ ra sẽ provide
một database. Vì vậy không có `db/`, không entity GORM, không bước migration, và không có Atlas
trong `Dockerfile.debug`. Nếu sau này một nhu cầu ở gateway trông như thật sự cần lưu trữ, hãy chất
vấn nhu cầu đó trước khi thêm database.

### Dependency injection

Dependency đăng ký trong `internal/diregistry/diregistry.go` bằng Uber Dig. Hiện đồ thị chỉ có
Config: gateway không convert pb↔DTO (grpc-gateway tự marshal) và không gọi HTTP upstream, nên
không provide `copyhelper` hay `httpclienthelper`. Khi thêm một dependency, đăng ký constructor của
nó ở đó và nhận nó làm tham số trong nơi tiêu thụ.

**Pattern: luôn inject qua tham số constructor — không bao giờ khởi tạo trực tiếp bên trong constructor.**

```go
// SAI — hard-code, đi vòng qua DI
func NewTokenVerifier() TokenVerifier {
    cfg, _ := configs.LoadConfig()
    return &tokenVerifier{cfg: cfg}
}

// ĐÚNG — nhận qua tham số để Dig inject vào
func NewTokenVerifier(cfg *configs.Config) TokenVerifier {
    return &tokenVerifier{cfg: cfg}
}
```

## Cấu hình

Copy `.env.example` thành `.env`. Viper dùng `__` làm dấu phân cách key, nên field config lồng nhau
map sang biến môi trường như sau:

| Field config | Biến môi trường |
|---|---|
| `http_port` | `HTTP_PORT` |
| `grpc_services.product.host` | `GRPC_SERVICES__PRODUCT__HOST` |
| `server.request_timeout` | `SERVER__REQUEST_TIMEOUT` |
| `allowed_origins` | `ALLOWED_ORIGINS` — chuỗi phân tách bằng dấu phẩy, không bọc `[...]` |

Config mặc định nhúng trong `configs/config.go` (dùng khi không có env nào ghi đè). Giá trị mặc
định: HTTP `50051`, và `product` tại `localhost:50060`. Không có key `grpc_port` và không có
`database_postgres`: gateway chỉ lắng nghe HTTP và không giữ state.

## Docker

- `docker-compose.debug.yaml` + `Dockerfile.debug` — môi trường phát triển với debugger Delve và hot reload CompileDaemon; publish port `50061:50051` (HTTP) và `2302:2302` (Delve), ngoài ra không gì khác; mount repo vào `/app`, kèm `../gopkg` và `../gm-core`
