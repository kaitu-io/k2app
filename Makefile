.PHONY: build test mobile-ios mobile-android mobile

build:
	go build -o k2 ./cmd/k2
	go build -o k2s ./cmd/k2s

test:
	go test ./...

mobile-ios:
	gomobile bind -target=ios -o build/K2Mobile.xcframework ./mobile/

mobile-android:
	gomobile bind -target=android -o build/k2mobile.aar -androidapi 21 ./mobile/

mobile: mobile-ios mobile-android
