FROM golang:1.26-bookworm AS builder

# Build context is the k2app root: k2's go.mod replaces kuic and smux with
# ../third_party/*, so both trees must be in the image at the same layout.
WORKDIR /src
COPY third_party third_party
COPY k2/go.mod k2/go.sum k2/
WORKDIR /src/k2
RUN go mod download

COPY k2 .

# Build all binaries
RUN CGO_ENABLED=0 go build -o /k2s ./cmd/k2s
RUN CGO_ENABLED=0 go build -o /k2  ./cmd/k2
RUN CGO_ENABLED=0 go build -o /gen-config ./docker/gen-config

# ---- Server image ----
FROM debian:bookworm-slim AS k2s
COPY --from=builder /k2s /usr/local/bin/k2s
COPY --from=builder /gen-config /usr/local/bin/gen-config
ENTRYPOINT ["k2s"]

# ---- Client image ----
FROM debian:bookworm-slim AS k2
RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=builder /k2 /usr/local/bin/k2
ENTRYPOINT ["k2"]
