# K2 Sidecar - registration, config generation, RADIUS proxy
# Binary must be pre-built: CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o k2-sidecar .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates curl
WORKDIR /app
COPY k2-sidecar /usr/local/bin/k2-sidecar
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh /usr/local/bin/k2-sidecar
ENTRYPOINT ["/entrypoint.sh"]
