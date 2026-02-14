VERSION := $(shell node -p "require('./package.json').version")
COMMIT  := $(shell cd k2 && git rev-parse --short HEAD)

pre-build:
	mkdir -p webapp/public
	echo '{"version":"$(VERSION)"}' > webapp/public/version.json

build-k2:
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

dev: pre-build
	./scripts/dev.sh

publish-release:
	bash scripts/publish-release.sh

clean:
	rm -rf webapp/dist desktop/src-tauri/target desktop/src-tauri/binaries/k2-*
