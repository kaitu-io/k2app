package appext

import "github.com/kaitu-io/k2/wire"

// RelayFetch sends one control-plane HTTP request to Center through a node
// (camouflage entry) and returns a {code,message,data} JSON envelope.
// gomobile-exported: string in / string out. Runs in the App process (not the
// Network Extension) — a single ordinary outbound connection, VPN-independent.
func RelayFetch(reqJSON string) string {
	return wire.RelayFetchJSON(reqJSON)
}
