// Own module so both quic-go forks can be required at once: they share the
// package name `quic` but not the module path, and kuic's own go.mod cannot
// require the module it replaced.
module github.com/kaitu-io/kuic/integrationtests/apernetcompat

go 1.24

require (
	github.com/apernet/quic-go v0.57.2-0.20260111184307-eec823306178
	github.com/kaitu-io/kuic v0.0.0
	github.com/metacubex/utls v1.8.4
	github.com/stretchr/testify v1.11.1
)

require (
	github.com/andybalholm/brotli v1.0.6 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/klauspost/compress v1.17.9 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	golang.org/x/crypto v0.41.0 // indirect
	golang.org/x/exp v0.0.0-20240904232852-e7e105dedf7e // indirect
	golang.org/x/net v0.43.0 // indirect
	golang.org/x/sys v0.35.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/kaitu-io/kuic => ../..
