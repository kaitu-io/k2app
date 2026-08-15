module github.com/kaitu-io/tessera

go 1.24

require (
	github.com/apernet/quic-go v0.57.2-0.20260111184307-eec823306178
	github.com/metacubex/utls v1.8.4
)

require (
	github.com/andybalholm/brotli v1.0.6 // indirect
	github.com/klauspost/compress v1.17.9 // indirect
	golang.org/x/crypto v0.41.0 // indirect
	golang.org/x/exp v0.0.0-20240904232852-e7e105dedf7e // indirect
	golang.org/x/net v0.43.0 // indirect
	golang.org/x/sys v0.35.0 // indirect
)

// The seam that lets a caller supply the client's TLS engine does not exist
// upstream. patches/quic-go-utls-seam.patch adds it; scripts/fork-quic-go.sh
// materializes the patched tree here. Keep the version above in sync with the
// one pinned in that script.
replace github.com/apernet/quic-go => ./.fork/quic-go
