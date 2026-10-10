# Những việc đang hoãn

## Lỗi lint có sẵn

`.golangci.yml` đặt `new-from-merge-base: origin/master`, nên các lỗi dưới đây (có từ trước khi thêm lint) không làm `make verify` fail. Sửa xong hết thì bỏ dòng `new-from-merge-base` để lint chặn toàn repo.

Kết quả golangci-lint v2.14.0, linter `standard` + gofmt + goimports:

- `cmd/server/main.go:34` — errcheck: `defer loghelper.Logger.Sync()`
- `cmd/server/main.go:49` — errcheck: `defer productConn.Close()`
- `internal/diregistry/diregistry.go:20` — errcheck: `ctn.Provide`
