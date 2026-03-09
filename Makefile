.PHONY: build test generate clean netem-test \
	build-darwin-arm64 build-darwin-amd64 build-darwin-universal \
	build-windows-amd64 build-linux-amd64 build-linux-arm64 \
	appext-ios appext-macos appext-android

VERSION ?= dev
COMMIT  ?= $(shell git rev-parse --short HEAD)
LDFLAGS  = -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)
# Production builds disable go-deadlock for zero overhead.
PROD_TAGS = -tags=deadlock_disable

# --- Native build ---
build:
	mkdir -p build
	go build $(PROD_TAGS) -ldflags "$(LDFLAGS)" -o build/k2 ./cmd/k2
	go build $(PROD_TAGS) -ldflags "$(LDFLAGS)" -o build/k2s ./cmd/k2s

test:
	go test ./...

generate:
	go generate ./daemon/wintun/

# --- macOS ---
build-darwin-arm64:
	mkdir -p build
	CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build $(PROD_TAGS) -ldflags "$(LDFLAGS)" -o build/k2-darwin-arm64 ./cmd/k2

build-darwin-amd64:
	mkdir -p build
	CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build $(PROD_TAGS) -ldflags "$(LDFLAGS)" -o build/k2-darwin-amd64 ./cmd/k2

build-darwin-universal: build-darwin-arm64 build-darwin-amd64
	lipo -create -output build/k2-darwin-universal build/k2-darwin-arm64 build/k2-darwin-amd64
	rm -f build/k2-darwin-arm64 build/k2-darwin-amd64

# --- Windows ---
build-windows-amd64: generate
	mkdir -p build
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(PROD_TAGS) -ldflags "$(LDFLAGS)" -o build/k2-windows-amd64.exe ./cmd/k2

# --- Linux ---
build-linux-amd64:
	mkdir -p build
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(PROD_TAGS) -ldflags "$(LDFLAGS)" -o build/k2-linux-amd64 ./cmd/k2

build-linux-arm64:
	mkdir -p build
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(PROD_TAGS) -ldflags "$(LDFLAGS)" -o build/k2-linux-arm64 ./cmd/k2

# --- Server (standalone) ---
build-server-darwin:
	mkdir -p build
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build $(PROD_TAGS) -ldflags "$(LDFLAGS)" -o build/k2s-darwin-amd64 ./cmd/k2s
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build $(PROD_TAGS) -ldflags "$(LDFLAGS)" -o build/k2s-darwin-arm64 ./cmd/k2s
	lipo -create -output build/k2s-darwin-universal build/k2s-darwin-amd64 build/k2s-darwin-arm64
	rm -f build/k2s-darwin-amd64 build/k2s-darwin-arm64

build-server-windows: generate
	mkdir -p build
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(PROD_TAGS) -ldflags "$(LDFLAGS)" -o build/k2s-windows-amd64.exe ./cmd/k2s

build-server-linux:
	mkdir -p build
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(PROD_TAGS) -ldflags "$(LDFLAGS)" -o build/k2s-linux-amd64 ./cmd/k2s
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(PROD_TAGS) -ldflags "$(LDFLAGS)" -o build/k2s-linux-arm64 ./cmd/k2s

# --- Mobile (gomobile bind) ---
appext-ios:
	mkdir -p build
	gomobile bind -tags "with_gvisor deadlock_disable" -target=ios -o build/K2Mobile.xcframework ./appext/

appext-macos:
	mkdir -p build
	gomobile bind -tags "with_gvisor deadlock_disable" -target=macos -o build/K2MobileMacOS.xcframework ./appext/

appext-android:
	mkdir -p build
	gomobile bind -tags "with_gvisor deadlock_disable" -target=android -o build/k2mobile.aar -androidapi 24 ./appext/

# --- Network degradation tests (Docker netem) ---
netem-test:
	SCENARIO=$(or $(SCENARIO),all) test/netem/run.sh

clean:
	rm -rf build/
