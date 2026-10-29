package appext

import "github.com/kaitu-io/k2/wire"

// RelayFetch sends one control-plane HTTP request to Center through a
// camouflage node and returns a {code,message,data} JSON envelope. The request
// is node-less ({centerHost,method,path,headers,body}); the wire RelayManager
// owns node selection, sequential failover, and keep-alive connection reuse.
// gomobile-exported: string in / string out. Runs in the App process (not the
// Network Extension) — VPN-independent. Feed nodes in via RelayAddNodes first.
func RelayFetch(reqJSON string) string {
	return wire.RelayFetchJSON(reqJSON)
}

// RelayAddNodes incrementally registers camouflage-node descriptors with the
// RelayManager (webapp relay-add-nodes). Input is a JSON array of {ip,port?,pin,
// ech,score?}. Idempotent, deduped by IP, known nodes keep their health.
// gomobile-exported: string in / string out.
func RelayAddNodes(nodesJSON string) string {
	return wire.RelayAddNodesJSON(nodesJSON)
}
