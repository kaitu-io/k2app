VERSION := $(shell node -p "require('./package.json').version")
COMMIT  := $(shell cd k2 && git rev-parse --short HEAD)

pre-build:
	mkdir -p webapp/public
	echo '{"version":"$(VERSION)"}' > webapp/public/version.json

build-k2:
	cd k2 && go generate ./wintun/ ./cloud/
	cd k2 && go build -tags nowebapp \
		-ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)" \
		-o ../desktop/src-tauri/binaries/k2-$(TARGET) ./cmd/k2

build-webapp:
	cd webapp && yarn build

build-macos:
	bash scripts/build-macos.sh

build-macos-fast:
	bash scripts/build-macos.sh --skip-notarization

build-macos-test:
	bash scripts/build-macos.sh --skip-notarization --single-arch --features=mcp-bridge

build-macos-test-notarized:
	bash scripts/build-macos.sh --single-arch --features=mcp-bridge

build-windows: pre-build build-webapp
	$(MAKE) build-k2 TARGET=x86_64-pc-windows-msvc
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
	cd webapp && npx vite build
	rm -rf k2/cloud/dist && cp -r webapp/dist k2/cloud/dist
	mkdir -p build
	CGO_ENABLED=0 GOOS=linux GOARCH=$(DOCKER_GOARCH) \
		go build -C k2 \
		-ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)" \
		-o ../$(OPENWRT_BIN) ./cmd/k2
	git -C k2 checkout -- cloud/dist/
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

# Mobile builds
mobile-deps:
	cd k2 && go get golang.org/x/mobile/bind@latest

mobile-ios: mobile-deps
	mkdir -p k2/build
	cd k2 && gomobile bind -target=ios -o build/K2Mobile.xcframework ./mobile/

mobile-macos: mobile-deps
	mkdir -p k2/build
	cd k2 && gomobile bind -target=macos -o build/K2MobileMacOS.xcframework ./mobile/

build-macos-ne-lib: mobile-macos
	@echo "macOS NE xcframework ready: k2/build/K2MobileMacOS.xcframework"

mobile-android: mobile-deps
	mkdir -p k2/build
	cd k2 && gomobile bind -target=android -o build/k2mobile.aar -androidapi 24 ./mobile/

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
	rm -rf webapp/dist desktop/src-tauri/target desktop/src-tauri/binaries/k2-* build/
