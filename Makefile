VERSION := $(shell node -p "require('./package.json').version")
K2_VARS  = VERSION=$(VERSION)
K2_BIN   = desktop/src-tauri/binaries

pre-build:
	mkdir -p webapp/public
	echo '{"version":"$(VERSION)"}' > webapp/public/version.json

build-webapp:
	cd webapp && yarn build

# --- k2 sidecar (delegates to k2/Makefile) ---
build-k2-macos:
	cd k2 && make build-darwin-universal $(K2_VARS)
	cp k2/build/k2-darwin-universal $(K2_BIN)/k2-universal-apple-darwin

build-k2-windows:
	cd k2 && make build-windows-amd64 $(K2_VARS)
	cp k2/build/k2-windows-amd64.exe $(K2_BIN)/k2-x86_64-pc-windows-msvc.exe

build-k2-linux:
	cd k2 && make build-linux-amd64 $(K2_VARS)
	cp k2/build/k2-linux-amd64 $(K2_BIN)/k2-x86_64-unknown-linux-gnu

# --- Desktop builds ---
build-macos:
	bash scripts/build-macos.sh

build-macos-fast:
	bash scripts/build-macos.sh --skip-notarization

build-macos-test:
	bash scripts/build-macos.sh --single-arch --skip-notarization --features=mcp-bridge

build-macos-sysext:
	bash scripts/build-macos.sh --ne-mode

build-macos-sysext-fast:
	bash scripts/build-macos.sh --ne-mode --skip-notarization

build-macos-sysext-test:
	bash scripts/build-macos.sh --ne-mode --single-arch --skip-notarization

build-windows: pre-build build-webapp build-k2-windows
	cd desktop && yarn tauri build --target x86_64-pc-windows-msvc

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

dev: pre-build
	./scripts/dev.sh

dev-desktop: pre-build
	./scripts/dev-desktop.sh

# Slave node Docker images (build + push to ECR)
publish-docker:
	bash scripts/publish-docker.sh

# API server
deploy-api:
	mkdir -p release
	cd api/cmd && GOOS=linux GOARCH=amd64 go build -o ../../release/kaitu-center .
	bash scripts/deploy-center.sh

publish-release:
	bash scripts/publish-release.sh

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
mobile-deps:
	cd k2 && go get golang.org/x/mobile/bind@latest

mobile-ios:
	cd k2 && make appext-ios

mobile-macos:
	cd k2 && make appext-macos

build-macos-ne-lib: mobile-macos
	@echo "macOS NE xcframework ready: k2/build/K2MobileMacOS.xcframework"

mobile-android: mobile-deps
	cd k2 && make appext-android

build-mobile-ios: pre-build build-webapp mobile-ios
	cp -r k2/build/K2Mobile.xcframework mobile/ios/App/
	cd mobile && npx cap sync ios
	cd mobile/ios/App && xcodebuild -workspace App.xcworkspace \
		-scheme App -configuration Release \
		-destination 'generic/platform=iOS' \
		-archivePath build/App.xcarchive archive

build-mobile-android: pre-build build-webapp mobile-android
	mkdir -p mobile/android/app/libs
	cp k2/build/k2mobile.aar mobile/android/app/libs/
	cd mobile && npx cap sync android
	cd mobile/android && ./gradlew assembleRelease

dev-ios: pre-build build-webapp
	cd mobile && npx cap sync ios && npx cap run ios

dev-android: pre-build build-webapp mobile-android
	mkdir -p mobile/android/app/libs
	cp k2/build/k2mobile.aar mobile/android/app/libs/
	cd mobile && npx cap sync android && npx cap run android

clean:
	rm -rf webapp/dist desktop/src-tauri/target $(K2_BIN)/k2-* build/
	cd k2 && make clean
