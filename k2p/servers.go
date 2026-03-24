package k2p

// DefaultSTUNServers returns a curated list of reliable public STUN servers.
// Ordered by expected reliability. At least 2 are needed for NAT classification.
func DefaultSTUNServers() []string {
	return []string{
		"stun.l.google.com:19302",
		"stun1.l.google.com:19302",
		"stun2.l.google.com:19302",
		"stun.cloudflare.com:3478",
		"stun.nextcloud.com:443",
		"stun.syncthing.net:3478",
		"stun.miwifi.com:3478",
		"stun.chat.bilibili.com:3478",
	}
}
