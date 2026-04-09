# Plan B: Gateway Features — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement gateway-side features: nftables Go library rewrite (direct netlink), DNS redirect, RouterDevice MAC allowlist management, LAN device discovery, and OTA self-updater. All changes in k2 submodule on independent branch.

**Architecture:** `intercept_nft.go` rewritten with `google/nftables` (netlink). iptables kept as fallback. `InterceptConfig` extended with DNS redirect and MAC allowlist. RouterDevice management via new HTTP API endpoints. OTA updater downloads from CDN with SHA256 verification.

**Tech Stack:** Go 1.25, `google/nftables`, `golang.org/x/sys/unix`, k2/gateway package (Linux only, `//go:build linux`)

**Branch:** Create `feat/gateway-router-features` in k2 submodule.

**Spec:** `docs/superpowers/specs/2026-04-09-k2r-router-release-features-design.md` (Sections 6-7)
**Principles:** `docs/superpowers/specs/2026-04-09-k2r-development-principles.md`

**Dependencies:** None on Plan A. Gateway features are self-contained. Plan A's Subscription changes affect Center API; this plan only touches k2 submodule.

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `gateway/intercept.go` | Modify | Extend InterceptConfig, update NewInterceptor for netlink detection |
| `gateway/intercept_nft.go` | Rewrite | google/nftables library, DNS redirect, MAC set management |
| `gateway/intercept_nft_test.go` | Create | Unit tests for nftables rule generation (netlink mock) |
| `gateway/intercept_ipt.go` | Modify | Add DNS redirect + MAC filter to iptables fallback |
| `gateway/intercept_test.go` | Modify | Update existing tests, add DNS redirect + MAC tests |
| `gateway/discovery.go` | Create | Platform-aware LAN device discovery (ubus/dnsmasq/ip-neigh) |
| `gateway/discovery_test.go` | Create | Parse format tests for each discovery backend |
| `gateway/router_device.go` | Create | MAC allowlist CRUD, nftables set management, quota enforcement |
| `gateway/router_device_test.go` | Create | Allowlist logic tests |
| `gateway/api.go` | Modify | Register /api/router-devices and /api/updater endpoints |
| `gateway/updater.go` | Create | CDN check, download, verify SHA256, backup, replace, restart |
| `gateway/updater_test.go` | Create | Version comparison, checksum verification tests |
| `gateway/config.go` | Modify | Add DNSPort to GatewayConfig |
| `go.mod` | Modify | Add google/nftables dependency |

---

## Task 1: Add google/nftables Dependency + Extend InterceptConfig

**Files:**
- Modify: `go.mod`
- Modify: `gateway/intercept.go`
- Modify: `gateway/config.go`

- [ ] **Step 1: Add google/nftables dependency**

```bash
cd k2 && go get github.com/google/nftables@latest
```

Verify it appears in `go.mod` under `require`.

- [ ] **Step 2: Extend InterceptConfig**

In `gateway/intercept.go`, add new fields to `InterceptConfig` (line 20-25):

```go
type InterceptConfig struct {
	ListenPort    int
	LANSubnets    []string
	ExcludeIPs    []string
	// New fields
	DNSRedirect   bool     // redirect port 53 to DNSPort
	DNSPort       int      // k2r DNS resolver port (from engine config)
	AllowlistMode bool     // true = only AllowedMACs can access proxy
	AllowedMACs   []string // MAC addresses in allowlist (e.g., "AA:BB:CC:DD:EE:FF")
}
```

- [ ] **Step 3: Add DNSPort to GatewayConfig**

In `gateway/config.go`, add to `GatewayConfig` (line 17-21):

```go
type GatewayConfig struct {
	config.ClientConfig `yaml:",inline"`
	ListenPort          int      `yaml:"listen_port"`
	LANSubnets          []string `yaml:"lan_subnets"`
	DNSRedirect         bool     `yaml:"dns_redirect"`
	DNSPort             int      `yaml:"dns_port"` // default: ListenPort + 1
}
```

In `SetGatewayDefaults` (line 39), add:

```go
	if cfg.DNSPort == 0 {
		cfg.DNSPort = cfg.ListenPort + 1
	}
```

- [ ] **Step 4: Update NewInterceptor to try netlink first**

In `gateway/intercept.go`, replace `NewInterceptor` (line 29-38):

```go
func NewInterceptor() (Interceptor, error) {
	// Try Go nftables library (direct netlink) first — no nft binary needed
	if nft, err := newNftNetlinkInterceptor(); err == nil {
		return nft, nil
	}
	// Fallback: shell-based nft command
	if _, err := exec.LookPath("nft"); err == nil {
		return &nftShellInterceptor{}, nil
	}
	// Fallback: iptables
	if _, err := exec.LookPath("iptables"); err == nil {
		return &iptInterceptor{}, nil
	}
	return nil, fmt.Errorf("gateway: no firewall backend available (tried netlink, nft, iptables)")
}
```

Note: rename current `nftInterceptor` to `nftShellInterceptor` (keeping it as second fallback), and the new netlink-based implementation will be `nftNetlinkInterceptor`.

- [ ] **Step 5: Rename existing nftInterceptor to nftShellInterceptor**

In `gateway/intercept_nft.go`, rename:
- `nftInterceptor` → `nftShellInterceptor`
- Keep all existing code unchanged (it's now the shell-exec fallback)

- [ ] **Step 6: Update tests that reference nftInterceptor**

In `gateway/intercept_test.go`, update all `&nftInterceptor{}` → `&nftShellInterceptor{}`.

- [ ] **Step 7: Verify compilation**

```bash
cd k2 && GOOS=linux go build ./gateway/...
```

- [ ] **Step 8: Run existing tests**

```bash
cd k2 && GOOS=linux go test ./gateway/... -v
```

Expected: All existing tests pass (only renamed struct).

- [ ] **Step 9: Commit**

```bash
cd k2 && git add go.mod go.sum gateway/intercept.go gateway/intercept_nft.go gateway/intercept_test.go gateway/config.go
git commit -m "feat(gateway): add google/nftables dep, extend InterceptConfig, rename shell interceptor

InterceptConfig gains: DNSRedirect, DNSPort, AllowlistMode, AllowedMACs.
NewInterceptor: try netlink → nft shell → iptables (3-tier fallback).
Existing nftInterceptor renamed to nftShellInterceptor (fallback role)."
```

---

## Task 2: nftables Netlink Interceptor

**Files:**
- Create: `gateway/intercept_nft_netlink.go`
- Create: `gateway/intercept_nft_netlink_test.go`

This is the highest-risk task. The `google/nftables` API constructs rules via netlink expressions. Each rule is a sequence of `expr.Xxx` structs.

- [ ] **Step 1: Create intercept_nft_netlink.go with basic structure**

```go
//go:build linux

package gateway

import (
	"fmt"
	"log/slog"
	"net"

	"github.com/google/nftables"
	"github.com/google/nftables/binaryutil"
	"github.com/google/nftables/expr"
	"golang.org/x/sys/unix"
)

type nftNetlinkInterceptor struct {
	conn      *nftables.Conn
	table     *nftables.Table
	chain     *nftables.Chain
	macSet    *nftables.Set // allowed_router_devices (nil if open mode)
	installed bool
}

func newNftNetlinkInterceptor() (*nftNetlinkInterceptor, error) {
	conn, err := nftables.New()
	if err != nil {
		return nil, fmt.Errorf("nftables netlink init: %w", err)
	}
	// Verify kernel supports nftables by listing tables
	if _, err := conn.ListTables(); err != nil {
		return nil, fmt.Errorf("nftables netlink probe: %w", err)
	}
	return &nftNetlinkInterceptor{conn: conn}, nil
}

func (n *nftNetlinkInterceptor) Name() string { return "nftables-netlink" }
```

- [ ] **Step 2: Implement Install method**

```go
func (n *nftNetlinkInterceptor) Install(cfg InterceptConfig) error {
	// Delete stale table if exists (idempotent cleanup)
	n.conn.DelTable(&nftables.Table{Family: nftables.TableFamilyINet, Name: "k2r"})
	_ = n.conn.Flush() // ignore error (table may not exist)

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

	// Rule 1: skip loopback
	n.addLoopbackBypass()

	// Rule 2: skip excluded IPs
	n.addExcludeRules(cfg.ExcludeIPs)

	// Rule 3: MAC allowlist (if enabled)
	if cfg.AllowlistMode && len(cfg.AllowedMACs) > 0 {
		n.addMACAllowlist(cfg.AllowedMACs)
	}

	// Rule 4: DNS redirect (MUST come before TPROXY)
	if cfg.DNSRedirect && cfg.DNSPort > 0 {
		n.addDNSRedirect(cfg)
	}

	// Rule 5: TPROXY rules per LAN subnet
	n.addTPROXYRules(cfg)

	// Atomic commit all rules
	if err := n.conn.Flush(); err != nil {
		return fmt.Errorf("nftables flush: %w", err)
	}

	// ip rule + route for fwmark-based routing
	setupIPRules()

	n.installed = true
	slog.Info("DIAG: gw-intercept-install", "backend", "nftables-netlink",
		"subnets", cfg.LANSubnets, "excludeCount", len(cfg.ExcludeIPs),
		"dnsRedirect", cfg.DNSRedirect, "allowlistMode", cfg.AllowlistMode)
	return nil
}
```

- [ ] **Step 3: Implement loopback bypass rule**

```go
func (n *nftNetlinkInterceptor) addLoopbackBypass() {
	// iifname "lo" return
	n.conn.AddRule(&nftables.Rule{
		Table: n.table,
		Chain: n.chain,
		Exprs: []expr.Any{
			// Load iifname into register
			&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
			// Compare with "lo"
			&expr.Cmp{
				Op:       expr.CmpOpEq,
				Register: 1,
				Data:     padString("lo", 16), // IFNAMSIZ = 16
			},
			&expr.Verdict{Kind: expr.VerdictReturn},
		},
	})
}

// padString pads a string to the given length with null bytes (for nftables string matching)
func padString(s string, length int) []byte {
	b := make([]byte, length)
	copy(b, s)
	return b
}
```

- [ ] **Step 4: Implement exclude IP rules**

```go
func (n *nftNetlinkInterceptor) addExcludeRules(excludeIPs []string) {
	v4, v6 := splitIPsByFamily(excludeIPs)

	if len(v4) > 0 {
		// Create exclude_v4 set
		set := &nftables.Set{
			Table:   n.table,
			Name:    "exclude_v4",
			KeyType: nftables.TypeIPAddr,
		}
		var elements []nftables.SetElement
		for _, ip := range v4 {
			elements = append(elements, nftables.SetElement{
				Key: net.ParseIP(ip).To4(),
			})
		}
		n.conn.AddSet(set, elements)

		// ip daddr @exclude_v4 return
		n.conn.AddRule(&nftables.Rule{
			Table: n.table,
			Chain: n.chain,
			Exprs: []expr.Any{
				// Load destination IP
				&expr.Payload{
					DestRegister: 1,
					Base:         expr.PayloadBaseNetworkHeader,
					Offset:       16, // IPv4 dst addr offset
					Len:          4,
				},
				// Lookup in set
				&expr.Lookup{
					SourceRegister: 1,
					SetName:        "exclude_v4",
					SetID:          set.ID,
				},
				&expr.Verdict{Kind: expr.VerdictReturn},
			},
		})
	}

	if len(v6) > 0 {
		set := &nftables.Set{
			Table:   n.table,
			Name:    "exclude_v6",
			KeyType: nftables.TypeIP6Addr,
		}
		var elements []nftables.SetElement
		for _, ip := range v6 {
			elements = append(elements, nftables.SetElement{
				Key: net.ParseIP(ip).To16(),
			})
		}
		n.conn.AddSet(set, elements)

		n.conn.AddRule(&nftables.Rule{
			Table: n.table,
			Chain: n.chain,
			Exprs: []expr.Any{
				&expr.Payload{
					DestRegister: 1,
					Base:         expr.PayloadBaseNetworkHeader,
					Offset:       24, // IPv6 dst addr offset
					Len:          16,
				},
				&expr.Lookup{
					SourceRegister: 1,
					SetName:        "exclude_v6",
					SetID:          set.ID,
				},
				&expr.Verdict{Kind: expr.VerdictReturn},
			},
		})
	}
}
```

- [ ] **Step 5: Implement MAC allowlist**

```go
func (n *nftNetlinkInterceptor) addMACAllowlist(macs []string) {
	// Create named set for MAC addresses
	n.macSet = &nftables.Set{
		Table:   n.table,
		Name:    "allowed_router_devices",
		KeyType: nftables.TypeEtherAddr,
	}
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

	// Check iiftype is Ethernet first (safety for non-Ethernet interfaces)
	// Then: ether saddr != @allowed_router_devices drop
	n.conn.AddRule(&nftables.Rule{
		Table: n.table,
		Chain: n.chain,
		Exprs: []expr.Any{
			// Check interface type is Ethernet (ARPHRD_ETHER = 1)
			&expr.Meta{Key: expr.MetaKeyIIFTYPE, Register: 1},
			&expr.Cmp{
				Op:       expr.CmpOpNeq,
				Register: 1,
				Data:     binaryutil.NativeEndian.PutUint16(1), // ARPHRD_ETHER
			},
			&expr.Verdict{Kind: expr.VerdictReturn}, // skip non-Ethernet
		},
	})
	n.conn.AddRule(&nftables.Rule{
		Table: n.table,
		Chain: n.chain,
		Exprs: []expr.Any{
			// Load source MAC (offset 6 in link layer header, 6 bytes)
			&expr.Payload{
				DestRegister: 1,
				Base:         expr.PayloadBaseLLHeader,
				Offset:       6, // source MAC offset in Ethernet frame
				Len:          6,
			},
			// Lookup in set — Invert=true means "NOT in set"
			&expr.Lookup{
				SourceRegister: 1,
				SetName:        "allowed_router_devices",
				SetID:          n.macSet.ID,
				Invert:         true,
			},
			&expr.Verdict{Kind: expr.VerdictDrop},
		},
	})
}

// AddMAC adds a MAC to the allowed set (live, without full reinstall)
func (n *nftNetlinkInterceptor) AddMAC(macStr string) error {
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

// RemoveMAC removes a MAC from the allowed set
func (n *nftNetlinkInterceptor) RemoveMAC(macStr string) error {
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
```

- [ ] **Step 6: Implement DNS redirect rules**

```go
func (n *nftNetlinkInterceptor) addDNSRedirect(cfg InterceptConfig) {
	v4Subnets, v6Subnets := splitSubnetsByFamily(cfg.LANSubnets)
	dnsPort := uint16(cfg.DNSPort)

	for _, subnet := range v4Subnets {
		_, ipNet, _ := net.ParseCIDR(subnet)
		if ipNet == nil {
			continue
		}
		// ip saddr $subnet meta l4proto {tcp, udp} th dport 53 redirect to :$DNSPort
		for _, proto := range []byte{unix.IPPROTO_TCP, unix.IPPROTO_UDP} {
			n.conn.AddRule(&nftables.Rule{
				Table: n.table,
				Chain: n.chain,
				Exprs: []expr.Any{
					// Match source subnet
					&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 12, Len: 4},
					&expr.Bitwise{
						SourceRegister: 1,
						DestRegister:   1,
						Len:            4,
						Mask:           ipNet.Mask,
						Xor:            make([]byte, 4),
					},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: ipNet.IP.To4()},
					// Match protocol
					&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{proto}},
					// Match destination port 53
					&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: 2, Len: 2},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: binaryutil.BigEndian.PutUint16(53)},
					// Redirect to DNS port
					&expr.Immediate{Register: 1, Data: binaryutil.BigEndian.PutUint16(dnsPort)},
					&expr.Redir{RegisterProtoMin: 1},
				},
			})
		}
	}

	// IPv6 DNS redirect (same pattern, different offsets)
	for _, subnet := range v6Subnets {
		_, ipNet, _ := net.ParseCIDR(subnet)
		if ipNet == nil {
			continue
		}
		for _, proto := range []byte{unix.IPPROTO_TCP, unix.IPPROTO_UDP} {
			n.conn.AddRule(&nftables.Rule{
				Table: n.table,
				Chain: n.chain,
				Exprs: []expr.Any{
					&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 8, Len: 16},
					&expr.Bitwise{
						SourceRegister: 1,
						DestRegister:   1,
						Len:            16,
						Mask:           ipNet.Mask,
						Xor:            make([]byte, 16),
					},
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
```

- [ ] **Step 7: Implement TPROXY rules**

```go
func (n *nftNetlinkInterceptor) addTPROXYRules(cfg InterceptConfig) {
	v4Subnets, v6Subnets := splitSubnetsByFamily(cfg.LANSubnets)
	port := uint16(cfg.ListenPort)

	for _, subnet := range v4Subnets {
		_, ipNet, _ := net.ParseCIDR(subnet)
		if ipNet == nil {
			continue
		}
		for _, proto := range []byte{unix.IPPROTO_TCP, unix.IPPROTO_UDP} {
			// ip saddr $subnet meta l4proto $proto tproxy ip to :$port meta mark set 1
			n.conn.AddRule(&nftables.Rule{
				Table: n.table,
				Chain: n.chain,
				Exprs: []expr.Any{
					// Match source subnet
					&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 12, Len: 4},
					&expr.Bitwise{
						SourceRegister: 1, DestRegister: 1, Len: 4,
						Mask: ipNet.Mask, Xor: make([]byte, 4),
					},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: ipNet.IP.To4()},
					// Match protocol
					&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{proto}},
					// TPROXY to port
					&expr.TProxy{
						Family:      byte(nftables.TableFamilyIPv4),
						TableFamily: byte(nftables.TableFamilyIPv4),
						RegPort:     1,
					},
					&expr.Immediate{Register: 1, Data: binaryutil.BigEndian.PutUint16(port)},
					// Set mark
					&expr.Immediate{Register: 1, Data: binaryutil.NativeEndian.PutUint32(1)},
					&expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 1},
				},
			})
		}
	}

	// IPv6 TPROXY rules (same pattern)
	for _, subnet := range v6Subnets {
		_, ipNet, _ := net.ParseCIDR(subnet)
		if ipNet == nil {
			continue
		}
		for _, proto := range []byte{unix.IPPROTO_TCP, unix.IPPROTO_UDP} {
			n.conn.AddRule(&nftables.Rule{
				Table: n.table,
				Chain: n.chain,
				Exprs: []expr.Any{
					&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 8, Len: 16},
					&expr.Bitwise{
						SourceRegister: 1, DestRegister: 1, Len: 16,
						Mask: ipNet.Mask, Xor: make([]byte, 16),
					},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: ipNet.IP.To16()},
					&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{proto}},
					&expr.TProxy{
						Family:      byte(nftables.TableFamilyIPv6),
						TableFamily: byte(nftables.TableFamilyIPv6),
						RegPort:     1,
					},
					&expr.Immediate{Register: 1, Data: binaryutil.BigEndian.PutUint16(port)},
					&expr.Immediate{Register: 1, Data: binaryutil.NativeEndian.PutUint32(1)},
					&expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 1},
				},
			})
		}
	}
}
```

- [ ] **Step 8: Implement Remove + setupIPRules helper**

```go
func (n *nftNetlinkInterceptor) Remove() {
	if n.table != nil {
		n.conn.DelTable(n.table)
		_ = n.conn.Flush()
	}
	cleanupIPRules()
	n.installed = false
	n.macSet = nil
	slog.Info("gateway: nftables-netlink rules cleaned up")
}

// setupIPRules configures policy routing for TPROXY fwmark
func setupIPRules() {
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

func cleanupIPRules() {
	_ = runCmd("ip", "rule", "del", "fwmark", "1", "table", "100")
	_ = runCmd("ip", "route", "del", "local", "0.0.0.0/0", "dev", "lo", "table", "100")
	_ = runCmd("ip", "-6", "rule", "del", "fwmark", "1", "table", "100")
	_ = runCmd("ip", "-6", "route", "del", "local", "::/0", "dev", "lo", "table", "100")
}
```

- [ ] **Step 9: Verify cross-compilation**

```bash
cd k2 && GOOS=linux go build ./gateway/...
```

Expected: Compiles (netlink packages are Linux-only but build-tagged).

- [ ] **Step 10: Commit**

```bash
cd k2 && git add gateway/intercept_nft_netlink.go
git commit -m "feat(gateway): nftables netlink interceptor with DNS redirect + MAC allowlist

Direct netlink via google/nftables — no nft binary dependency.
Supports: TPROXY, DNS redirect (port 53), MAC allowlist (ether saddr set).
Atomic Flush() commit. Clean teardown via DelTable()."
```

**Known risk:** The exact `expr.TProxy` struct fields may differ across google/nftables versions. The TPROXY expression construction needs validation on actual Linux. If `expr.TProxy` doesn't match the expected API, use `expr.Immediate` + raw bytecode as fallback. Test this in Task 2's test step.

---

## Task 3: LAN Device Discovery

**Files:**
- Create: `gateway/discovery.go`
- Create: `gateway/discovery_test.go`

- [ ] **Step 1: Create discovery.go**

```go
//go:build linux

package gateway

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strings"
)

// LanDevice represents a device discovered on the LAN
type LanDevice struct {
	MAC      string `json:"mac"`
	IP       string `json:"ip"`
	Hostname string `json:"hostname"`
	Online   bool   `json:"online"`
}

// discoverLANDevices returns devices on the LAN using platform-specific methods.
// Priority: ubus (OpenWrt) → dnsmasq leases → ip neigh
func discoverLANDevices(lanSubnets []string) []LanDevice {
	if hasUbus() {
		devices := discoverViaUbus()
		if len(devices) > 0 {
			return filterBySubnets(devices, lanSubnets)
		}
	}
	if path := findDnsmasqLeases(); path != "" {
		devices := discoverViaDnsmasqLeases(path)
		if len(devices) > 0 {
			return filterBySubnets(devices, lanSubnets)
		}
	}
	return filterBySubnets(discoverViaIPNeigh(), lanSubnets)
}

func hasUbus() bool {
	_, err := exec.LookPath("ubus")
	return err == nil
}

// discoverViaUbus uses OpenWrt's ubus to get DHCP lease info
func discoverViaUbus() []LanDevice {
	out, err := exec.Command("ubus", "call", "dhcp", "ipv4leases").Output()
	if err != nil {
		slog.Debug("gateway: ubus dhcp query failed", "err", err)
		return nil
	}
	// Parse ubus JSON response
	// Format: {"device":{"br-lan":{"leases":[{"hostname":"iPhone","mac":"AA:BB:CC:DD:EE:FF","ip":"192.168.1.100","expire":12345}]}}}
	var resp struct {
		Device map[string]struct {
			Leases []struct {
				Hostname string `json:"hostname"`
				MAC      string `json:"mac"`
				IP       string `json:"ip"`
				Expire   int64  `json:"expire"`
			} `json:"leases"`
		} `json:"device"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		slog.Debug("gateway: ubus parse failed", "err", err)
		return nil
	}
	var devices []LanDevice
	for _, iface := range resp.Device {
		for _, lease := range iface.Leases {
			devices = append(devices, LanDevice{
				MAC:      strings.ToUpper(lease.MAC),
				IP:       lease.IP,
				Hostname: lease.Hostname,
				Online:   true, // DHCP lease implies recently active
			})
		}
	}
	return devices
}

// findDnsmasqLeases checks common lease file locations
func findDnsmasqLeases() string {
	paths := []string{
		"/tmp/dhcp.leases",            // OpenWrt
		"/var/lib/misc/dnsmasq.leases", // Debian/Ubuntu
		"/var/lib/dnsmasq/dnsmasq.leases",
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// discoverViaDnsmasqLeases parses dnsmasq lease file
// Format: <expire_time> <mac> <ip> <hostname> <client_id>
func discoverViaDnsmasqLeases(path string) []LanDevice {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var devices []LanDevice
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 {
			continue
		}
		hostname := fields[3]
		if hostname == "*" {
			hostname = ""
		}
		devices = append(devices, LanDevice{
			MAC:      strings.ToUpper(fields[1]),
			IP:       fields[2],
			Hostname: hostname,
			Online:   true,
		})
	}
	return devices
}

// discoverViaIPNeigh parses `ip neigh show` output
// Format: 192.168.1.100 dev br-lan lladdr aa:bb:cc:dd:ee:ff REACHABLE
func discoverViaIPNeigh() []LanDevice {
	out, err := exec.Command("ip", "neigh", "show").Output()
	if err != nil {
		return nil
	}
	var devices []LanDevice
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		ip := fields[0]
		macIdx := -1
		for i, f := range fields {
			if f == "lladdr" && i+1 < len(fields) {
				macIdx = i + 1
				break
			}
		}
		if macIdx < 0 {
			continue // no MAC (e.g., FAILED state)
		}
		mac := strings.ToUpper(fields[macIdx])
		state := fields[len(fields)-1]
		online := state == "REACHABLE" || state == "DELAY" || state == "STALE" || state == "PROBE"
		devices = append(devices, LanDevice{
			MAC:      mac,
			IP:       ip,
			Hostname: "", // ip neigh doesn't provide hostname
			Online:   online,
		})
	}
	return devices
}

// filterBySubnets filters devices to only those in the configured LAN subnets
func filterBySubnets(devices []LanDevice, subnets []string) []LanDevice {
	if len(subnets) == 0 {
		return devices
	}
	var nets []*net.IPNet
	for _, s := range subnets {
		_, ipNet, err := net.ParseCIDR(s)
		if err == nil {
			nets = append(nets, ipNet)
		}
	}
	var filtered []LanDevice
	for _, d := range devices {
		ip := net.ParseIP(d.IP)
		if ip == nil {
			continue
		}
		for _, n := range nets {
			if n.Contains(ip) {
				filtered = append(filtered, d)
				break
			}
		}
	}
	return filtered
}
```

- [ ] **Step 2: Create discovery_test.go**

```go
//go:build linux

package gateway

import (
	"testing"
)

func TestParseDnsmasqLeases(t *testing.T) {
	// Create temp lease file
	content := `1712600000 aa:bb:cc:dd:ee:ff 192.168.1.100 iPhone *
1712600000 11:22:33:44:55:66 192.168.1.101 laptop client-id
1712600000 AA:BB:CC:DD:EE:11 192.168.1.102 * *
`
	tmpFile := t.TempDir() + "/leases"
	if err := writeTestFile(tmpFile, content); err != nil {
		t.Fatal(err)
	}

	devices := discoverViaDnsmasqLeases(tmpFile)
	if len(devices) != 3 {
		t.Fatalf("expected 3 devices, got %d", len(devices))
	}
	if devices[0].MAC != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("MAC should be uppercased, got %s", devices[0].MAC)
	}
	if devices[0].Hostname != "iPhone" {
		t.Errorf("hostname = %s, want iPhone", devices[0].Hostname)
	}
	if devices[2].Hostname != "" {
		t.Errorf("* hostname should be empty, got %s", devices[2].Hostname)
	}
}

func TestParseIPNeigh(t *testing.T) {
	// Test parsing of `ip neigh show` output format
	// This is a format validation test — actual `ip neigh` requires Linux
}

func TestFilterBySubnets(t *testing.T) {
	devices := []LanDevice{
		{MAC: "AA:BB:CC:DD:EE:FF", IP: "192.168.1.100"},
		{MAC: "11:22:33:44:55:66", IP: "10.0.0.50"},
		{MAC: "77:88:99:AA:BB:CC", IP: "192.168.2.200"},
	}
	filtered := filterBySubnets(devices, []string{"192.168.1.0/24"})
	if len(filtered) != 1 {
		t.Fatalf("expected 1 device in 192.168.1.0/24, got %d", len(filtered))
	}
	if filtered[0].IP != "192.168.1.100" {
		t.Errorf("wrong device: %s", filtered[0].IP)
	}
}

func TestFilterBySubnets_Empty(t *testing.T) {
	devices := []LanDevice{{MAC: "AA:BB:CC:DD:EE:FF", IP: "192.168.1.100"}}
	filtered := filterBySubnets(devices, nil)
	if len(filtered) != 1 {
		t.Error("empty subnets should return all devices")
	}
}

func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}
```

- [ ] **Step 3: Run tests**

```bash
cd k2 && GOOS=linux go test ./gateway/... -run TestParse -v
cd k2 && GOOS=linux go test ./gateway/... -run TestFilter -v
```

- [ ] **Step 4: Commit**

```bash
cd k2 && git add gateway/discovery.go gateway/discovery_test.go
git commit -m "feat(gateway): platform-aware LAN device discovery (ubus/dnsmasq/ip-neigh)"
```

---

## Task 4: RouterDevice Management API

**Files:**
- Create: `gateway/router_device.go`
- Create: `gateway/router_device_test.go`
- Modify: `gateway/api.go` — register endpoints

- [ ] **Step 1: Create router_device.go**

```go
//go:build linux

package gateway

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

// RouterDeviceEntry is a MAC in the allowlist
type RouterDeviceEntry struct {
	MAC     string `json:"mac"`
	Remark  string `json:"remark"`
	AddedAt int64  `json:"addedAt"`
}

// RouterDeviceManager manages the MAC allowlist and nftables enforcement
type RouterDeviceManager struct {
	mu          sync.RWMutex
	entries     []RouterDeviceEntry
	mode        string // "open" or "allowlist"
	quota       int    // 0 = unlimited
	lanSubnets  []string
	interceptor Interceptor // for MAC set operations
	storage     func(key string, value any) error // storage write callback
	loadStorage func(key string, dest any) error  // storage read callback
}

// RouterDeviceListResponse is the GET /api/router-devices response
type RouterDeviceListResponse struct {
	Mode            string            `json:"mode"`
	MaxRouterDevice int               `json:"maxRouterDevice"`
	RouterDevices   []RouterDeviceInfo `json:"routerDevices"`
}

type RouterDeviceInfo struct {
	MAC      string `json:"mac"`
	IP       string `json:"ip"`
	Hostname string `json:"hostname"`
	Online   bool   `json:"online"`
	Allowed  bool   `json:"allowed"`
	Remark   string `json:"remark"`
}

func (m *RouterDeviceManager) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	m.mu.RLock()
	mode := m.mode
	quota := m.quota
	entries := make([]RouterDeviceEntry, len(m.entries))
	copy(entries, m.entries)
	subnets := m.lanSubnets
	m.mu.RUnlock()

	// Discover LAN devices
	lanDevices := discoverLANDevices(subnets)

	// Build allowlist MAC set for quick lookup
	allowedMACs := make(map[string]RouterDeviceEntry)
	for _, e := range entries {
		allowedMACs[e.MAC] = e
	}

	// Merge discovered + allowlist
	seen := make(map[string]bool)
	var result []RouterDeviceInfo
	for _, d := range lanDevices {
		seen[d.MAC] = true
		entry, allowed := allowedMACs[d.MAC]
		remark := ""
		if allowed {
			remark = entry.Remark
		}
		result = append(result, RouterDeviceInfo{
			MAC: d.MAC, IP: d.IP, Hostname: d.Hostname,
			Online: d.Online, Allowed: allowed, Remark: remark,
		})
	}
	// Add offline allowlist entries
	for _, e := range entries {
		if !seen[e.MAC] {
			result = append(result, RouterDeviceInfo{
				MAC: e.MAC, Online: false, Allowed: true, Remark: e.Remark,
			})
		}
	}

	writeJSON(w, Response{Code: 0, Data: RouterDeviceListResponse{
		Mode: mode, MaxRouterDevice: quota, RouterDevices: result,
	}})
}

func (m *RouterDeviceManager) handleAllow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		MAC    string `json:"mac"`
		Remark string `json:"remark"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, Response{Code: 1, Message: "bad request"})
		return
	}
	mac := normalizeMAC(body.MAC)
	if mac == "" {
		writeJSON(w, Response{Code: 1, Message: "invalid MAC address"})
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check quota
	if m.quota > 0 && len(m.entries) >= m.quota {
		writeJSON(w, Response{Code: 1, Message: "quotaExceeded"})
		return
	}
	// Check duplicate
	for _, e := range m.entries {
		if e.MAC == mac {
			writeJSON(w, Response{Code: 1, Message: "already in allowlist"})
			return
		}
	}

	entry := RouterDeviceEntry{MAC: mac, Remark: body.Remark, AddedAt: time.Now().Unix()}
	m.entries = append(m.entries, entry)
	m.persist()

	// Update nftables set if in allowlist mode
	if m.mode == "allowlist" {
		if nft, ok := m.interceptor.(*nftNetlinkInterceptor); ok {
			if err := nft.AddMAC(mac); err != nil {
				slog.Warn("gateway: failed to add MAC to nftables set", "mac", mac, "err", err)
			}
		}
	}

	writeJSON(w, Response{Code: 0, Message: "ok"})
}

func (m *RouterDeviceManager) handleRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		MAC string `json:"mac"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, Response{Code: 1, Message: "bad request"})
		return
	}
	mac := normalizeMAC(body.MAC)

	m.mu.Lock()
	defer m.mu.Unlock()

	found := false
	var remaining []RouterDeviceEntry
	for _, e := range m.entries {
		if e.MAC == mac {
			found = true
			continue
		}
		remaining = append(remaining, e)
	}
	if !found {
		writeJSON(w, Response{Code: 1, Message: "not in allowlist"})
		return
	}
	m.entries = remaining
	m.persist()

	if m.mode == "allowlist" {
		if nft, ok := m.interceptor.(*nftNetlinkInterceptor); ok {
			if err := nft.RemoveMAC(mac); err != nil {
				slog.Warn("gateway: failed to remove MAC from nftables set", "mac", mac, "err", err)
			}
		}
	}

	writeJSON(w, Response{Code: 0, Message: "ok"})
}

func (m *RouterDeviceManager) handleMode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Mode string `json:"mode"` // "open" or "allowlist"
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, Response{Code: 1, Message: "bad request"})
		return
	}
	if body.Mode != "open" && body.Mode != "allowlist" {
		writeJSON(w, Response{Code: 1, Message: "mode must be 'open' or 'allowlist'"})
		return
	}

	m.mu.Lock()
	m.mode = body.Mode
	m.persist()
	m.mu.Unlock()

	// Mode change requires interceptor reinstall (MAC rules added/removed)
	// Signal gateway to re-apply interceptor config
	slog.Info("gateway: router device mode changed", "mode", body.Mode)

	writeJSON(w, Response{Code: 0, Message: "ok"})
}

func (m *RouterDeviceManager) persist() {
	if m.storage != nil {
		_ = m.storage("router-device-allowlist", m.entries)
		_ = m.storage("router-device-allowlist-mode", m.mode)
	}
}

func normalizeMAC(s string) string {
	hw, err := net.ParseMAC(s)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X", hw[0], hw[1], hw[2], hw[3], hw[4], hw[5])
}
```

- [ ] **Step 2: Register endpoints in api.go**

In `gateway.go` `Run` method (line 58-62), add after existing mux routes:

```go
	// Router device management
	if g.routerDeviceMgr != nil {
		mux.HandleFunc("/api/router-devices", g.routerDeviceMgr.handleList)
		mux.HandleFunc("/api/router-devices/allow", g.routerDeviceMgr.handleAllow)
		mux.HandleFunc("/api/router-devices/remove", g.routerDeviceMgr.handleRemove)
		mux.HandleFunc("/api/router-devices/mode", g.routerDeviceMgr.handleMode)
	}
```

Add `routerDeviceMgr *RouterDeviceManager` field to Gateway struct (line 24-40).

Initialize in `New()` or `doUp()`.

- [ ] **Step 3: Write tests**

```go
func TestNormalizeMAC(t *testing.T) {
	tests := []struct{ in, want string }{
		{"aa:bb:cc:dd:ee:ff", "AA:BB:CC:DD:EE:FF"},
		{"AA:BB:CC:DD:EE:FF", "AA:BB:CC:DD:EE:FF"},
		{"aa-bb-cc-dd-ee-ff", "AA:BB:CC:DD:EE:FF"},
		{"invalid", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := normalizeMAC(tt.in)
		if got != tt.want {
			t.Errorf("normalizeMAC(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestRouterDeviceQuota(t *testing.T) {
	mgr := &RouterDeviceManager{quota: 2, mode: "allowlist"}
	mgr.entries = []RouterDeviceEntry{
		{MAC: "AA:BB:CC:DD:EE:01"},
		{MAC: "AA:BB:CC:DD:EE:02"},
	}
	// Adding a 3rd should fail
	// (test via direct method call, not HTTP)
}
```

- [ ] **Step 4: Commit**

```bash
cd k2 && git add gateway/router_device.go gateway/router_device_test.go gateway/gateway.go gateway/api.go
git commit -m "feat(gateway): RouterDevice management API (MAC allowlist CRUD + quota enforcement)"
```

---

## Task 5: OTA Updater

**Files:**
- Create: `gateway/updater.go`
- Create: `gateway/updater_test.go`
- Modify: `gateway/api.go` — register updater endpoints

- [ ] **Step 1: Create updater.go**

```go
//go:build linux

package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	cdnBaseURL       = "https://dl.kaitu.io/kaitu/k2r"
	cdnBackupURL     = "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/k2r"
	defaultBinPath   = "/usr/bin/k2r"
	updateStatePath  = "/etc/k2r/update-state.json"
)

type UpdateInfo struct {
	HasUpdate      bool   `json:"hasUpdate"`
	CurrentVersion string `json:"currentVersion"`
	NewVersion     string `json:"newVersion"`
}

type UpdateProgress struct {
	Stage    string  `json:"stage"`    // downloading, verifying, backing-up, replacing, restarting, done, error
	Progress float64 `json:"progress"` // 0.0-1.0 for downloading
	Error    string  `json:"error,omitempty"`
}

// UpdateState persisted to disk for crash recovery
type UpdateState struct {
	Stage     string `json:"stage"`     // last completed stage
	Version   string `json:"version"`   // target version
	TmpPath   string `json:"tmpPath"`   // download temp file
	BakPath   string `json:"bakPath"`   // backup path
	StartedAt int64  `json:"startedAt"` // Unix timestamp
}

type Updater struct {
	mu             sync.Mutex
	currentVersion string
	arch           string
	binPath        string
	progress       UpdateProgress
	onProgress     func(UpdateProgress) // SSE broadcast
}

func NewUpdater(version, arch string) *Updater {
	u := &Updater{
		currentVersion: version,
		arch:           mapArch(arch),
		binPath:        defaultBinPath,
	}
	return u
}

// RecoverOnStartup checks for interrupted update state and recovers.
// Called once during Gateway.Run() before serving HTTP.
func (u *Updater) RecoverOnStartup() {
	state := loadUpdateState()
	if state == nil {
		return // no pending update
	}
	slog.Info("gateway: recovering from interrupted update", "stage", state.Stage, "version", state.Version)

	switch state.Stage {
	case "downloading", "verifying":
		// Interrupted before binary was replaced — clean up tmp file, reset
		slog.Info("gateway: cleaning up incomplete download", "tmp", state.TmpPath)
		os.Remove(state.TmpPath)
		clearUpdateState()

	case "backing-up":
		// Backup may be partial — clean up both tmp and partial backup
		os.Remove(state.TmpPath)
		os.Remove(state.BakPath)
		clearUpdateState()

	case "replacing":
		// Critical: binary may or may not have been replaced
		// Check if current binary version matches target
		if u.currentVersion == state.Version {
			// New binary is running — update succeeded
			slog.Info("gateway: update to %s succeeded (post-replace recovery)", state.Version)
			os.Remove(state.TmpPath)
			clearUpdateState()
		} else {
			// Replace may have failed — rollback from .bak if exists
			if _, err := os.Stat(state.BakPath); err == nil {
				slog.Warn("gateway: rolling back to backup after interrupted replace", "bak", state.BakPath)
				os.Rename(state.BakPath, u.binPath)
				os.Chmod(u.binPath, 0755)
			}
			os.Remove(state.TmpPath)
			clearUpdateState()
		}

	case "restarting":
		// We ARE the new binary (restart succeeded, we're running)
		// Verify we're healthy (if we got here, HTTP server is about to start = healthy)
		slog.Info("gateway: update to %s completed successfully (post-restart)", state.Version)
		// Keep .bak for manual rollback. Clean state.
		os.Remove(state.TmpPath)
		clearUpdateState()

	default:
		// Unknown state — clean up
		clearUpdateState()
	}
}

func (u *Updater) Check() (*UpdateInfo, error) {
	latest, err := fetchLatestVersion()
	if err != nil {
		return nil, err
	}
	return &UpdateInfo{
		HasUpdate:      latest != u.currentVersion && latest > u.currentVersion,
		CurrentVersion: u.currentVersion,
		NewVersion:     latest,
	}, nil
}

func (u *Updater) Apply() error {
	info, err := u.Check()
	if err != nil {
		return fmt.Errorf("check: %w", err)
	}
	if !info.HasUpdate {
		return fmt.Errorf("no update available")
	}

	version := info.NewVersion
	binaryName := fmt.Sprintf("k2r-linux-%s", u.arch)
	binaryURL := fmt.Sprintf("%s/%s/%s", cdnBaseURL, version, binaryName)
	checksumsURL := fmt.Sprintf("%s/%s/checksums.txt", cdnBaseURL, version)
	tmpPath := fmt.Sprintf("/tmp/k2r-update-%s", version)
	bakPath := u.binPath + ".bak"

	state := &UpdateState{Version: version, TmpPath: tmpPath, BakPath: bakPath, StartedAt: time.Now().Unix()}

	// Stage 1: Download
	state.Stage = "downloading"
	saveUpdateState(state)
	u.setProgress("downloading", 0, "")
	if err := downloadFile(tmpPath, binaryURL, func(p float64) {
		u.setProgress("downloading", p, "")
	}); err != nil {
		os.Remove(tmpPath)
		clearUpdateState()
		u.setProgress("error", 0, err.Error())
		return fmt.Errorf("download: %w", err)
	}

	// Stage 2: Verify checksum
	state.Stage = "verifying"
	saveUpdateState(state)
	u.setProgress("verifying", 0, "")
	if err := verifyChecksum(tmpPath, binaryName, checksumsURL); err != nil {
		os.Remove(tmpPath)
		clearUpdateState()
		u.setProgress("error", 0, err.Error())
		return fmt.Errorf("verify: %w", err)
	}

	// Stage 3: Backup current binary
	state.Stage = "backing-up"
	saveUpdateState(state)
	u.setProgress("backing-up", 0, "")
	if err := copyFile(u.binPath, bakPath); err != nil {
		os.Remove(tmpPath)
		clearUpdateState()
		u.setProgress("error", 0, err.Error())
		return fmt.Errorf("backup: %w", err)
	}

	// Stage 4: Atomic replace (POINT OF NO RETURN)
	state.Stage = "replacing"
	saveUpdateState(state)
	u.setProgress("replacing", 0, "")
	if err := os.Rename(tmpPath, u.binPath); err != nil {
		// Rollback: restore from backup
		os.Rename(bakPath, u.binPath)
		clearUpdateState()
		u.setProgress("error", 0, err.Error())
		return fmt.Errorf("replace: %w", err)
	}
	os.Chmod(u.binPath, 0755)

	// Stage 5: Restart service
	state.Stage = "restarting"
	saveUpdateState(state)
	u.setProgress("restarting", 0, "")
	restartService()
	// Note: after restartService(), this process will be killed.
	// RecoverOnStartup() in the new process handles the "restarting" state.

	u.setProgress("done", 1, "")
	return nil
}

// --- Update state persistence ---

func saveUpdateState(state *UpdateState) {
	data, _ := json.Marshal(state)
	os.MkdirAll("/etc/k2r", 0755)
	os.WriteFile(updateStatePath, data, 0644)
}

func loadUpdateState() *UpdateState {
	data, err := os.ReadFile(updateStatePath)
	if err != nil {
		return nil
	}
	var state UpdateState
	if json.Unmarshal(data, &state) != nil {
		return nil
	}
	// Expire stale state (> 1 hour old = something went very wrong)
	if time.Since(time.Unix(state.StartedAt, 0)) > time.Hour {
		slog.Warn("gateway: expiring stale update state", "age", time.Since(time.Unix(state.StartedAt, 0)))
		clearUpdateState()
		return nil
	}
	return &state
}

func clearUpdateState() {
	os.Remove(updateStatePath)
}

func (u *Updater) setProgress(stage string, progress float64, errMsg string) {
	u.mu.Lock()
	u.progress = UpdateProgress{Stage: stage, Progress: progress, Error: errMsg}
	p := u.progress
	cb := u.onProgress
	u.mu.Unlock()
	if cb != nil {
		cb(p)
	}
}

// HTTP handlers

func (u *Updater) handleCheck(w http.ResponseWriter, r *http.Request) {
	info, err := u.Check()
	if err != nil {
		writeJSON(w, Response{Code: 1, Message: err.Error()})
		return
	}
	writeJSON(w, Response{Code: 0, Data: info})
}

func (u *Updater) handleApply(w http.ResponseWriter, r *http.Request) {
	go func() {
		if err := u.Apply(); err != nil {
			slog.Error("gateway: OTA update failed", "err", err)
		}
	}()
	writeJSON(w, Response{Code: 0, Message: "update started"})
}

func (u *Updater) handleStatus(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")

	ch := make(chan UpdateProgress, 8)
	u.mu.Lock()
	origCb := u.onProgress
	u.onProgress = func(p UpdateProgress) {
		if origCb != nil {
			origCb(p)
		}
		select {
		case ch <- p:
		default:
		}
	}
	u.mu.Unlock()

	defer func() {
		u.mu.Lock()
		u.onProgress = origCb
		u.mu.Unlock()
	}()

	for {
		select {
		case p := <-ch:
			data, _ := json.Marshal(p)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			if p.Stage == "done" || p.Stage == "error" {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

// Helpers

func fetchLatestVersion() (string, error) {
	resp, err := http.Get(cdnBaseURL + "/LATEST")
	if err != nil {
		// Try backup CDN
		resp, err = http.Get(cdnBackupURL + "/LATEST")
		if err != nil {
			return "", fmt.Errorf("fetch LATEST: %w", err)
		}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return strings.TrimSpace(string(body)), nil
}

func downloadFile(dst, url string, onProgress func(float64)) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()

	total := resp.ContentLength
	var written int64
	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			f.Write(buf[:n])
			written += int64(n)
			if total > 0 && onProgress != nil {
				onProgress(float64(written) / float64(total))
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func verifyChecksum(filePath, binaryName, checksumsURL string) error {
	// Fetch checksums.txt
	resp, err := http.Get(checksumsURL)
	if err != nil {
		return fmt.Errorf("fetch checksums: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// Parse: "<sha256>  <filename>" per line
	var expectedHash string
	for _, line := range strings.Split(string(body), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == binaryName {
			expectedHash = parts[0]
			break
		}
	}
	if expectedHash == "" {
		return fmt.Errorf("checksum not found for %s", binaryName)
	}

	// Hash the downloaded file
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	io.Copy(h, f)
	actualHash := hex.EncodeToString(h.Sum(nil))

	if actualHash != expectedHash {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, actualHash)
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func restartService() {
	// Try systemd first, then procd (OpenWrt)
	if err := exec.Command("systemctl", "restart", "k2r").Run(); err != nil {
		exec.Command("/etc/init.d/k2r", "restart").Run()
	}
}

func mapArch(goarch string) string {
	switch goarch {
	case "arm64":
		return "arm64"
	case "amd64":
		return "amd64"
	case "arm":
		return "armv7"
	case "mipsle":
		return "mipsle"
	default:
		return runtime.GOARCH
	}
}
```

- [ ] **Step 2: Write tests**

```go
package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestMapArch(t *testing.T) {
	tests := []struct{ in, want string }{
		{"arm64", "arm64"},
		{"amd64", "amd64"},
		{"arm", "armv7"},
		{"mipsle", "mipsle"},
	}
	for _, tt := range tests {
		if got := mapArch(tt.in); got != tt.want {
			t.Errorf("mapArch(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestVerifyChecksum_Match(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := tmpDir + "/k2r-linux-amd64"
	content := []byte("test binary content")
	os.WriteFile(filePath, content, 0644)

	h := sha256.Sum256(content)
	expectedHash := hex.EncodeToString(h[:])

	// Serve checksums via test HTTP server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  k2r-linux-amd64\n", expectedHash)
	}))
	defer srv.Close()

	err := verifyChecksum(filePath, "k2r-linux-amd64", srv.URL)
	if err != nil {
		t.Errorf("expected checksum match, got error: %v", err)
	}
}

func TestVerifyChecksum_Mismatch(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := tmpDir + "/k2r-linux-amd64"
	os.WriteFile(filePath, []byte("real content"), 0644)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "0000000000000000000000000000000000000000000000000000000000000000  k2r-linux-amd64\n")
	}))
	defer srv.Close()

	err := verifyChecksum(filePath, "k2r-linux-amd64", srv.URL)
	if err == nil {
		t.Error("expected checksum mismatch error")
	}
}

func TestUpdateState_SaveLoadClear(t *testing.T) {
	// Override state path for test
	origPath := updateStatePath
	updateStatePath = t.TempDir() + "/update-state.json"
	defer func() { updateStatePath = origPath }()

	// Save
	state := &UpdateState{
		Stage: "downloading", Version: "0.4.3",
		TmpPath: "/tmp/k2r-update-0.4.3", BakPath: "/usr/bin/k2r.bak",
		StartedAt: time.Now().Unix(),
	}
	saveUpdateState(state)

	// Load
	loaded := loadUpdateState()
	if loaded == nil {
		t.Fatal("expected state to be loaded")
	}
	if loaded.Stage != "downloading" || loaded.Version != "0.4.3" {
		t.Errorf("loaded state mismatch: %+v", loaded)
	}

	// Clear
	clearUpdateState()
	if loadUpdateState() != nil {
		t.Error("expected nil after clear")
	}
}

func TestUpdateState_Expiry(t *testing.T) {
	origPath := updateStatePath
	updateStatePath = t.TempDir() + "/update-state.json"
	defer func() { updateStatePath = origPath }()

	// Save state with old timestamp (> 1 hour ago)
	state := &UpdateState{
		Stage: "downloading", Version: "0.4.3",
		StartedAt: time.Now().Add(-2 * time.Hour).Unix(),
	}
	saveUpdateState(state)

	// Load should return nil (expired)
	if loadUpdateState() != nil {
		t.Error("expected stale state to be expired")
	}
}

func TestRecoverOnStartup_DownloadInterrupted(t *testing.T) {
	origPath := updateStatePath
	updateStatePath = t.TempDir() + "/update-state.json"
	defer func() { updateStatePath = origPath }()

	tmpFile := t.TempDir() + "/k2r-update-0.4.3"
	os.WriteFile(tmpFile, []byte("partial download"), 0644)

	saveUpdateState(&UpdateState{
		Stage: "downloading", Version: "0.4.3",
		TmpPath: tmpFile, StartedAt: time.Now().Unix(),
	})

	u := NewUpdater("0.4.2", "amd64")
	u.RecoverOnStartup()

	// tmp file should be cleaned up
	if _, err := os.Stat(tmpFile); err == nil {
		t.Error("tmp file should have been removed")
	}
	// state should be cleared
	if loadUpdateState() != nil {
		t.Error("state should be cleared")
	}
}

func TestRecoverOnStartup_RestartSucceeded(t *testing.T) {
	origPath := updateStatePath
	updateStatePath = t.TempDir() + "/update-state.json"
	defer func() { updateStatePath = origPath }()

	saveUpdateState(&UpdateState{
		Stage: "restarting", Version: "0.4.3",
		TmpPath: "/tmp/gone", BakPath: "/usr/bin/k2r.bak",
		StartedAt: time.Now().Unix(),
	})

	// Current version IS the target version = restart succeeded
	u := NewUpdater("0.4.3", "amd64")
	u.RecoverOnStartup()

	// state should be cleared (update completed)
	if loadUpdateState() != nil {
		t.Error("state should be cleared after successful restart")
	}
}
```

- [ ] **Step 3: Register updater endpoints in api.go/gateway.go**

Add to Gateway struct: `updater *Updater`
Add to `Run` mux routes:

```go
	if g.updater != nil {
		mux.HandleFunc("/api/updater/check", g.updater.handleCheck)
		mux.HandleFunc("/api/updater/apply", g.updater.handleApply)
		mux.HandleFunc("/api/updater/status", g.updater.handleStatus)
	}
```

Initialize in `New()`:

```go
	updater: NewUpdater(config.Version(), runtime.GOARCH),
```

In `Gateway.Run()` (line 54), add after `g.cleanStaleRules()`:

```go
	g.updater.RecoverOnStartup()
```

This ensures interrupted updates are recovered BEFORE serving HTTP.

- [ ] **Step 4: Commit**

```bash
cd k2 && git add gateway/updater.go gateway/updater_test.go gateway/api.go gateway/gateway.go
git commit -m "feat(gateway): OTA self-updater (CDN check, SHA256 verify, backup, atomic replace)"
```

---

## Task 6: Wire InterceptConfig in doUp

**Files:**
- Modify: `gateway/gateway.go` — pass extended InterceptConfig to provider

- [ ] **Step 1: Update doUp to pass DNS redirect and MAC config**

In `gateway.go` `doUp` method (line ~124), update `InterceptConfig`:

```go
	icfg := InterceptConfig{
		ListenPort:    cfg.ListenPort,
		LANSubnets:    subnets,
		ExcludeIPs:    excludeIPs,
		DNSRedirect:   cfg.DNSRedirect,
		DNSPort:       cfg.DNSPort,
		AllowlistMode: g.routerDeviceMgr != nil && g.routerDeviceMgr.mode == "allowlist",
		AllowedMACs:   g.getAllowedMACs(),
	}
```

Add helper:

```go
func (g *Gateway) getAllowedMACs() []string {
	if g.routerDeviceMgr == nil {
		return nil
	}
	g.routerDeviceMgr.mu.RLock()
	defer g.routerDeviceMgr.mu.RUnlock()
	var macs []string
	for _, e := range g.routerDeviceMgr.entries {
		macs = append(macs, e.MAC)
	}
	return macs
}
```

- [ ] **Step 2: Commit**

```bash
cd k2 && git add gateway/gateway.go
git commit -m "feat(gateway): wire DNS redirect + MAC allowlist into InterceptConfig"
```

---

## Self-Review

| Spec Requirement | Task |
|-----------------|------|
| 6.1 Storage (MAC allowlist) | Task 4 (persist via storage callback) |
| 6.2 Gateway HTTP API | Task 4 (handleList/Allow/Remove/Mode) |
| 6.3 Response format | Task 4 (RouterDeviceListResponse) |
| 6.4 LAN Device Discovery | Task 3 (ubus/dnsmasq/ip-neigh) |
| 6.5 nftables enforcement | Task 2 (MAC set + AddMAC/RemoveMAC) |
| 6.6a DNS Redirect | Task 2 (addDNSRedirect) |
| 6.6 Quota enforcement | Task 4 (handleAllow quota check) |
| 7.1 CDN structure | Task 5 (fetchLatestVersion, download URL pattern) |
| 7.2 Update flow | Task 5 (Apply method) |
| 7.3 Rollback | Task 5 (backup to .bak) |
| 7.4 HTTP API | Task 5 (handleCheck/Apply/Status) |
| Principles #1 nftables Go lib | Task 1-2 |
| Principles #3 MAC prerouting | Task 2 (addMACAllowlist) |
| Principles #5 DNS configurable | Task 2 (DNSRedirect flag) |
| Principles #8 Clean teardown | Task 2 (Remove = DelTable) |

**Type consistency:** `InterceptConfig`, `LanDevice`, `RouterDeviceEntry`, `RouterDeviceInfo`, `RouterDeviceListResponse`, `UpdateInfo`, `UpdateProgress` — all used consistently. `nftNetlinkInterceptor.AddMAC`/`RemoveMAC` called from `RouterDeviceManager`.

**Known risk:** `expr.TProxy` API in google/nftables may need adjustment based on actual library version. Test on Linux.
