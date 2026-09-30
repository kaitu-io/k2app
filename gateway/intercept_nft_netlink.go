//go:build linux

package gateway

import (
	"fmt"
	"log/slog"
	"net"

	"github.com/google/nftables"
	"github.com/google/nftables/binaryutil"
	"github.com/google/nftables/expr"
	"github.com/sasha-s/go-deadlock"
	"golang.org/x/sys/unix"
)

// nftNetlinkInterceptor manages nftables rules via the netlink API.
// mu is a serialization lock protecting the nftables.Conn (not goroutine-safe).
// All methods that touch conn/table/chain/macSet must hold mu.
type nftNetlinkInterceptor struct {
	mu        deadlock.Mutex // serialization: nftables.Conn access
	conn      *nftables.Conn
	table     *nftables.Table
	chain     *nftables.Chain
	macSet    *nftables.Set
	installed bool
}

func newNftNetlinkInterceptor() (*nftNetlinkInterceptor, error) {
	conn, err := nftables.New()
	if err != nil {
		return nil, fmt.Errorf("nftables netlink init: %w", err)
	}
	if _, err := conn.ListTables(); err != nil {
		return nil, fmt.Errorf("nftables netlink probe: %w", err)
	}
	return &nftNetlinkInterceptor{conn: conn}, nil
}

func (n *nftNetlinkInterceptor) Name() string { return "nftables-netlink" }

func (n *nftNetlinkInterceptor) Install(cfg InterceptConfig) error {
	n.mu.Lock()
	// concurrency:allow linear nft table setup — only operates on n.conn/n.table/n.chain fields
	defer n.mu.Unlock()

	// Delete stale table if exists (idempotent cleanup)
	n.conn.DelTable(&nftables.Table{Family: nftables.TableFamilyINet, Name: "k2r"})
	_ = n.conn.Flush()

	// Create table inet k2r
	n.table = n.conn.AddTable(&nftables.Table{
		Family: nftables.TableFamilyINet,
		Name:   "k2r",
	})

	// Create prerouting chain
	n.chain = n.conn.AddChain(&nftables.Chain{
		Name:     "prerouting",
		Table:    n.table,
		Type:     nftables.ChainTypeFilter,
		Hooknum:  nftables.ChainHookPrerouting,
		Priority: nftables.ChainPriorityMangle,
	})

	n.addAnchorRedirect()
	n.addLoopbackBypass()
	n.addExcludeRules(cfg.ExcludeIPs)

	if cfg.AllowlistMode && len(cfg.AllowedMACs) > 0 {
		n.addMACAllowlist(cfg.AllowedMACs)
	}

	n.addLinkScopeExempt(cfg.LANSubnets)

	if cfg.DNSRedirect && cfg.DNSPort > 0 {
		n.addDNSRedirect(cfg, n.table)
		n.addDNSRedirectOutput(cfg, n.table)
		n.addDNSExemptFromTPROXY()
	}

	n.addTPROXYRules(cfg)

	if err := n.conn.Flush(); err != nil {
		return fmt.Errorf("nftables flush: %w", err)
	}

	setupIPRules()
	n.installed = true
	slog.Info("DIAG: gw-intercept-install", "backend", "nftables-netlink",
		"subnets", cfg.LANSubnets, "excludeCount", len(cfg.ExcludeIPs),
		"dnsRedirect", cfg.DNSRedirect, "allowlistMode", cfg.AllowlistMode)
	return nil
}

// Remove deletes the table by name, not through n.table: an interceptor that
// never installed anything must still clear what a dead daemon left
// (RemoveInterception). Deleting a table that is not there fails the flush,
// nothing else.
func (n *nftNetlinkInterceptor) Remove() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.conn.DelTable(&nftables.Table{Family: nftables.TableFamilyINet, Name: "k2r"})
	_ = n.conn.Flush()
	cleanupIPRules()
	n.installed = false
	n.table = nil
	n.chain = nil
	n.macSet = nil
	slog.Info("gateway: nftables-netlink rules cleaned up")
}

// --- Rule builders ---

func (n *nftNetlinkInterceptor) addLoopbackBypass() {
	n.conn.AddRule(&nftables.Rule{
		Table: n.table,
		Chain: n.chain,
		Exprs: []expr.Any{
			&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: padString("lo", 16)},
			&expr.Verdict{Kind: expr.VerdictReturn},
		},
	})
}

func (n *nftNetlinkInterceptor) addExcludeRules(excludeIPs []string) {
	v4, v6 := splitIPsByFamily(excludeIPs)

	if len(v4) > 0 {
		set := &nftables.Set{Table: n.table, Name: "exclude_v4", KeyType: nftables.TypeIPAddr}
		var elements []nftables.SetElement
		for _, ip := range v4 {
			elements = append(elements, nftables.SetElement{Key: net.ParseIP(ip).To4()})
		}
		n.conn.AddSet(set, elements)
		n.conn.AddRule(&nftables.Rule{
			Table: n.table, Chain: n.chain,
			Exprs: []expr.Any{
				&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 16, Len: 4},
				&expr.Lookup{SourceRegister: 1, SetName: "exclude_v4", SetID: set.ID},
				&expr.Verdict{Kind: expr.VerdictReturn},
			},
		})
	}

	if len(v6) > 0 {
		set := &nftables.Set{Table: n.table, Name: "exclude_v6", KeyType: nftables.TypeIP6Addr}
		var elements []nftables.SetElement
		for _, ip := range v6 {
			elements = append(elements, nftables.SetElement{Key: net.ParseIP(ip).To16()})
		}
		n.conn.AddSet(set, elements)
		n.conn.AddRule(&nftables.Rule{
			Table: n.table, Chain: n.chain,
			Exprs: []expr.Any{
				&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 24, Len: 16},
				&expr.Lookup{SourceRegister: 1, SetName: "exclude_v6", SetID: set.ID},
				&expr.Verdict{Kind: expr.VerdictReturn},
			},
		})
	}
}

func (n *nftNetlinkInterceptor) addMACAllowlist(macs []string) {
	n.macSet = &nftables.Set{Table: n.table, Name: "allowed_router_devices", KeyType: nftables.TypeEtherAddr}
	var elements []nftables.SetElement
	for _, macStr := range macs {
		mac, err := net.ParseMAC(macStr)
		if err != nil {
			slog.Warn("gateway: invalid MAC in allowlist", "mac", macStr, "err", err)
			continue
		}
		elements = append(elements, nftables.SetElement{Key: mac})
	}
	n.conn.AddSet(n.macSet, elements)

	// Skip non-Ethernet traffic
	n.conn.AddRule(&nftables.Rule{
		Table: n.table, Chain: n.chain,
		Exprs: []expr.Any{
			&expr.Meta{Key: expr.MetaKeyIIFTYPE, Register: 1},
			&expr.Cmp{Op: expr.CmpOpNeq, Register: 1, Data: binaryutil.NativeEndian.PutUint16(1)}, // ARPHRD_ETHER
			&expr.Verdict{Kind: expr.VerdictReturn},
		},
	})
	// Drop traffic from MACs NOT in allowed set
	n.conn.AddRule(&nftables.Rule{
		Table: n.table, Chain: n.chain,
		Exprs: []expr.Any{
			&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseLLHeader, Offset: 6, Len: 6},
			&expr.Lookup{SourceRegister: 1, SetName: "allowed_router_devices", SetID: n.macSet.ID, Invert: true},
			&expr.Verdict{Kind: expr.VerdictDrop},
		},
	})
}

func (n *nftNetlinkInterceptor) addDNSRedirect(cfg InterceptConfig, table *nftables.Table) {
	// DNS redirect requires a NAT chain (redirect is a NAT operation).
	// Using the filter chain would be incorrect per nftables spec.
	dnsChain := n.conn.AddChain(&nftables.Chain{
		Name:     "dnsredir",
		Table:    table,
		Type:     nftables.ChainTypeNAT,
		Hooknum:  nftables.ChainHookPrerouting,
		Priority: nftables.ChainPriorityNATDest,
	})

	v4Subnets, v6Subnets := splitSubnetsByFamily(cfg.LANSubnets)
	dnsPort := uint16(cfg.DNSPort)

	for _, subnet := range v4Subnets {
		_, ipNet, _ := net.ParseCIDR(subnet)
		if ipNet == nil {
			continue
		}
		for _, proto := range []byte{unix.IPPROTO_TCP, unix.IPPROTO_UDP} {
			n.conn.AddRule(&nftables.Rule{
				Table: table, Chain: dnsChain,
				Exprs: []expr.Any{
					&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 12, Len: 4},
					&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: ipNet.Mask, Xor: make([]byte, 4)},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: ipNet.IP.To4()},
					&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{proto}},
					&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: 2, Len: 2},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: binaryutil.BigEndian.PutUint16(53)},
					// Load DNS port into register 1, THEN redirect reads it
					&expr.Immediate{Register: 1, Data: binaryutil.BigEndian.PutUint16(dnsPort)},
					&expr.Redir{RegisterProtoMin: 1},
				},
			})
		}
	}

	for _, subnet := range v6Subnets {
		_, ipNet, _ := net.ParseCIDR(subnet)
		if ipNet == nil {
			continue
		}
		for _, proto := range []byte{unix.IPPROTO_TCP, unix.IPPROTO_UDP} {
			n.conn.AddRule(&nftables.Rule{
				Table: table, Chain: dnsChain,
				Exprs: []expr.Any{
					&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 8, Len: 16},
					&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 16, Mask: ipNet.Mask, Xor: make([]byte, 16)},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: ipNet.IP.To16()},
					&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{proto}},
					&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: 2, Len: 2},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: binaryutil.BigEndian.PutUint16(53)},
					&expr.Immediate{Register: 1, Data: binaryutil.BigEndian.PutUint16(dnsPort)},
					&expr.Redir{RegisterProtoMin: 1},
				},
			})
		}
	}
}

// addDNSRedirectOutput installs the OUTPUT-hook NAT chain that catches
// locally-originated DNS traffic (e.g. dnsmasq → ISP upstream). The mark
// RETURN must come FIRST in the rule list so k2r's own DirectDNS queries,
// which carry the K2RFwmark via gatewayDirectDialer.ControlFunc, are not
// trapped in an infinite recursion loop through the DNS listener.
func (n *nftNetlinkInterceptor) addDNSRedirectOutput(cfg InterceptConfig, table *nftables.Table) {
	outChain := n.conn.AddChain(&nftables.Chain{
		Name:     "dnsredir_out",
		Table:    table,
		Type:     nftables.ChainTypeNAT,
		Hooknum:  nftables.ChainHookOutput,
		Priority: nftables.ChainPriorityNATDest,
	})

	dnsPort := uint16(cfg.DNSPort)

	// Rule 1 (must be first): exempt k2r's own marked traffic.
	n.conn.AddRule(&nftables.Rule{
		Table: table, Chain: outChain,
		Exprs: []expr.Any{
			&expr.Meta{Key: expr.MetaKeyMARK, Register: 1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: binaryutil.NativeEndian.PutUint32(uint32(K2RFwmark))},
			&expr.Verdict{Kind: expr.VerdictReturn},
		},
	})

	// Rules 2-3: REDIRECT port 53 (TCP + UDP) to the local DNS listener.
	// No subnet filter — any locally-originated DNS query qualifies.
	for _, proto := range []byte{unix.IPPROTO_TCP, unix.IPPROTO_UDP} {
		n.conn.AddRule(&nftables.Rule{
			Table: table, Chain: outChain,
			Exprs: []expr.Any{
				&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
				&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{proto}},
				&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: 2, Len: 2},
				&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: binaryutil.BigEndian.PutUint16(53)},
				&expr.Immediate{Register: 1, Data: binaryutil.BigEndian.PutUint16(dnsPort)},
				&expr.Redir{RegisterProtoMin: 1},
			},
		})
	}
}

// dnsExemptTPROXYExprs builds `meta l4proto <proto> th dport 53 return` for
// the TPROXY chain. Port 53 is owned by the dnsredir NAT chain; if TPROXY
// claims it too, both actions apply on a real router and the query is
// tunneled to <router>:DNSPort instead of answered (OpenWrt 25.12, 2026-09-25).
func dnsExemptTPROXYExprs(proto byte) []expr.Any {
	return []expr.Any{
		&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{proto}},
		&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: 2, Len: 2},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: binaryutil.BigEndian.PutUint16(53)},
		&expr.Verdict{Kind: expr.VerdictReturn},
	}
}

// daddrReturnExprs builds `ip daddr <cidr> return` / `ip6 daddr <cidr> return`.
// The nfproto match is not optional in an inet table: a raw network-header
// offset is read from whatever packet arrives, so without it the IPv4 rule
// would test bytes of an IPv6 source address (and the reverse).
func daddrReturnExprs(ipNet *net.IPNet) []expr.Any {
	family, offset, addr := byte(unix.NFPROTO_IPV4), uint32(16), []byte(ipNet.IP.To4())
	if addr == nil {
		family, offset, addr = unix.NFPROTO_IPV6, 24, ipNet.IP.To16()
	}
	return []expr.Any{
		&expr.Meta{Key: expr.MetaKeyNFPROTO, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{family}},
		&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: offset, Len: uint32(len(addr))},
		&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: uint32(len(addr)), Mask: ipNet.Mask, Xor: make([]byte, len(addr))},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: addr},
		&expr.Verdict{Kind: expr.VerdictReturn},
	}
}

// addLinkScopeExempt must run BEFORE addTPROXYRules (and after the MAC
// allowlist, which keeps deciding who may talk at all) — see linkScopeDests.
func (n *nftNetlinkInterceptor) addLinkScopeExempt(lanSubnets []string) {
	v4, v6 := linkScopeDests(lanSubnets)
	for _, cidr := range append(v4, v6...) {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		n.conn.AddRule(&nftables.Rule{
			Table: n.table, Chain: n.chain,
			Exprs: daddrReturnExprs(ipNet),
		})
	}
}

// addDNSExemptFromTPROXY must run BEFORE addTPROXYRules so the return
// precedes the tproxy verdicts in the chain.
func (n *nftNetlinkInterceptor) addDNSExemptFromTPROXY() {
	for _, proto := range []byte{unix.IPPROTO_TCP, unix.IPPROTO_UDP} {
		n.conn.AddRule(&nftables.Rule{
			Table: n.table, Chain: n.chain,
			Exprs: dnsExemptTPROXYExprs(proto),
		})
	}
}

func (n *nftNetlinkInterceptor) addTPROXYRules(cfg InterceptConfig) {
	v4Subnets, v6Subnets := splitSubnetsByFamily(cfg.LANSubnets)
	port := uint16(cfg.ListenPort)

	for _, subnet := range v4Subnets {
		_, ipNet, _ := net.ParseCIDR(subnet)
		if ipNet == nil {
			continue
		}
		for _, proto := range []byte{unix.IPPROTO_TCP, unix.IPPROTO_UDP} {
			n.conn.AddRule(&nftables.Rule{
				Table: n.table, Chain: n.chain,
				Exprs: []expr.Any{
					&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 12, Len: 4},
					&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: ipNet.Mask, Xor: make([]byte, 4)},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: ipNet.IP.To4()},
					&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{proto}},
					// Load TPROXY port into register 1, THEN TProxy reads it
					&expr.Immediate{Register: 1, Data: binaryutil.BigEndian.PutUint16(port)},
					&expr.TProxy{Family: unix.NFPROTO_IPV4, RegPort: 1},
					// Set fwmark for policy routing
					&expr.Immediate{Register: 1, Data: binaryutil.NativeEndian.PutUint32(1)},
					&expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 1},
				},
			})
		}
	}

	for _, subnet := range v6Subnets {
		_, ipNet, _ := net.ParseCIDR(subnet)
		if ipNet == nil {
			continue
		}
		for _, proto := range []byte{unix.IPPROTO_TCP, unix.IPPROTO_UDP} {
			n.conn.AddRule(&nftables.Rule{
				Table: n.table, Chain: n.chain,
				Exprs: []expr.Any{
					&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 8, Len: 16},
					&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 16, Mask: ipNet.Mask, Xor: make([]byte, 16)},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: ipNet.IP.To16()},
					&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{proto}},
					&expr.Immediate{Register: 1, Data: binaryutil.BigEndian.PutUint16(port)},
					&expr.TProxy{Family: unix.NFPROTO_IPV6, RegPort: 1},
					&expr.Immediate{Register: 1, Data: binaryutil.NativeEndian.PutUint32(1)},
					&expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 1},
				},
			})
		}
	}
}

// --- MAC set live operations (implements MACManager) ---

func (n *nftNetlinkInterceptor) AddMAC(macStr string) error {
	n.mu.Lock()
	// concurrency:allow single nft SetAddElements call on n.macSet — no I/O beyond netlink RPC
	defer n.mu.Unlock()

	if n.macSet == nil {
		return fmt.Errorf("MAC set not initialized (not in allowlist mode)")
	}
	mac, err := net.ParseMAC(macStr)
	if err != nil {
		return fmt.Errorf("invalid MAC %q: %w", macStr, err)
	}
	if err := n.conn.SetAddElements(n.macSet, []nftables.SetElement{{Key: mac}}); err != nil {
		return err
	}
	return n.conn.Flush()
}

func (n *nftNetlinkInterceptor) RemoveMAC(macStr string) error {
	n.mu.Lock()
	// concurrency:allow single nft SetDeleteElements call on n.macSet — no I/O beyond netlink RPC
	defer n.mu.Unlock()

	if n.macSet == nil {
		return fmt.Errorf("MAC set not initialized")
	}
	mac, err := net.ParseMAC(macStr)
	if err != nil {
		return fmt.Errorf("invalid MAC %q: %w", macStr, err)
	}
	if err := n.conn.SetDeleteElements(n.macSet, []nftables.SetElement{{Key: mac}}); err != nil {
		return err
	}
	return n.conn.Flush()
}

// --- Helpers ---

func padString(s string, length int) []byte {
	b := make([]byte, length)
	copy(b, s)
	return b
}

// setupIPRules installs the policy route every backend's TPROXY mark relies
// on. It clears first: `ip rule add` does not dedupe, and an Install that
// follows a dead daemon (no Remove) would otherwise stack one more copy of
// the rule on every restart.
func setupIPRules() {
	cleanupIPRules()
	for _, c := range []struct{ args []string }{
		{[]string{"rule", "add", "fwmark", "1", "table", "100"}},
		{[]string{"route", "add", "local", "0.0.0.0/0", "dev", "lo", "table", "100"}},
		{[]string{"-6", "rule", "add", "fwmark", "1", "table", "100"}},
		{[]string{"-6", "route", "add", "local", "::/0", "dev", "lo", "table", "100"}},
	} {
		if err := runCmd("ip", c.args...); err != nil {
			slog.Warn("gateway: ip rule/route setup", "args", c.args, "err", err)
		}
	}
}

// maxStaleIPRules bounds cleanupIPRules' delete-until-gone loop.
const maxStaleIPRules = 32

// cleanupIPRules deletes the policy route, however many copies of each entry
// earlier daemons stacked up (see setupIPRules): each delete removes one copy
// and fails once none is left.
func cleanupIPRules() {
	for _, args := range [][]string{
		{"rule", "del", "fwmark", "1", "table", "100"},
		{"route", "del", "local", "0.0.0.0/0", "dev", "lo", "table", "100"},
		{"-6", "rule", "del", "fwmark", "1", "table", "100"},
		{"-6", "route", "del", "local", "::/0", "dev", "lo", "table", "100"},
	} {
		for i := 0; i < maxStaleIPRules; i++ {
			if runCmd("ip", args...) != nil {
				break
			}
		}
	}
}
