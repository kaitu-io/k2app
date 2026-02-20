# K2v5 Server - QUIC+TCP-WS VPN with ECH camouflage
# Binary must be pre-built and copied to this directory before docker build
# Build: cd k2 && go build -o ../docker/k2s/k2s ./cmd/k2s

FROM alpine:3.20

RUN apk add --no-cache ca-certificates iptables

WORKDIR /app

COPY k2s .
COPY entrypoint.sh .
RUN chmod +x entrypoint.sh k2s

ENV K2_HOP_PORT_MIN=10020 \
    K2_HOP_PORT_MAX=10119

EXPOSE 443

ENTRYPOINT ["./entrypoint.sh"]
