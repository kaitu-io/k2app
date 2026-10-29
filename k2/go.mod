module github.com/kaitu-io/k2

go 1.26.0

toolchain go1.26.10

require (
	github.com/coder/websocket v1.8.14
	github.com/electricbubble/gadb v0.1.0
	github.com/google/nftables v0.3.0
	github.com/gorilla/websocket v1.5.3
	github.com/kaitu-io/k2-rules v0.1.4
	github.com/metacubex/utls v1.8.4
	github.com/miekg/dns v1.1.72
	github.com/pion/ice/v4 v4.2.2
	github.com/pion/stun/v3 v3.1.5
	github.com/pion/turn/v4 v4.1.4
	github.com/sagernet/sing v0.7.6
	github.com/sagernet/sing-tun v0.7.13
	github.com/sasha-s/go-deadlock v0.3.7
	github.com/xtaci/smux/v2 v2.1.0
	go.uber.org/goleak v1.3.0
	golang.org/x/crypto v0.58.0
	golang.org/x/mobile v0.0.0-20260529142300-ecb4cd65260a
	golang.org/x/net v0.60.0
	golang.org/x/sync v0.24.0
	golang.org/x/sys v0.49.0
	gopkg.in/lumberjack.v2 v2.0.0
	gopkg.in/yaml.v3 v3.0.1
)

require aead.dev/minisign v0.3.0

require (
	github.com/andybalholm/brotli v1.2.7 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/fsnotify/fsnotify v1.7.0 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/google/go-cmp v0.6.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/kaitu-io/kuic v0.0.0
	github.com/klauspost/compress v1.18.7 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/mdlayher/netlink v1.7.3-0.20250113171957-fbb4dce95f42 // indirect
	github.com/mdlayher/socket v0.5.0 // indirect
	github.com/petermattis/goid v0.0.0-20250813065127-a731cc31b4fe // indirect
	github.com/pion/dtls/v3 v3.1.4 // indirect
	github.com/pion/logging v0.2.4 // indirect
	github.com/pion/mdns/v2 v2.1.0 // indirect
	github.com/pion/randutil v0.1.0 // indirect
	github.com/pion/transport/v4 v4.0.2 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/sagernet/fswatch v0.1.1 // indirect
	github.com/sagernet/gvisor v0.0.0-20241123041152-536d05261cff // indirect
	github.com/sagernet/netlink v0.0.0-20240612041022-b9a21c07ac6a // indirect
	github.com/sagernet/nftables v0.3.0-beta.4 // indirect
	github.com/vishvananda/netns v0.0.4 // indirect
	github.com/wlynxg/anet v0.0.5 // indirect
	go4.org/netipx v0.0.0-20231129151722-fdeea329fbba // indirect
	golang.org/x/exp v0.0.0-20240904232852-e7e105dedf7e // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	golang.org/x/time v0.14.0 // indirect
	golang.org/x/tools v0.51.0 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
)

replace github.com/kaitu-io/kuic => ../third_party/kuic

replace github.com/xtaci/smux/v2 => ../third_party/smux
