VERSION := $(shell node -p "require('./package.json').version")
COMMIT  := $(shell cd k2 && git rev-parse --short HEAD)

pre-build:
	mkdir -p webapp/public
	echo '{"version":"$(VERSION)"}' > webapp/public/version.json

build-k2:
	cd k2 && go generate ./wintun/ ./cloud/
	cd k2 && go build -tags nowebapp \
		-ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT)" \
		-o ../desktop/src-tauri/binaries/k2-$(TARGET) ./cmd/k2

build-webapp:
	cd webapp && yarn build

build-macos:
	bash scripts/build-macos.sh

build-macos-fast:
	bash scripts/build-macos.sh --skip-notarization

build-windows: pre-build build-webapp
	$(MAKE) build-k2 TARGET=x86_64-pc-windows-msvc
	cd desktop && yarn tauri build --target x86_64-pc-windows-msvc

build-openwrt: pre-build
	bash scripts/build-openwrt.sh

dev: pre-build
	./scripts/dev.sh

# API server
build-api:
	cd api/cmd && go build -o kaitu-center .

deploy-api: build-api
	bash scripts/deploy-center.sh

publish-release:
	bash scripts/publish-release.sh

# Mobile builds
mobile-deps:
	cd k2 && go get golang.org/x/mobile/bind@latest

mobile-ios: mobile-deps
	mkdir -p k2/build
	cd k2 && gomobile bind -target=ios -o build/K2Mobile.xcframework ./mobile/

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
	rm -rf webapp/dist desktop/src-tauri/target desktop/src-tauri/binaries/k2-*
