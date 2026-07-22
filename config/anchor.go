package config

// RouterAnchorIP is the well-known virtual anchor address the client app
// uses to discover and control a k2r router, regardless of how many NAT
// layers sit between the app and the router. Cross-repo contract — do not
// change this value without also updating:
//   - k2app webapp's ROUTER_ANCHOR constant + B4/B5 SSRF gates
//   - gateway/anchor.go (k2r's PREROUTING DNAT rule that redirects forwarded
//     traffic addressed to this IP:RouterAnchorPort to the local control port)
//   - engine/engine.go's built-in anchor-direct route entry (keeps anchor
//     traffic off the tunnel even in Global/match-all proxy mode)
//
// See docs/superpowers/specs/2026-07-17-k2r-headless-app-control-design.md
// §3.4 (anchor intercept) and §10.4 (TUN conclusion + fix) in the k2app repo.
const RouterAnchorIP = "10.17.79.1"

// RouterAnchorPort is k2r's control-plane HTTP port, reachable via the
// anchor address. Fixed — not derived from GatewayConfig.Listen, which is
// theoretically operator-overridable; the anchor path always targets this
// well-known port.
const RouterAnchorPort = 1779
