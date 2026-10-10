# gm-gateway

## 1. Resources

You only need two things installed locally:

- **Docker + Docker Compose**
- **`docker_stack` external network**, which is how the gateway reaches the domain services it
  fans out to. Each downstream address is configured under `grpc_services` in `configs/config.go`;
  today that is `product` alone. The gateway itself needs no database and no other backing service.

Go tooling for running and debugging the gateway (CompileDaemon, Delve) is pre-installed inside the container via `Dockerfile.debug` — no local setup required. Two things are deliberately absent: protobuf codegen, which belongs to `gm-core` and runs on the host with `buf`, and any database tooling, because the gateway is stateless.

If the network doesn't exist yet:
```bash
docker network create docker_stack
```

## 2. Run with watch

```bash
make start      # build image and start the container
make attach     # open a shell inside the container

# Inside the container:
make mod        # download and vendor deps (first time, or after go.mod changes)
make run-watch  # live reload via CompileDaemon + Delve debugger
```

Ports exposed to the host:

| Protocol | Host port |
|---|---|
| HTTP REST | `50061` |
| Delve debugger | `2302` |

The gateway runs no gRPC server. It speaks gRPC outward to the domain services, so there is
nothing to publish for it.

There is no migration step. The gateway holds no state and opens no database connection, so it
starts as soon as the container is up.

## 3. Regenerate proto

This repo has no `proto/` and no `pb/`. Every downstream contract lives in the shared
[`gm-core`](../gm-core) module under `proto/<service>/v<n>/`, split into `dto/<service>.dto.proto`
(messages) and `service/<service>.service.proto` (rpcs). Route registration only needs the
service half:

```go
productsvcv1 "github.com/dangngochoainam/gm-core/application/product/v1/service"
```

After editing a contract:

```bash
cd ../gm-core && make gen-proto   # on the host — needs buf; this repo has no gen-proto target
make mod                          # inside the container — re-tidy and re-vendor
```

Commit the regenerated `gm-core/application/` alongside your `.proto` change. If the change adds or moves
HTTP routes, update the matching `Register<Service>ServiceHandler` call in `cmd/server/main.go`,
one per downstream service connection. `gm-core` is bind-mounted at `/gm-core` in the debug container so
the `replace github.com/dangngochoainam/gm-core => ../gm-core` in `go.mod` resolves from `/app`.
