FROM golang:1.26-bookworm AS builder

# Build context is the k2app root: k2's go.mod replaces kuic and smux with
# ../third_party/*, so both trees must be in the image at the same layout.
WORKDIR /src
COPY third_party third_party
COPY k2/go.mod k2/go.sum k2/
WORKDIR /src/k2
RUN go mod download
COPY k2 .

RUN CGO_ENABLED=0 go build -o /k2s ./cmd/k2s && \
    CGO_ENABLED=0 go build -o /k2  ./cmd/k2 && \
    CGO_ENABLED=0 go build -o /gen-bench-config ./docker/bench/gen-bench-config

# --- Runtime ---
FROM debian:bookworm-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        iproute2 python3 curl ca-certificates procps && \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /k2s /usr/local/bin/k2s
COPY --from=builder /k2  /usr/local/bin/k2
COPY --from=builder /gen-bench-config /usr/local/bin/gen-bench-config

# sing-box binary (pre-downloaded linux-amd64, v1.12.17)
COPY k2/docker/bench/sing-box /usr/local/bin/sing-box

COPY k2/docker/bench/speedtest.py /speedtest.py
COPY k2/docker/bench/report.py /report.py
COPY k2/docker/bench/entrypoint-server.sh /entrypoint-server.sh
COPY k2/docker/bench/entrypoint-client.sh /entrypoint-client.sh
COPY k2/docker/bench/scenarios.sh /scenarios.sh

RUN chmod +x /entrypoint-server.sh /entrypoint-client.sh /scenarios.sh /usr/local/bin/sing-box
