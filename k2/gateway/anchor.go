//go:build linux

package gateway

import (
	"fmt"
	"net"
	"strconv"

	"github.com/google/nftables"
	"github.com/google/nftables/binaryutil"
	"github.com/google/nftables/expr"
	"github.com/kaitu-io/k2/config"
	"golang.org/x/sys/unix"
)

// Anchor DNAT intercept — 2026-07-18 spec revision (anchor-only discovery
// for k2r router-control): the client app's *sole* path to discover/control
// a k2r router, regardless of how many NAT layers separate them, is the
// well-known anchor address http://<config.RouterAnchorIP>:<RouterAnchorPort>.
// k2r sits on the forwarding path, so it PREROUTING-REDIRECTs any *forwarded*
// TCP traffic addressed to anchor:port to its own local control port. REDIRECT
// (not plain DNAT) is used so the kernel auto-handles the return path, exactly
// like the existing DNS REDIRECT rules below.
//
// This rule is unconditional — independent of LANSubnets/DNSRedirect/
// AllowlistMode — so it is always installed by every backend's Install(),
// never gated behind cfg fields. It follows the same lifecycle as the rest
// of the interceptor: installed in Install(), torn down in Remove(), and
// idempotent across repeated Install() calls / daemon crash+restart via the
// same conventions the DNS-redirect chains already rely on:
//   - iptables: a K2R-prefixed chain (K2R_ANCHOR) — automatically discovered
//     and cleaned by cleanK2RFamily's generic "K2R*" scan, so Install()'s
//     preflight Remove() (and doDown) tear it down for free.
//   - nftables (shell + netlink): lives inside the single `inet k2r` table,
//     which both backends delete wholesale on Remove() — no separate
//     lifecycle needed.

// anchorIptCommands returns the iptables commands that create the
// K2R_ANCHOR nat chain and jump PREROUTING traffic into it. IPv4 only — the
// anchor address is not dual-stack. Appended unconditionally by
// buildIptCommands.
func anchorIptCommands() []cmdSpec {
	port := strconv.Itoa(config.RouterAnchorPort)
	return []cmdSpec{
		{"iptables", ipt("-t", "nat", "-N", "K2R_ANCHOR")},
		{"iptables", ipt("-t", "nat", "-A", "K2R_ANCHOR",
			"-d", config.RouterAnchorIP, "-p", "tcp", "--dport", port,
			"-j", "REDIRECT", "--to-ports", port)},
		{"iptables", ipt("-t", "nat", "-A", "PREROUTING", "-j", "K2R_ANCHOR")},
	}
}

// anchorNftLines returns the nft-shell script lines that create the anchor
// NAT chain (hooked at prerouting, NAT-dest priority — REDIRECT is a NAT
// action, so this cannot live in the mangle-table `prerouting` filter chain
// used for TPROXY). Appended unconditionally by nftShellInterceptor.buildScript.
func anchorNftLines() []string {
	return []string{
		"add chain inet k2r anchor { type nat hook prerouting priority dstnat ; policy accept ; }",
		fmt.Sprintf("add rule inet k2r anchor ip daddr %s tcp dport %d redirect to :%d",
			config.RouterAnchorIP, config.RouterAnchorPort, config.RouterAnchorPort),
	}
}

// addAnchorRedirect installs the netlink-backend equivalent of
// anchorNftLines/anchorIptCommands: a NAT-hook chain matching forwarded TCP
// traffic to the anchor IP:port, REDIRECTed to the local control port.
// Called unconditionally from nftNetlinkInterceptor.Install() while n.mu is
// held. Torn down implicitly by Remove()'s DelTable(n.table).
func (n *nftNetlinkInterceptor) addAnchorRedirect() {
	anchorChain := n.conn.AddChain(&nftables.Chain{
		Name:     "anchor",
		Table:    n.table,
		Type:     nftables.ChainTypeNAT,
		Hooknum:  nftables.ChainHookPrerouting,
		Priority: nftables.ChainPriorityNATDest,
	})

	anchorIP := net.ParseIP(config.RouterAnchorIP).To4()
	anchorPort := uint16(config.RouterAnchorPort)

	n.conn.AddRule(&nftables.Rule{
		Table: n.table, Chain: anchorChain,
		Exprs: []expr.Any{
			// dst addr == anchor IP (IPv4 header offset 16, len 4).
			&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 16, Len: 4},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: anchorIP},
			// l4proto == tcp.
			&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{unix.IPPROTO_TCP}},
			// dst port == anchor port.
			&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: 2, Len: 2},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: binaryutil.BigEndian.PutUint16(anchorPort)},
			// redirect to :anchorPort (local control port).
			&expr.Immediate{Register: 1, Data: binaryutil.BigEndian.PutUint16(anchorPort)},
			&expr.Redir{RegisterProtoMin: 1},
		},
	})
}
