.PHONY: build test generate build-windows build-linux build-nowebapp mobile-ios mobile-android mobile appext-ios appext-android

build: generate
	go build -o build/k2 ./cmd/k2
	go build -o build/k2s ./cmd/k2s

test:
	go test ./...

generate:
	go generate ./wintun/
	go generate ./cloud/

build-windows: generate
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o build/k2.exe ./cmd/k2
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o build/k2s.exe ./cmd/k2s

build-linux: generate
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o build/k2-linux ./cmd/k2
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o build/k2s-linux ./cmd/k2s

build-nowebapp:
	go build -tags nowebapp -o build/k2-nowebapp ./cmd/k2

mobile-ios: appext-ios
mobile-android: appext-android
mobile: appext-ios appext-android

appext-ios:
	gomobile bind -target=ios -o build/K2Mobile.xcframework ./appext/

appext-android:
	gomobile bind -target=android -o build/k2mobile.aar -androidapi 21 ./appext/
