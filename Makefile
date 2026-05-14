VERSION := $(shell node -p "require('./package.json').version")
K2_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "")
K2_BUILD_LOG_LEVEL ?= debug
export K2_BUILD_LOG_LEVEL  # Rust option_env!() and Vite process.env read this
export K2_COMMIT
K2_VARS  = VERSION=$(VERSION) K2_BUILD_LOG_LEVEL=$(K2_BUILD_LOG_LEVEL) K2_COMMIT=$(K2_COMMIT)
K2_BIN   = desktop/src-tauri/binaries
FEATURES ?=
TAURI_FEATURES_ARG := $(if $(FEATURES),--features $(FEATURES),)

# Capacitor 7 Android targets require JDK 21. Honor caller's JAVA_HOME if it's
# already 21.x (CI sets this via actions/setup-java); otherwise auto-detect a
# brew-installed openjdk@21 locally. Leaves system-default JDK alone so other
# projects keep their JDK 17/20/etc.
ANDROID_JAVA_HOME := $(shell \
  v() { [ -x "$$1/bin/java" ] && "$$1/bin/java" -version 2>&1 | head -1 | grep -q '"21\.'; }; \
  if [ -n "$$JAVA_HOME" ] && v "$$JAVA_HOME"; then echo "$$JAVA_HOME"; \
  elif jh=$$(/usr/libexec/java_home -v 21 2>/dev/null) && v "$$jh"; then echo "$$jh"; \
  elif v /opt/homebrew/opt/openjdk@21/libexec/openjdk.jdk/Contents/Home; then echo /opt/homebrew/opt/openjdk@21/libexec/openjdk.jdk/Contents/Home; \
  elif v /usr/local/opt/openjdk@21/libexec/openjdk.jdk/Contents/Home; then echo /usr/local/opt/openjdk@21/libexec/openjdk.jdk/Contents/Home; \
  fi)

check-jdk-21:
	@if [ -z "$(ANDROID_JAVA_HOME)" ]; then \
	  echo ""; \
	  echo "❌ JDK 21 required for Capacitor 7 Android builds, none found."; \
	  echo "   macOS: brew install openjdk@21"; \
	  echo "   Or set JAVA_HOME to an existing JDK 21 install."; \
	  echo ""; \
	  exit 1; \
	fi
	@echo "✓ Android JDK 21: $(ANDROID_JAVA_HOME)"

sync-version:
	bash scripts/sync-version.sh

pre-build: sync-version
	mkdir -p webapp/public
	echo '{"version":"$(VERSION)","commit":"$(K2_COMMIT)"}' > webapp/public/version.json

build-k2-plugin:
	cd mobile/plugins/k2-plugin && npm run build
	rm -rf node_modules/k2-plugin && yarn install --force

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

# Stage the built webapp into k2/webui/dist so the k2 binary's //go:embed
# picks it up at compile time. Called by build-linux before the Go build.
stage-k2-webui-dist: build-webapp
	@echo "--- Staging webapp into k2/webui/dist ---"
	@rm -rf k2/webui/dist
	@mkdir -p k2/webui/dist
	@rsync -a --exclude='.gitkeep' webapp/dist/ k2/webui/dist/
	@touch k2/webui/dist/.gitkeep

# --- adb tools sync ---
sync-adb-tools:
	@bash scripts/sync-adb-tools.sh

# --- Desktop builds ---
build-macos: sync-adb-tools
	bash scripts/build-macos.sh

build-macos-test:
	bash scripts/build-macos.sh --single-arch --skip-notarization --features=mcp-bridge

build-macos-sysext:
	bash scripts/build-macos.sh --ne-mode

build-macos-sysext-fast:
	bash scripts/build-macos.sh --ne-mode --skip-notarization

build-macos-sysext-test:
	bash scripts/build-macos.sh --ne-mode --single-arch --skip-notarization

simplisign-login:
	@if [ "$$(uname -s)" = "Darwin" ]; then \
		bash scripts/ci/macos/simplisign-login.sh; \
	else \
		echo "simplisign-login is macOS only, skipping"; \
	fi

build-windows: pre-build build-webapp build-k2-windows sync-adb-tools simplisign-login
	@if [ "$$(uname -s)" = "Darwin" ] || [ "$$(uname -s)" = "Linux" ]; then \
		echo "--- Cross-compiling Windows from $$(uname -s) via cargo-xwin ---"; \
		command -v cargo-xwin >/dev/null 2>&1 || { echo "ERROR: cargo-xwin not found. Install: cargo install --locked cargo-xwin"; exit 1; }; \
		cd desktop && yarn tauri build --runner cargo-xwin --target x86_64-pc-windows-msvc $(TAURI_FEATURES_ARG); \
	else \
		cd desktop && yarn tauri build --target x86_64-pc-windows-msvc $(TAURI_FEATURES_ARG); \
	fi
	@echo "--- Collecting artifacts ---"
	@mkdir -p release/$(VERSION)
	@cp desktop/src-tauri/target/x86_64-pc-windows-msvc/release/bundle/nsis/Kaitu_$(VERSION)_x64-setup.exe release/$(VERSION)/Kaitu_$(VERSION)_x64.exe
	@cp desktop/src-tauri/target/x86_64-pc-windows-msvc/release/bundle/nsis/Kaitu_$(VERSION)_x64-setup.exe.sig release/$(VERSION)/Kaitu_$(VERSION)_x64.exe.sig 2>/dev/null || true
	@echo "=== Build complete ==="
	@echo "Release artifacts in release/$(VERSION)/:"
	@ls -la release/$(VERSION)/

# --- Linux desktop build (embedded webapp, no Tauri) ---
#
# Linux ships a single Go binary with the React webapp embedded via
# //go:embed in the k2/webui package. Users open http://127.0.0.1:1777
# in their browser after installing k2 as a systemd service. macOS and
# Windows continue to use the Tauri shell; this target never touches
# desktop/src-tauri/.
#
# Builds both amd64 and arm64. Cross-compiles from macOS or Linux host
# via CGO_ENABLED=0 (sing-tun and quic-go are pure Go).
#
# Output (per arch in {amd64, arm64}):
#   release/$(VERSION)/k2-linux-$(ARCH)                       raw binary
#   release/$(VERSION)/Kaitu_$(VERSION)_linux_$(ARCH).tar.gz  bundle
#                                                             (k2 + install.sh
#                                                              + kaitu.service)
#   release/$(VERSION)/Kaitu_$(VERSION)_linux_$(ARCH).tar.gz.sha256
LINUX_ARCHES := amd64 arm64

build-linux: pre-build stage-k2-webui-dist
	@for arch in $(LINUX_ARCHES); do \
		echo "--- [host] Go cross-compile k2 for linux/$$arch (embedded webapp) ---"; \
		(cd k2 && CGO_ENABLED=0 GOOS=linux GOARCH=$$arch \
			go build \
			-tags release \
			-ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(K2_COMMIT) -X github.com/kaitu-io/k2/config.buildLogLevel=$(K2_BUILD_LOG_LEVEL)" \
			-o build/k2-linux-$$arch ./cmd/k2) || exit 1; \
		echo "--- Packaging tarball for $$arch ---"; \
		rm -rf release/$(VERSION)/linux-pkg; \
		mkdir -p release/$(VERSION)/linux-pkg; \
		cp k2/build/k2-linux-$$arch release/$(VERSION)/linux-pkg/k2; \
		cp packaging/linux/install.sh release/$(VERSION)/linux-pkg/install.sh; \
		cp packaging/linux/uninstall.sh release/$(VERSION)/linux-pkg/uninstall.sh; \
		cp packaging/linux/kaitu.service release/$(VERSION)/linux-pkg/kaitu.service; \
		chmod +x release/$(VERSION)/linux-pkg/k2 \
			release/$(VERSION)/linux-pkg/install.sh \
			release/$(VERSION)/linux-pkg/uninstall.sh; \
		(cd release/$(VERSION)/linux-pkg && \
			tar czf ../Kaitu_$(VERSION)_linux_$$arch.tar.gz k2 install.sh uninstall.sh kaitu.service) || exit 1; \
		rm -rf release/$(VERSION)/linux-pkg; \
		cp k2/build/k2-linux-$$arch release/$(VERSION)/k2-linux-$$arch; \
		echo "--- Generating SHA-256 checksum for $$arch ---"; \
		(cd release/$(VERSION) && \
			if command -v sha256sum >/dev/null 2>&1; then \
				sha256sum Kaitu_$(VERSION)_linux_$$arch.tar.gz > Kaitu_$(VERSION)_linux_$$arch.tar.gz.sha256; \
			else \
				shasum -a 256 Kaitu_$(VERSION)_linux_$$arch.tar.gz > Kaitu_$(VERSION)_linux_$$arch.tar.gz.sha256; \
			fi) || exit 1; \
	done
	@echo "=== Linux build complete ==="
	@echo "Release artifacts in release/$(VERSION)/:"
	@ls -la release/$(VERSION)/

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

dev-openwrt: pre-build
	./scripts/dev-openwrt.sh

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
	cd api && go mod tidy
	cd api/cmd && go mod tidy && GOOS=linux GOARCH=amd64 go build \
		-ldflags "-s -w -X main.Version=$(VERSION) -X main.GitCommit=$(K2_COMMIT) -X main.BuildTime=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)" \
		-o ../../release/kaitu-center .
	bash scripts/deploy-center.sh

publish-desktop:
	bash scripts/publish-desktop.sh

# k2/k2s standalone binaries (linux + darwin × amd64 + arm64)
build-k2-standalone:
	bash scripts/build-k2-standalone.sh

publish-k2:
	bash scripts/publish-k2.sh

publish-android:
	@test -n "$(VERSION)" || (echo "Usage: make publish-android VERSION=x.y.z" && exit 1)
	@echo "Publishing Android v$(VERSION)..."
	bash scripts/publish-mobile.sh $(VERSION) --platform=android

publish-ios:
	@test -n "$(VERSION)" || (echo "Usage: make publish-ios VERSION=x.y.z" && exit 1)
	@echo "Publishing iOS v$(VERSION)..."
	bash scripts/publish-mobile.sh $(VERSION) --platform=ios

# --- Mobile (delegates to k2/Makefile) ---
appext-deps:
	cd k2 && go get golang.org/x/mobile/bind@latest

# Guard: Capacitor plugin must never import gomobile symbols directly.
# If it does, gradle / podspec needs to link k2mobile.aar / K2Mobile.xcframework,
# which the plugin is not wired for. Call gomobile from the main app shell
# instead (AppDelegate.swift on iOS, MainApplication.kt on Android).
plugin-purity-check:
	@echo "[plugin-purity-check] verifying k2-plugin has no gomobile references..."
	@if grep -rn -E "(^|[^A-Za-z])appext\.|(^|[^A-Za-z])Appext[A-Z]|import[[:space:]]+appext\." mobile/plugins/k2-plugin/ 2>/dev/null | grep -v -E '/(build|node_modules|\.gradle)/'; then \
		echo ""; \
		echo "ERROR: k2-plugin is a Capacitor plugin and must stay gomobile-free."; \
		echo "       Plugin gradle/podspec does not link k2mobile.aar / K2Mobile.xcframework."; \
		echo "       Move the gomobile call to the main app (AppDelegate.swift / MainApplication.kt)."; \
		exit 1; \
	fi
	@echo "[plugin-purity-check] OK: plugin is gomobile-pure."

appext-ios: plugin-purity-check
	cd k2 && make appext-ios

appext-macos:
	cd k2 && make appext-macos

build-macos-ne-lib: appext-macos
	@echo "macOS NE xcframework ready: k2/build/K2MobileMacOS.xcframework"

appext-android build-android dev-android: export JAVA_HOME = $(ANDROID_JAVA_HOME)

appext-android: appext-deps plugin-purity-check check-jdk-21
	cd k2 && mkdir -p build && gomobile bind -tags "with_gvisor deadlock_disable release" -ldflags "-checklinkname=0" -target=android/arm64 -o build/k2mobile.aar -androidapi 24 ./appext/

build-ios: pre-build build-webapp appext-ios
	cp -r k2/build/K2Mobile.xcframework mobile/ios/App/
	rm -rf node_modules/k2-plugin && cd mobile && yarn install --force && npx cap sync ios
	cd mobile/ios/App && xcodebuild -workspace App.xcworkspace \
		-scheme App -configuration Release \
		-destination 'generic/platform=iOS' \
		-archivePath build/App.xcarchive archive

build-android: pre-build build-webapp appext-android decrypt-keystore
	mkdir -p mobile/android/app/libs
	cp k2/build/k2mobile.aar mobile/android/app/libs/
	rm -rf node_modules/k2-plugin && cd mobile && yarn install --force && npx cap sync android
	cd mobile/android && KAITU_ANDROID_STORE_PASSWORD="$$KAITU_ANDROID_STORE_PASSWORD" ./gradlew assembleRelease
	@echo "--- Collecting artifacts ---"
	@mkdir -p release/$(VERSION)
	@cp mobile/android/app/build/outputs/apk/release/app-release.apk release/$(VERSION)/Kaitu-$(VERSION).apk
	@echo "=== Build complete ==="
	@echo "Release artifacts in release/$(VERSION)/:"
	@ls -la release/$(VERSION)/Kaitu-$(VERSION).apk

decrypt-keystore:
	@if [ ! -f mobile/android/app/kaitu-release.jks ]; then \
		if [ -z "$$KAITU_ANDROID_STORE_PASSWORD" ]; then \
			echo "❌ KAITU_ANDROID_STORE_PASSWORD not set. Run: export KAITU_ANDROID_STORE_PASSWORD=<password>"; exit 1; \
		fi; \
		openssl enc -aes-256-cbc -d -pbkdf2 -iter 100000 \
			-in mobile/android/app/kaitu-release.jks.enc \
			-out mobile/android/app/kaitu-release.jks \
			-pass pass:$$KAITU_ANDROID_STORE_PASSWORD; \
		echo "Keystore decrypted."; \
	fi

IOS_DEVICE ?= $(shell xcrun xctrace list devices 2>/dev/null | grep 'iPhone' | grep -v 'Simulator' | sed 's/.*(\([0-9A-Fa-f-]\{25,\}\)).*/\1/' | head -1)

dev-ios: pre-build build-webapp appext-ios
	cp -r k2/build/K2Mobile.xcframework mobile/ios/App/
	rm -rf node_modules/k2-plugin && cd mobile && yarn install --force && npx cap sync ios && npx cap run ios $(if $(IOS_DEVICE),--target $(IOS_DEVICE),)

dev-android: pre-build build-webapp appext-android
	mkdir -p mobile/android/app/libs
	cp k2/build/k2mobile.aar mobile/android/app/libs/
	rm -rf node_modules/k2-plugin && cd mobile && yarn install --force && npx cap sync android && npx cap run android

# --- Upload artifacts to S3 (run AFTER build-*, separate to prevent accidental uploads) ---
upload-macos:
	bash scripts/ci/upload-release.sh --macos

upload-windows:
	bash scripts/ci/upload-release.sh --windows

upload-linux:
	bash scripts/ci/upload-release.sh --linux

upload-android:
	bash scripts/ci/upload-release.sh --android

# upload-web removed — Web OTA disabled due to native/webapp version mismatch risk (2026-03-22)

clean:
	rm -rf webapp/dist desktop/src-tauri/target $(K2_BIN)/k2-* build/
	cd k2 && make clean
