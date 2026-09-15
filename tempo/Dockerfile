FROM golang:1.25.9-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -o /out/tempo-evm-diff ./cmd/tempo-evm-diff && \
    CGO_ENABLED=0 go build -trimpath -o /out/tempo-compare ./cmd/tempo-compare && \
    CGO_ENABLED=0 go build -trimpath -o /out/evm-rpc-oracle ./cmd/tempo-chain-oracle && \
    CGO_ENABLED=0 go build -trimpath -o /out/zone-lifecycle-oracle ./cmd/zone-lifecycle-oracle && \
    CGO_ENABLED=0 go build -trimpath -o /out/semantic-coverage ./cmd/semantic-coverage && \
    CGO_ENABLED=0 go build -trimpath -o /out/tempo-fund ./cmd/tempo-fund

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/tempo-evm-diff /usr/local/bin/tempo-evm-diff
COPY --from=build /out/tempo-compare /usr/local/bin/tempo-compare
COPY --from=build /out/evm-rpc-oracle /usr/local/bin/evm-rpc-oracle
COPY --from=build /out/zone-lifecycle-oracle /usr/local/bin/zone-lifecycle-oracle
COPY --from=build /out/semantic-coverage /usr/local/bin/semantic-coverage
COPY --from=build /out/tempo-fund /usr/local/bin/tempo-fund
ENTRYPOINT ["/bin/sh", "-c", "sleep infinity"]
