.PHONY: build test generate clean netem-test \
	build-darwin-arm64 build-darwin-amd64 build-darwin-universal \
	build-windows-amd64 build-linux-amd64 build-linux-arm64 \
	build-k2r build-k2r-linux-amd64 build-k2r-linux-arm64 build-k2r-armv7 build-k2r-mipsle build-k2r-mips build-k2r-all \
	appext-ios appext-macos appext-android \
	quick-check pre-release build-all-platforms release \
	gateway-check gateway-uat

VERSION ?= dev
COMMIT  ?= $(shell git rev-parse --short HEAD)
K2_BUILD_LOG_LEVEL ?= debug
LDFLAGS  = -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X github.com/kaitu-io/k2/config.buildLogLevel=$(K2_BUILD_LOG_LEVEL)
# Build constraints. `release` strips test-only symbols (engine/test_export.go,
# test/k2subs_switch_*_test.go). Default test/dev paths (go test, quick-check)
# pass NO tags, so test_export.go is included and the E2E suite compiles.
# Production binaries built via these Makefile targets always include `release`.
RELEASE_TAGS = release
# Desktop: lock-ordering detection enabled (low overhead, catches deadlocks).
DESKTOP_TAGS = -tags=$(RELEASE_TAGS)
# Gateway (k2r): production-only build, same constraint.
PROD_TAGS    = -tags=$(RELEASE_TAGS)
# k2r ships headless (no embedded webapp) — nowebapp drops the embedded SPA
# asset (several MB) since discovery/config is done via the companion app.
K2R_TAGS     = -tags=$(RELEASE_TAGS),nowebapp
# Mobile: deadlock_disable for zero overhead (tight iOS NE ~50MB jetsam).
MOBILE_TAGS  = -tags=deadlock_disable,$(RELEASE_TAGS)
# gomobile bind takes a single -tags argument with space-separated values.
GOMOBILE_TAGS = with_gvisor deadlock_disable $(RELEASE_TAGS)

# --- Native build ---
build:
	mkdir -p build
	go build $(DESKTOP_TAGS) -ldflags "$(LDFLAGS)" -o build/k2 ./cmd/k2
	go build $(DESKTOP_TAGS) -ldflags "$(LDFLAGS)" -o build/k2s ./cmd/k2s

test:
	go test ./...

generate:
	go generate ./daemon/wintun/

# --- macOS ---
build-darwin-arm64:
	mkdir -p build
	CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build $(DESKTOP_TAGS) -ldflags "$(LDFLAGS)" -o build/k2-darwin-arm64 ./cmd/k2

build-darwin-amd64:
	mkdir -p build
	CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build $(DESKTOP_TAGS) -ldflags "$(LDFLAGS)" -o build/k2-darwin-amd64 ./cmd/k2

build-darwin-universal: build-darwin-arm64 build-darwin-amd64
	lipo -create -output build/k2-darwin-universal build/k2-darwin-arm64 build/k2-darwin-amd64
	rm -f build/k2-darwin-arm64 build/k2-darwin-amd64

# --- Windows ---
build-windows-amd64: generate
	mkdir -p build
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(DESKTOP_TAGS) -ldflags "$(LDFLAGS)" -o build/k2-windows-amd64.exe ./cmd/k2

# --- Linux ---
build-linux-amd64:
	mkdir -p build
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(DESKTOP_TAGS) -ldflags "$(LDFLAGS)" -o build/k2-linux-amd64 ./cmd/k2

build-linux-arm64:
	mkdir -p build
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(DESKTOP_TAGS) -ldflags "$(LDFLAGS)" -o build/k2-linux-arm64 ./cmd/k2

# --- Gateway (k2r) ---
build-k2r:
	mkdir -p build
	CGO_ENABLED=0 GOOS=linux go build $(K2R_TAGS) -ldflags "$(LDFLAGS)" -o build/k2r ./cmd/k2r

build-k2r-linux-amd64:
	mkdir -p build
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(K2R_TAGS) -ldflags "$(LDFLAGS)" -o build/k2r-linux-amd64 ./cmd/k2r

build-k2r-linux-arm64:
	mkdir -p build
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(K2R_TAGS) -ldflags "$(LDFLAGS)" -o build/k2r-linux-arm64 ./cmd/k2r

build-k2r-armv7:
	mkdir -p build
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build $(K2R_TAGS) -ldflags "$(LDFLAGS)" -o build/k2r-linux-armv7 ./cmd/k2r

build-k2r-mipsle:
	mkdir -p build
	CGO_ENABLED=0 GOOS=linux GOARCH=mipsle GOMIPS=softfloat go build $(K2R_TAGS) -ldflags "$(LDFLAGS)" -o build/k2r-linux-mipsle ./cmd/k2r

build-k2r-mips:
	mkdir -p build
	CGO_ENABLED=0 GOOS=linux GOARCH=mips GOMIPS=softfloat go build $(K2R_TAGS) -ldflags "$(LDFLAGS)" -o build/k2r-linux-mips ./cmd/k2r

# All k2r targets with optional UPX compression (router flash is 16-128MB):
#   make build-k2r-all UPX=1
build-k2r-all: build-k2r-linux-amd64 build-k2r-linux-arm64 build-k2r-armv7 build-k2r-mipsle build-k2r-mips
	@if [ "$(UPX)" = "1" ] && command -v upx >/dev/null 2>&1; then \
		for f in build/k2r-linux-*; do \
			echo "UPX compressing $$f..."; \
			upx --best --lzma "$$f"; \
		done; \
	elif [ "$(UPX)" = "1" ]; then \
		echo "WARN: UPX not found, skipping compression"; \
	fi
	@echo "k2r builds OK"

# --- Server (standalone) ---
build-server-darwin:
	mkdir -p build
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build $(DESKTOP_TAGS) -ldflags "$(LDFLAGS)" -o build/k2s-darwin-amd64 ./cmd/k2s
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build $(DESKTOP_TAGS) -ldflags "$(LDFLAGS)" -o build/k2s-darwin-arm64 ./cmd/k2s

build-server-windows: generate
	mkdir -p build
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(DESKTOP_TAGS) -ldflags "$(LDFLAGS)" -o build/k2s-windows-amd64.exe ./cmd/k2s

build-server-linux:
	mkdir -p build
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(DESKTOP_TAGS) -ldflags "$(LDFLAGS)" -o build/k2s-linux-amd64 ./cmd/k2s
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(DESKTOP_TAGS) -ldflags "$(LDFLAGS)" -o build/k2s-linux-arm64 ./cmd/k2s

# --- Mobile (gomobile bind) ---
appext-ios:
	mkdir -p build
	gomobile bind -tags "$(GOMOBILE_TAGS)" -target=ios -o build/K2Mobile.xcframework ./appext/

appext-macos:
	mkdir -p build
	gomobile bind -tags "$(GOMOBILE_TAGS)" -target=macos -o build/K2MobileMacOS.xcframework ./appext/

appext-android:
	mkdir -p build
	# -checklinkname=0: github.com/wlynxg/anet (transitive via pion/ice) uses
	# //go:linkname to patch net.zoneCache on Android. Go 1.23+ rejects this
	# by default. See anet v0.0.5 README. iOS target doesn't compile
	# interface_android.go so the flag is Android-only.
	gomobile bind -tags "$(GOMOBILE_TAGS)" -target=android -o build/k2mobile.aar -androidapi 24 -ldflags=-checklinkname=0 ./appext/

# --- Network degradation tests (Docker netem) ---
netem-test:
	SCENARIO=$(or $(SCENARIO),all) test/netem/run.sh

# --- Quality gates ---

# Phase 1+2: fast checks for local development (~2min)
quick-check:
	go vet ./...
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || echo "SKIP: golangci-lint not installed"
	go test -short -race -count=1 ./...

# Gateway static checks + unit tests (Linux native) or cross-compile (macOS).
# Fast gate: runs in seconds. Use gateway-uat for full integration.
gateway-check:
	GOOS=linux go vet ./gateway/...
	GOOS=linux go build ./gateway/...
	@if [ "$$(uname)" = "Linux" ]; then \
		go test -race -count=1 ./gateway/...; \
	else \
		echo "SKIP: gateway unit tests (not Linux). Use 'make gateway-uat' for Docker integration."; \
	fi

# Gateway integration test: Docker-based UAT (~5min, requires Docker).
# 10 phases, ~56 test points covering full soft-router functionality.
gateway-uat:
	test/k2r-uat/run.sh

# Full pre-release gate: CI and release builds call this single target.
pre-release: quick-check gateway-check
	go test -race -count=1 ./...
	go test -run TestContract -v ./wire/... ./engine/...
	$(MAKE) build-all-platforms

# Cross-compile all platforms (compiles = no broken imports).
build-all-platforms: build-darwin-arm64 build-linux-amd64 build-linux-arm64 build-windows-amd64 build-server-linux build-k2r-all
	@echo "All platform builds OK"

# --- Release ---
release:
	@test -n "$(VERSION)" || (echo "Usage: make VERSION=1.0.0 release"; exit 1)
	@test "$(VERSION)" != "dev" || (echo "Error: set VERSION"; exit 1)
	$(MAKE) VERSION=$(VERSION) build-server-linux build-server-darwin \
		build-linux-amd64 build-linux-arm64 build-darwin-arm64 build-darwin-amd64
	scripts/release.sh $(VERSION)

clean:
	rm -rf build/
