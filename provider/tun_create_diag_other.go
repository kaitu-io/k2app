//go:build (darwin && !ios) || (linux && !android)

package provider

// startTUNCreateDiag is Windows-only (wintun); see tun_create_diag.go.
func startTUNCreateDiag(string) func(error) { return func(error) {} }
