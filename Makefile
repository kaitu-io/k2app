VERSION := $(shell node -p "require('./package.json').version")
K2_VARS  = VERSION=$(VERSION)
K2_BIN   = desktop/src-tauri/binaries
FEATURES ?=
TAURI_FEATURES_ARG := $(if $(FEATURES),--features $(FEATURES),)

sync-version:
	bash scripts/sync-version.sh

pre-build: sync-version
	mkdir -p webapp/public
	echo '{"version":"$(VERSION)"}' > webapp/public/version.json

build-k2-plugin:
	cd mobile/plugins/k2-plugin && npm run build

build-webapp: build-k2-plugin
	cd webapp && yarn build

# --- k2 sidecar (delegates to k2/Makefile) ---
build-k2-macos:
	cd k2 && make build-darwin-universal $(K2_VARS)
	mkdir -p $(K2_BIN)
	cp k2/build/k2-darwin-universal $(K2_BIN)/k2-universal-apple-darwin

build-k2-windows:
	cd k2/daemon/wintun && go run gen.go
	cd k2 && make build-windows-amd64 $(K2_VARS)
	mkdir -p $(K2_BIN)
	cp k2/build/k2-windows-amd64.exe $(K2_BIN)/k2-x86_64-pc-windows-msvc.exe

build-k2-linux:
	cd k2 && make build-linux-amd64 $(K2_VARS)
	mkdir -p $(K2_BIN)
	cp k2/build/k2-linux-amd64 $(K2_BIN)/k2-x86_64-unknown-linux-gnu

# --- Desktop builds ---
build-macos:
	bash scripts/build-macos.sh

build-macos-test:
	bash scripts/build-macos.sh --single-arch --skip-notarization --features=mcp-bridge

build-macos-sysext:
	bash scripts/build-macos.sh --ne-mode

build-macos-sysext-fast:
	bash scripts/build-macos.sh --ne-mode --skip-notarization

build-macos-sysext-test:
	bash scripts/build-macos.sh --ne-mode --single-arch --skip-notarization

build-windows: pre-build build-webapp build-k2-windows
	@if [ "$$(uname -s 2>/dev/null)" = "Darwin" ] || [ "$$(uname -s 2>/dev/null)" = "Linux" ]; then \
		echo "ERROR: build-windows requires a Windows host (ring crate needs MSVC). Use 'make build-windows-test' to delegate to Windows VM."; exit 1; fi
	cd desktop && yarn tauri build --target x86_64-pc-windows-msvc $(TAURI_FEATURES_ARG)
	@echo "--- Collecting artifacts ---"
	@mkdir -p release/$(VERSION)
	@cp desktop/src-tauri/target/x86_64-pc-windows-msvc/release/bundle/nsis/Kaitu_$(VERSION)_x64-setup.exe release/$(VERSION)/
	@cp desktop/src-tauri/target/x86_64-pc-windows-msvc/release/bundle/nsis/Kaitu_$(VERSION)_x64-setup.exe.sig release/$(VERSION)/ 2>/dev/null || true
	@echo "=== Build complete ==="
	@echo "Release artifacts in release/$(VERSION)/:"
	@ls -la release/$(VERSION)/

build-windows-test:
	bash scripts/build-windows-test.sh

build-openwrt: pre-build
	bash scripts/build-openwrt.sh

# OpenWrt Docker testing (single arch, fast)
# Override: make run-openwrt OPENWRT_PORT=11777 DOCKER_IMAGE=redis:7-alpine
DOCKER_GOARCH ?= $(shell go env GOARCH)
DOCKER_IMAGE  ?= alpine:latest
OPENWRT_PORT  ?= 11777
OPENWRT_BIN    = build/k2-linux-$(DOCKER_GOARCH)

build-openwrt-docker: pre-build
	cd k2 && make build-linux-$(DOCKER_GOARCH) $(K2_VARS)
	mkdir -p build
	cp k2/build/k2-linux-$(DOCKER_GOARCH) $(OPENWRT_BIN)
	@echo "Built $(OPENWRT_BIN)"

run-openwrt: build-openwrt-docker
	@echo "Starting k2 daemon at http://localhost:$(OPENWRT_PORT)"
	docker run --rm -it -p $(OPENWRT_PORT):1777 \
		--entrypoint "" \
		-v $(PWD)/$(OPENWRT_BIN):/usr/bin/k2:ro \
		$(DOCKER_IMAGE) /usr/bin/k2 run -l 0.0.0.0:1777

test-openwrt: build-openwrt-docker
	OPENWRT_PORT=$(OPENWRT_PORT) DOCKER_IMAGE=$(DOCKER_IMAGE) \
		bash scripts/test-openwrt.sh $(OPENWRT_BIN)

dev-standalone: pre-build
	./scripts/dev-standalone.sh

dev-macos: pre-build
	./scripts/dev-macos.sh

dev-windows: pre-build
	./scripts/dev-windows.sh

# Slave node Docker images (build + push to ECR)
publish-docker:
	bash scripts/publish-docker.sh

# API server
deploy-api:
	mkdir -p release
	cd api/cmd && GOOS=linux GOARCH=amd64 go build -o ../../release/kaitu-center .
	bash scripts/deploy-center.sh

publish-desktop:
	bash scripts/publish-desktop.sh

# k2/k2s standalone binaries (linux + darwin × amd64 + arm64)
build-k2-standalone:
	bash scripts/build-k2-standalone.sh

publish-k2:
	bash scripts/publish-k2.sh

publish-mobile:
	@test -n "$(VERSION)" || (echo "Usage: make publish-mobile VERSION=x.y.z" && exit 1)
	@echo "Publishing mobile v$(VERSION)..."
	bash scripts/publish-mobile.sh $(VERSION)

# --- Mobile (delegates to k2/Makefile) ---
appext-deps:
	cd k2 && go get golang.org/x/mobile/bind@latest

appext-ios:
	cd k2 && make appext-ios

appext-macos:
	cd k2 && make appext-macos

build-macos-ne-lib: appext-macos
	@echo "macOS NE xcframework ready: k2/build/K2MobileMacOS.xcframework"

appext-android: appext-deps
	cd k2 && make appext-android

build-mobile-ios: pre-build build-webapp appext-ios
	cp -r k2/build/K2Mobile.xcframework mobile/ios/App/
	cd mobile && rm -rf node_modules/k2-plugin && yarn install --force && npx cap sync ios
	cd mobile/ios/App && xcodebuild -workspace App.xcworkspace \
		-scheme App -configuration Release \
		-destination 'generic/platform=iOS' \
		-archivePath build/App.xcarchive archive

build-mobile-android: pre-build build-webapp appext-android
	mkdir -p mobile/android/app/libs
	cp k2/build/k2mobile.aar mobile/android/app/libs/
	cd mobile && rm -rf node_modules/k2-plugin && yarn install --force && npx cap sync android
	cd mobile/android && ./gradlew assembleRelease

IOS_DEVICE ?= $(shell xcrun xctrace list devices 2>/dev/null | grep 'iPhone' | grep -v 'Simulator' | sed 's/.*(\([0-9A-Fa-f-]\{25,\}\)).*/\1/' | head -1)

dev-ios: pre-build build-webapp appext-ios
	cp -r k2/build/K2Mobile.xcframework mobile/ios/App/
	cd mobile && rm -rf node_modules/k2-plugin && yarn install --force && npx cap sync ios && npx cap run ios $(if $(IOS_DEVICE),--target $(IOS_DEVICE),)

dev-android: pre-build build-webapp appext-android
	mkdir -p mobile/android/app/libs
	cp k2/build/k2mobile.aar mobile/android/app/libs/
	cd mobile && rm -rf node_modules/k2-plugin && yarn install --force && npx cap sync android && npx cap run android

clean:
	rm -rf webapp/dist desktop/src-tauri/target $(K2_BIN)/k2-* build/
	cd k2 && make clean
