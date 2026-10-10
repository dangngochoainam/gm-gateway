start:
	docker compose -f docker-compose.debug.yaml up -d

attach:
	docker exec -it gm-gateway-debug /bin/bash

mod:
	go mod tidy
	go mod vendor

run-watch:
	CompileDaemon -graceful-kill=true -build="go build -v -gcflags all=-N -o ./cmd/server/bin ./cmd/server/main.go" -command="dlv exec ./cmd/server/bin --listen=:2302 --headless=true --api-version=2 --accept-multiclient --continue"

run:
	go run ./cmd/server/main.go

# golangci-lint pin theo version, chạy qua `go run` nên không cài gì lên máy.
# Config ở .golangci.yml; chỉ lỗi mới so với origin/master mới làm fail.
GOLANGCI_LINT := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0

fmt:
	$(GOLANGCI_LINT) fmt ./...

lint:
	$(GOLANGCI_LINT) run ./...

build:
	go build ./...

test:
	go test ./...

verify: lint build

# No gen-proto target here on purpose. Protos and generated code live in gm-core and
# nothing is ever generated into this repo, so the only honest place to run the
# generator is gm-core itself:
#
#   cd ../gm-core && make gen-proto   # needs buf on the host, not in this container
#   make mod                          # back here, to re-vendor the change
