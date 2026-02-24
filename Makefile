.PHONY: build test generate build-macos build-windows build-linux appext-ios appext-android

build: generate
	go build -o build/k2 ./cmd/k2
	go build -o build/k2s ./cmd/k2s

test:
	go test ./...

generate:
	go generate ./daemon/wintun/

build-macos:
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o build/k2-darwin-amd64 ./cmd/k2
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o build/k2-darwin-arm64 ./cmd/k2
	lipo -create -output build/k2-darwin build/k2-darwin-amd64 build/k2-darwin-arm64
	rm build/k2-darwin-amd64 build/k2-darwin-arm64
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o build/k2s-darwin-amd64 ./cmd/k2s
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o build/k2s-darwin-arm64 ./cmd/k2s
	lipo -create -output build/k2s-darwin build/k2s-darwin-amd64 build/k2s-darwin-arm64
	rm build/k2s-darwin-amd64 build/k2s-darwin-arm64

build-windows: generate
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o build/k2.exe ./cmd/k2
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o build/k2s.exe ./cmd/k2s

build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o build/k2-linux ./cmd/k2
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o build/k2s-linux ./cmd/k2s

appext-ios:
	gomobile bind -target=ios -o build/K2Mobile.xcframework ./appext/

appext-android:
	gomobile bind -target=android -o build/k2mobile.aar -androidapi 21 ./appext/
