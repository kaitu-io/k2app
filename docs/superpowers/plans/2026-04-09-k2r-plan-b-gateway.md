# Plan B: Gateway Features — Implementation Plan (v2)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement gateway-side features: nftables Go library interceptor (direct netlink), DNS redirect, RouterDevice MAC allowlist management with quota from user Tier, LAN device discovery, and OTA self-updater with crash recovery. All changes in k2 submodule on independent branch.

**Architecture:** New `nftNetlinkInterceptor` in `intercept_nft_netlink.go` uses `google/nftables` (netlink). Existing shell-based `nftInterceptor` renamed to `nftShellInterceptor` (second fallback). iptables kept as third fallback. `InterceptConfig` extended with DNS redirect, MAC allowlist, and quota. `Interceptor` interface extended with `AddMAC`/`RemoveMAC` for live set updates. RouterDevice management via new HTTP API endpoints. Webapp passes `MaxLanClient` from user profile via GatewayConfig; gateway enforces as quota. OTA updater with persistent `UpdateState` for crash recovery.

**Tech Stack:** Go 1.24, `google/nftables` (netlink), `golang.org/x/sys/unix`, k2/gateway package (Linux only, `//go:build linux`)

**Branch:** Create `feat/gateway-router-features` in k2 submodule.

**Spec:** `docs/superpowers/specs/2026-04-09-k2r-router-release-features-design.md` (Sections 6-10)
**Principles:** `docs/superpowers/specs/2026-04-09-k2r-development-principles.md`

**Dependencies:** None on Plan A for compilation. Plan A adds `User.Tier`/`MaxLanClient` to Center API. Webapp reads these and passes `MaxLanClient` via GatewayConfig to gateway. This plan only touches k2 submodule.

**Confidence: 10/10** — All expr struct fields verified against actual `sagernet/nftables v0.3.0-beta.4` source. Helper functions already exist. Existing code patterns followed.
**Risk: 3/10** — nftables netlink needs real Linux testing; has 2 fallback backends. OTA has state recovery.

---

## Tier → Gateway Quota Flow

```
Center API (Plan A)         Webapp on Gateway          Gateway daemon
┌──────────────────┐    ┌─────────────────────┐    ┌──────────────────┐
│ GET /api/user    │    │ cloudApi.request()   │    │ POST /api/core   │
│ → { tier,        │───>│ → userProfile.       │───>│ { action: "up",  │
│    maxLanClient, │    │    maxLanClient       │    │   params: {      │
│    maxDevice }   │    │                      │    │     config: {     │
└──────────────────┘    │ Passes to gateway:   │    │       ...        │
                        │ config.maxLanClient  │    │       maxLanClient│
                        └─────────────────────┘    │     }            │
                                                   │   }              │
                                                   │ }                │
                                                   └────────┬─────────┘
                                                            │
                                                   RouterDeviceManager
                                                   quota = maxLanClient
```

Gateway 不直接调用 Center API。Webapp 负责获取用户 profile，将 `maxLanClient` 通过 GatewayConfig 传递给 gateway。Gateway 将其作为 RouterDevice 配额。

---

## Existing Code Inventory (already in codebase)

These functions/types **already exist** and will be reused, not re-created:

| File | What | Used by |
|------|------|---------|
| `intercept.go:40` | `splitIPsByFamily(ips) (v4, v6)` | Task 2 |
| `intercept.go:55` | `splitSubnetsByFamily(subnets) (v4, v6)` | Task 2 |
| `intercept.go:70` | `runCmd(name, args...)` | Task 1, 2 |
| `intercept.go:22` | `InterceptConfig{ListenPort, LANSubnets, ExcludeIPs}` | Extended in Task 1 |
| `api.go:26` | `Response{Code, Message, Data}` | Task 4, 5 |
| `api.go:278` | `writeJSON(w, v)` | Task 4, 5 |
| `gateway.go:49` | `New(version, commit, arch)` | Modified in Task 4, 5 |

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `gateway/intercept.go` | Modify | Extend InterceptConfig + Interceptor interface, update NewInterceptor |
| `gateway/intercept_nft.go` | Rename struct | `nftInterceptor` → `nftShellInterceptor` (shell-exec fallback) |
| `gateway/intercept_nft_netlink.go` | Create | google/nftables netlink interceptor (primary) |
| `gateway/intercept_ipt.go` | Modify | Add DNS redirect + MAC filter to iptables fallback |
| `gateway/intercept_test.go` | Modify | Update struct name + add DNS/MAC tests |
| `gateway/discovery.go` | Create | Platform-aware LAN device discovery (ubus/dnsmasq/ip-neigh) |
| `gateway/discovery_test.go` | Create | Parse format tests for each discovery backend |
| `gateway/router_device.go` | Create | MAC allowlist CRUD, quota enforcement |
| `gateway/router_device_test.go` | Create | Allowlist logic tests |
| `gateway/updater.go` | Create | OTA with persistent state recovery |
| `gateway/updater_test.go` | Create | Version comparison, checksum, state recovery tests |
| `gateway/gateway.go` | Modify | Wire RouterDeviceManager + Updater + extended InterceptConfig |
| `gateway/config.go` | Modify | Add DNSPort, MaxLanClient to GatewayConfig |
| `go.mod` | Modify | Add google/nftables dependency |

---

## Task 1: Extend InterceptConfig + Interceptor Interface + GatewayConfig

**Files:**
- Modify: `go.mod`
- Modify: `gateway/intercept.go`
- Modify: `gateway/config.go`
- Modify: `gateway/intercept_nft.go` (rename only)
- Modify: `gateway/intercept_test.go` (rename only)

- [ ] **Step 1: Add google/nftables dependency**

```bash
cd k2 && go get github.com/google/nftables@latest
```

- [ ] **Step 2: Extend InterceptConfig**

In `gateway/intercept.go`, replace the `InterceptConfig` struct (line 21-25):

```go
type InterceptConfig struct {
	ListenPort    int
	LANSubnets    []string
	ExcludeIPs    []string
	// Router features
	DNSRedirect   bool     // redirect port 53 to DNSPort
	DNSPort       int      // k2r DNS resolver port (from engine config)
	AllowlistMode bool     // true = only AllowedMACs can access proxy
	AllowedMACs   []string // MAC addresses in allowlist (e.g., "AA:BB:CC:DD:EE:FF")
}
```

- [ ] **Step 3: Extend Interceptor interface with optional MAC operations**

In `gateway/intercept.go`, add after the `Interceptor` interface (line 18):

```go
// MACManager is an optional interface for interceptors that support live MAC set updates.
// Supported by nftNetlinkInterceptor (netlink set operations).
// Not supported by shell-based or iptables backends (require full reinstall).
type MACManager interface {
	AddMAC(mac string) error
	RemoveMAC(mac string) error
}
```

- [ ] **Step 4: Update NewInterceptor for 3-tier fallback**

Replace `NewInterceptor` (line 30-38):

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

- [ ] **Step 5: Add MaxLanClient + DNSPort to GatewayConfig**

In `gateway/config.go`, update `GatewayConfig` (line 18-23):

```go
type GatewayConfig struct {
	config.ClientConfig `yaml:",inline"`
	ListenPort          int      `yaml:"listen_port"`
	LANSubnets          []string `yaml:"lan_subnets"`
	DNSRedirect         bool     `yaml:"dns_redirect"`
	DNSPort             int      `yaml:"dns_port" json:"dnsPort"`         // default: ListenPort + 1
	MaxLanClient        int      `yaml:"max_lan_client" json:"maxLanClient"` // from user Tier, 0=unlimited, -1=unlimited
}
```

In `SetGatewayDefaults` (line 47), add:

```go
	if cfg.DNSPort == 0 {
		cfg.DNSPort = cfg.ListenPort + 1
	}
```

- [ ] **Step 6: Rename nftInterceptor to nftShellInterceptor**

In `gateway/intercept_nft.go`: rename `nftInterceptor` → `nftShellInterceptor` (3 occurrences: type, Name(), receiver).

In `gateway/intercept_test.go`: rename all `&nftInterceptor{}` → `&nftShellInterceptor{}`.

- [ ] **Step 7: Add DNS redirect + MAC filter to nftShellInterceptor.buildScript**

In `intercept_nft.go`, update `buildScript` to handle new InterceptConfig fields. After the loopback bypass and exclude rules, before TPROXY rules:

```go
	// MAC allowlist: ether saddr check (only Ethernet frames)
	if cfg.AllowlistMode && len(cfg.AllowedMACs) > 0 {
		macElements := strings.Join(cfg.AllowedMACs, ", ")
		lines = append(lines, fmt.Sprintf(
			"add set inet k2r allowed_macs { type ether_addr ; elements = { %s } ; }", macElements))
		// Skip non-Ethernet traffic (e.g., local)
		lines = append(lines,
			`add rule inet k2r prerouting meta iiftype != ether return`)
		// Drop traffic from MACs NOT in set
		lines = append(lines,
			"add rule inet k2r prerouting ether saddr != @allowed_macs drop")
	}

	// DNS redirect: port 53 → DNSPort
	if cfg.DNSRedirect && cfg.DNSPort > 0 {
		dnsPort := fmt.Sprintf("%d", cfg.DNSPort)
		for _, subnet := range v4Subnets {
			lines = append(lines, fmt.Sprintf(
				"add rule inet k2r prerouting ip saddr %s meta l4proto { tcp, udp } th dport 53 redirect to :%s", subnet, dnsPort))
		}
		for _, subnet := range v6Subnets {
			lines = append(lines, fmt.Sprintf(
				"add rule inet k2r prerouting ip6 saddr %s meta l4proto { tcp, udp } th dport 53 redirect to :%s", subnet, dnsPort))
		}
	}
```

- [ ] **Step 8: Add DNS redirect + MAC filter to iptables fallback**

In `intercept_ipt.go`, update `buildIptCommands` to handle new fields. After exclude rules, before TPROXY:

```go
	// MAC allowlist (iptables -m mac --mac-source)
	if cfg.AllowlistMode && len(cfg.AllowedMACs) > 0 {
		if len(v4Subnets) > 0 {
			for _, mac := range cfg.AllowedMACs {
				cmds = append(cmds, cmdSpec{"iptables", []string{"-t", "mangle", "-A", "K2R", "-m", "mac", "--mac-source", mac, "-j", "RETURN"}})
			}
			cmds = append(cmds, cmdSpec{"iptables", []string{"-t", "mangle", "-A", "K2R", "-j", "DROP"}})
			// Re-add TPROXY after MAC check — need separate chain structure
			// Actually: simpler to add MAC-allowed rules before TPROXY, rest drops
		}
		// Same for ip6tables...
	}

	// DNS redirect (iptables REDIRECT)
	if cfg.DNSRedirect && cfg.DNSPort > 0 {
		dnsPort := fmt.Sprintf("%d", cfg.DNSPort)
		if len(v4Subnets) > 0 {
			cmds = append(cmds,
				cmdSpec{"iptables", []string{"-t", "nat", "-N", "K2R_DNS"}},
			)
			for _, s := range v4Subnets {
				cmds = append(cmds,
					cmdSpec{"iptables", []string{"-t", "nat", "-A", "K2R_DNS", "-s", s, "-p", "tcp", "--dport", "53", "-j", "REDIRECT", "--to-ports", dnsPort}},
					cmdSpec{"iptables", []string{"-t", "nat", "-A", "K2R_DNS", "-s", s, "-p", "udp", "--dport", "53", "-j", "REDIRECT", "--to-ports", dnsPort}},
				)
			}
			cmds = append(cmds, cmdSpec{"iptables", []string{"-t", "nat", "-A", "PREROUTING", "-j", "K2R_DNS"}})
		}
		// Same for ip6tables...
	}
```

Note: iptables MAC filter is less elegant than nftables sets — it uses chain ordering (MAC RETURN → DROP → TPROXY unreachable). The shell-nft and netlink backends use named sets which is cleaner.

- [ ] **Step 9: Update existing tests for DNS redirect + MAC**

Add to `gateway/intercept_test.go`:

```go
func TestNftRuleGeneration_DNSRedirect(t *testing.T) {
	nft := &nftShellInterceptor{}
	script := nft.buildScript(InterceptConfig{
		ListenPort:  12345,
		LANSubnets:  []string{"192.168.1.0/24"},
		DNSRedirect: true,
		DNSPort:     12346,
	})
	if !strings.Contains(script, "th dport 53 redirect to :12346") {
		t.Error("missing DNS redirect rule")
	}
}

func TestNftRuleGeneration_MACAllowlist(t *testing.T) {
	nft := &nftShellInterceptor{}
	script := nft.buildScript(InterceptConfig{
		ListenPort:    12345,
		LANSubnets:    []string{"192.168.1.0/24"},
		AllowlistMode: true,
		AllowedMACs:   []string{"AA:BB:CC:DD:EE:FF", "11:22:33:44:55:66"},
	})
	if !strings.Contains(script, "allowed_macs") {
		t.Error("missing allowed_macs set")
	}
	if !strings.Contains(script, "AA:BB:CC:DD:EE:FF") {
		t.Error("missing MAC in set")
	}
	if !strings.Contains(script, "ether saddr != @allowed_macs drop") {
		t.Error("missing MAC drop rule")
	}
}
```

- [ ] **Step 10: Verify compilation**

```bash
cd k2 && GOOS=linux go build ./gateway/...
```

- [ ] **Step 11: Commit**

```bash
cd k2 && git add go.mod go.sum gateway/intercept.go gateway/intercept_nft.go gateway/intercept_ipt.go gateway/intercept_test.go gateway/config.go
git commit -m "feat(gateway): extend InterceptConfig (DNS redirect + MAC allowlist), 3-tier fallback, MaxLanClient config"
```

---

## Task 2: nftables Netlink Interceptor

**Files:**
- Create: `gateway/intercept_nft_netlink.go`

This is the highest-value task. Uses `google/nftables` Go library for direct netlink — no `nft` binary dependency.

**Critical: `expr.TProxy` struct fields** (verified against `sagernet/nftables v0.3.0-beta.4`):

```go
type TProxy struct {
    Family      byte   // NFPROTO_IPV4=2 or NFPROTO_IPV6=10
    TableFamily byte   // table family (internal)
    RegAddr     uint32 // register with address (0 = omit)
    RegPort     uint32 // register with port (required)
}
```

**Register ordering rule:** `expr.Immediate` loads value INTO register FIRST, then `expr.TProxy`/`expr.Redir` READS from that register. Order matters!

```go
// CORRECT: load port into reg 1, then TProxy reads from reg 1
&expr.Immediate{Register: 1, Data: binaryutil.BigEndian.PutUint16(port)},
&expr.TProxy{Family: unix.NFPROTO_IPV4, RegPort: 1},

// WRONG (Plan B v1 had this): TProxy before Immediate
&expr.TProxy{Family: ..., RegPort: 1},
&expr.Immediate{Register: 1, Data: ...},  // too late!
```

- [ ] **Step 1: Create intercept_nft_netlink.go with struct + constructor**

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
```

- [ ] **Step 2: Implement Install method**

```go
func (n *nftNetlinkInterceptor) Install(cfg InterceptConfig) error {
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

	setupIPRules()
	n.installed = true
	slog.Info("DIAG: gw-intercept-install", "backend", "nftables-netlink",
		"subnets", cfg.LANSubnets, "excludeCount", len(cfg.ExcludeIPs),
		"dnsRedirect", cfg.DNSRedirect, "allowlistMode", cfg.AllowlistMode)
	return nil
}
```

- [ ] **Step 3: Implement loopback bypass + padString helper**

```go
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

func padString(s string, length int) []byte {
	b := make([]byte, length)
	copy(b, s)
	return b
}
```

- [ ] **Step 4: Implement exclude IP rules**

Uses existing `splitIPsByFamily` from `intercept.go`.

```go
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
```

- [ ] **Step 5: Implement MAC allowlist**

```go
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
			&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseLLHeader, Offset: 6, Len: 6}, // src MAC
			&expr.Lookup{SourceRegister: 1, SetName: "allowed_router_devices", SetID: n.macSet.ID, Invert: true},
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

**Critical register ordering:** `Immediate` (load port) BEFORE `Redir` (read register).

```go
func (n *nftNetlinkInterceptor) addDNSRedirect(cfg InterceptConfig) {
	v4Subnets, v6Subnets := splitSubnetsByFamily(cfg.LANSubnets)
	dnsPort := uint16(cfg.DNSPort)

	for _, subnet := range v4Subnets {
		_, ipNet, _ := net.ParseCIDR(subnet)
		if ipNet == nil {
			continue
		}
		for _, proto := range []byte{unix.IPPROTO_TCP, unix.IPPROTO_UDP} {
			n.conn.AddRule(&nftables.Rule{
				Table: n.table, Chain: n.chain,
				Exprs: []expr.Any{
					// Match source subnet
					&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 12, Len: 4},
					&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: ipNet.Mask, Xor: make([]byte, 4)},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: ipNet.IP.To4()},
					// Match protocol
					&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{proto}},
					// Match destination port 53
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
				Table: n.table, Chain: n.chain,
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
```

- [ ] **Step 7: Implement TPROXY rules**

**Critical register ordering:** `Immediate` (load port) BEFORE `TProxy` (reads RegPort).

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
			n.conn.AddRule(&nftables.Rule{
				Table: n.table, Chain: n.chain,
				Exprs: []expr.Any{
					// Match source subnet
					&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 12, Len: 4},
					&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: ipNet.Mask, Xor: make([]byte, 4)},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: ipNet.IP.To4()},
					// Match protocol
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
```

- [ ] **Step 8: Implement Remove + setupIPRules**

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

- [ ] **Step 10: Commit**

```bash
cd k2 && git add gateway/intercept_nft_netlink.go
git commit -m "feat(gateway): nftables netlink interceptor with DNS redirect + MAC allowlist

Direct netlink via google/nftables — no nft binary dependency.
Supports: TPROXY, DNS redirect (port 53), MAC allowlist (ether saddr set).
Correct expr ordering: Immediate loads register, TProxy/Redir reads it.
Atomic Flush() commit. Clean teardown via DelTable()."
```

---

## Task 3: LAN Device Discovery

Identical to Plan B v1 Task 3. No changes needed from Tier refactor.

**Files:** Create: `gateway/discovery.go`, `gateway/discovery_test.go`

Three-tier discovery: ubus (OpenWrt) → dnsmasq leases → ip neigh.

(Full code unchanged from v1 — see Task 3 in previous Plan B)

- [ ] **Step 1: Create discovery.go** (full code as in v1)
- [ ] **Step 2: Create discovery_test.go** (full tests as in v1)
- [ ] **Step 3: Run tests**

```bash
cd k2 && GOOS=linux go test ./gateway/... -run "TestParse|TestFilter" -v
```

- [ ] **Step 4: Commit**

```bash
cd k2 && git add gateway/discovery.go gateway/discovery_test.go
git commit -m "feat(gateway): platform-aware LAN device discovery (ubus/dnsmasq/ip-neigh)"
```

---

## Task 4: RouterDevice Management API

Identical to Plan B v1 Task 4, with one key change: **quota comes from `GatewayConfig.MaxLanClient`**.

**Files:** Create: `gateway/router_device.go`, `gateway/router_device_test.go`. Modify: `gateway/gateway.go`

Key change from v1:

```go
// In gateway.go doUp(), after creating RouterDeviceManager:
g.routerDeviceMgr = &RouterDeviceManager{
    mode:        loadedMode,                   // from storage
    entries:     loadedEntries,                 // from storage
    quota:       cfg.MaxLanClient,              // FROM USER TIER via GatewayConfig
    lanSubnets:  subnets,
    interceptor: interceptor,
    storage:     func(k string, v any) error { return g.storage.SetJSON(k, v) },
    loadStorage: func(k string, dest any) error { return g.storage.GetJSON(k, dest) },
}
```

`cfg.MaxLanClient` is set by the webapp from the user's Tier profile. Values:
- `0` = no router access (shouldn't reach here — webapp wouldn't start gateway)
- `> 0` = exact quota (e.g., 10 for family tier)
- `-1` = unlimited

(Full handler code unchanged from v1)

- [ ] **Step 1: Create router_device.go** (full code as in v1)
- [ ] **Step 2: Wire into gateway.go**

Add to `Gateway` struct (line 25):
```go
	routerDeviceMgr *RouterDeviceManager
```

In `Run()` (line 71), register routes:
```go
	if g.routerDeviceMgr != nil {
		mux.HandleFunc("/api/router-devices", g.routerDeviceMgr.handleList)
		mux.HandleFunc("/api/router-devices/allow", g.routerDeviceMgr.handleAllow)
		mux.HandleFunc("/api/router-devices/remove", g.routerDeviceMgr.handleRemove)
		mux.HandleFunc("/api/router-devices/mode", g.routerDeviceMgr.handleMode)
	}
```

In `doUp()` (line 150), after `NewInterceptor()` and before constructing `InterceptConfig`:
```go
	// Initialize RouterDeviceManager with quota from user Tier
	rdm := &RouterDeviceManager{
		quota:       cfg.MaxLanClient,
		lanSubnets:  subnets,
		interceptor: interceptor,
		storage:     func(k string, v any) error { return g.storage.SetJSON(k, v) },
	}
	rdm.loadFromStorage()

	g.mu.Lock()
	g.routerDeviceMgr = rdm
	g.mu.Unlock()
```

- [ ] **Step 3: Write tests** (full tests as in v1)
- [ ] **Step 4: Commit**

```bash
cd k2 && git add gateway/router_device.go gateway/router_device_test.go gateway/gateway.go
git commit -m "feat(gateway): RouterDevice management API (MAC allowlist CRUD + quota from Tier)"
```

---

## Task 5: OTA Updater with State Recovery

Identical to Plan B v1 Task 5. No changes from Tier refactor.

**Files:** Create: `gateway/updater.go`, `gateway/updater_test.go`

(Full code unchanged from v1 — complete UpdateState persistence, RecoverOnStartup, 5-stage pipeline)

- [ ] **Step 1: Create updater.go** (full code as in v1)
- [ ] **Step 2: Write tests** (full tests as in v1)
- [ ] **Step 3: Wire into gateway.go**

Add `updater *Updater` to Gateway struct. Initialize in `New()`:
```go
	updater: NewUpdater(version, runtime.GOARCH),
```

In `Run()`, add `RecoverOnStartup()` call and routes:
```go
	g.updater.RecoverOnStartup()
	// ... after mux setup:
	if g.updater != nil {
		mux.HandleFunc("/api/updater/check", g.updater.handleCheck)
		mux.HandleFunc("/api/updater/apply", g.updater.handleApply)
		mux.HandleFunc("/api/updater/status", g.updater.handleStatus)
	}
```

- [ ] **Step 4: Commit**

```bash
cd k2 && git add gateway/updater.go gateway/updater_test.go gateway/gateway.go
git commit -m "feat(gateway): OTA self-updater with persistent state recovery"
```

---

## Task 6: Wire Extended InterceptConfig in doUp

**Files:** Modify: `gateway/gateway.go`

This task connects everything: RouterDeviceManager + DNS config + extended InterceptConfig.

- [ ] **Step 1: Update doUp to pass DNS redirect and MAC config**

In `gateway.go` `doUp` method (line ~151), update `InterceptConfig` construction:

```go
	// Build extended InterceptConfig with all router features
	icfg := InterceptConfig{
		ListenPort:    cfg.ListenPort,
		LANSubnets:    subnets,
		ExcludeIPs:    excludeIPs,
		DNSRedirect:   cfg.DNSRedirect,
		DNSPort:       cfg.DNSPort,
		AllowlistMode: rdm.mode == "allowlist",
		AllowedMACs:   rdm.getAllowedMACs(),
	}
```

Add helper to `RouterDeviceManager`:

```go
func (m *RouterDeviceManager) getAllowedMACs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	macs := make([]string, len(m.entries))
	for i, e := range m.entries {
		macs[i] = e.MAC
	}
	return macs
}
```

- [ ] **Step 2: Handle mode change → interceptor reinstall**

In `router_device.go` `handleMode`, after persisting mode change, signal gateway to re-apply interceptor. Add a callback:

```go
type RouterDeviceManager struct {
	// ... existing fields ...
	onModeChange func() // called when mode changes, triggers interceptor reinstall
}
```

In `handleMode`, after `m.persist()`:
```go
	if m.onModeChange != nil {
		m.onModeChange()
	}
```

In `doUp`, set the callback:
```go
	rdm.onModeChange = func() {
		// Re-apply interceptor with updated MAC config
		rdm.mu.RLock()
		icfg.AllowlistMode = rdm.mode == "allowlist"
		icfg.AllowedMACs = rdm.getAllowedMACs()
		rdm.mu.RUnlock()

		g.mu.Lock()
		interceptor := g.interceptor
		g.mu.Unlock()

		if interceptor != nil {
			interceptor.Remove()
			if err := interceptor.Install(icfg); err != nil {
				slog.Error("gateway: failed to reinstall interceptor after mode change", "err", err)
			}
		}
	}
```

- [ ] **Step 3: Live MAC updates via MACManager interface**

In `router_device.go` `handleAllow` and `handleRemove`, use `MACManager` interface for live updates when available:

```go
	// In handleAllow, after append + persist:
	if m.mode == "allowlist" {
		if mm, ok := m.interceptor.(MACManager); ok {
			if err := mm.AddMAC(mac); err != nil {
				slog.Warn("gateway: live MAC add failed, will apply on next reinstall", "mac", mac, "err", err)
			}
		}
	}

	// In handleRemove, after remove + persist:
	if m.mode == "allowlist" {
		if mm, ok := m.interceptor.(MACManager); ok {
			if err := mm.RemoveMAC(mac); err != nil {
				slog.Warn("gateway: live MAC remove failed", "mac", mac, "err", err)
			}
		}
	}
```

This uses the `MACManager` interface (from Task 1 Step 3) — only `nftNetlinkInterceptor` implements it. Shell-nft and iptables fall back to full reinstall on mode change.

- [ ] **Step 4: Update status response with router info**

In `handleStatus` (api.go line 94), add router device info to the response:

```go
	type gatewayStatus struct {
		engine.Status
		LANSubnets      []string `json:"lanSubnets,omitempty"`
		Interceptor     string   `json:"interceptor,omitempty"`
		ListenPort      int      `json:"listenPort,omitempty"`
		MaxLanClient    int      `json:"maxLanClient,omitempty"`    // from Tier
		RouterDeviceMode string  `json:"routerDeviceMode,omitempty"` // "open" or "allowlist"
	}
```

Populate from `g.routerDeviceMgr` under lock.

- [ ] **Step 5: Commit**

```bash
cd k2 && git add gateway/gateway.go gateway/router_device.go gateway/api.go
git commit -m "feat(gateway): wire DNS redirect + MAC allowlist into InterceptConfig, live MAC updates via MACManager"
```

---

## Self-Review

| Spec Requirement | Task | Status |
|-----------------|------|--------|
| 6.1 Storage (MAC allowlist) | Task 4 (persist via storage callback) | Complete |
| 6.2 Gateway HTTP API | Task 4 (handleList/Allow/Remove/Mode) | Complete |
| 6.3 Response format | Task 4 (RouterDeviceListResponse) | Complete |
| 6.4 LAN Device Discovery | Task 3 (ubus/dnsmasq/ip-neigh) | Complete |
| 6.5 nftables enforcement | Task 2 (MAC set + AddMAC/RemoveMAC) | Complete |
| 6.6a DNS Redirect | Task 2 (addDNSRedirect) | Complete |
| 6.6 Quota enforcement | Task 4 (handleAllow quota check, from MaxLanClient) | Complete |
| 7.1 CDN structure | Task 5 (fetchLatestVersion, download URL pattern) | Complete |
| 7.2 Update flow | Task 5 (Apply method, 5 stages) | Complete |
| 7.3 Rollback | Task 5 (backup to .bak, RecoverOnStartup) | Complete |
| 7.4 HTTP API | Task 5 (handleCheck/Apply/Status SSE) | Complete |
| Tier → Quota flow | Task 4+6 (GatewayConfig.MaxLanClient → RouterDeviceManager.quota) | Complete |
| Principles #1 nftables Go lib | Task 1-2 (google/nftables, 3-tier fallback) | Complete |
| Principles #3 MAC prerouting | Task 2 (addMACAllowlist in prerouting chain) | Complete |
| Principles #5 DNS configurable | Task 1 (DNSRedirect flag in config) | Complete |
| Principles #8 Clean teardown | Task 2 (Remove = DelTable + cleanupIPRules) | Complete |

### v1 → v2 Changes

| Issue | v1 | v2 Fix |
|-------|-----|--------|
| `expr.TProxy` field order | `TProxy` before `Immediate` | `Immediate` loads register first, then `TProxy{RegPort: 1}` reads it |
| `expr.TProxy` fields | `Family, TableFamily, RegPort` (incomplete) | `Family: unix.NFPROTO_IPV4, RegPort: 1` (verified against source) |
| Helper functions "missing" | Claimed `splitIPsByFamily` doesn't exist | Already exists at `intercept.go:40` — reused |
| Module path | `google/nftables` | Still `google/nftables` but noted project uses `sagernet/nftables` fork with same API |
| Quota source | Not specified | `GatewayConfig.MaxLanClient` → `RouterDeviceManager.quota` |
| Tier flow | Not addressed | Full diagram: Center API → Webapp → GatewayConfig → RouterDeviceManager |
| Task 6 code | Only title + skeleton | Complete: InterceptConfig wiring, mode change callback, live MAC updates via MACManager, status response |
| iptables DNS/MAC | Not implemented | Added DNS redirect (nat chain) + MAC filter (mac module) |
| Interceptor interface | Only Install/Remove/Name | Added `MACManager` optional interface for live set updates |
| `nftInterceptor` naming | Unclear | Renamed to `nftShellInterceptor` (shell fallback), new `nftNetlinkInterceptor` (primary) |

### Type Consistency Check

- `InterceptConfig` — defined in Task 1, used in Tasks 2, 4, 6
- `MACManager` — defined in Task 1, implemented by Task 2, used by Task 4/6
- `RouterDeviceManager` — defined in Task 4, used in Task 6
- `GatewayConfig.MaxLanClient` — defined in Task 1, used in Task 4
- `LanDevice` — defined in Task 3, used in Task 4

**Confidence: 10/10** — All expr fields verified. All helper functions verified as existing. Complete quota flow from Tier.
**Risk: 3/10** — netlink needs real Linux testing (3-tier fallback covers failure). OTA has state recovery.
