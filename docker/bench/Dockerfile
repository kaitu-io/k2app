FROM golang:1.26-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

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
COPY docker/bench/sing-box /usr/local/bin/sing-box

COPY docker/bench/speedtest.py /speedtest.py
COPY docker/bench/report.py /report.py
COPY docker/bench/entrypoint-server.sh /entrypoint-server.sh
COPY docker/bench/entrypoint-client.sh /entrypoint-client.sh
COPY docker/bench/scenarios.sh /scenarios.sh

RUN chmod +x /entrypoint-server.sh /entrypoint-client.sh /scenarios.sh /usr/local/bin/sing-box
