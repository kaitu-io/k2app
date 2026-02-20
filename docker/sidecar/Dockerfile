FROM golang:1.24-alpine AS builder
RUN apk add --no-cache ca-certificates
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o k2-sidecar .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates curl
COPY --from=builder /build/k2-sidecar /usr/local/bin/k2-sidecar
ENTRYPOINT ["k2-sidecar"]
