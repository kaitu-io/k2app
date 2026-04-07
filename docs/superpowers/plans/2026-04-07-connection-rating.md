# Connection Rating Statistics Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Record good/bad connection ratings with user network environment data, hide auto-generated tickets from users, and provide manager dashboard statistics for connection quality monitoring.

**Architecture:** Client-side network probing (gateway HTTP + ipinfo.io) triggered on network change, cached in engine memory. Rating submitted on disconnect with connection info + network env. Server stores in new `connection_ratings` table. Admin dashboard shows trends by server/ISP/router/platform/user.

**Tech Stack:** Go (engine layer), React + MUI (webapp), Next.js + shadcn/ui (manager dashboard), Go + Gin + GORM (Center API)

**Spec:** `docs/superpowers/specs/2026-04-07-connection-rating-design.md`

---

### Task 1: Engine — NetworkEnvironment struct and probe logic

**Files:**
- Create: `k2/engine/network_env.go`
- Create: `k2/engine/network_env_test.go`

This task creates the NetworkEnvironment cache, gateway HTTP title probing, and ipinfo.io client. All probing is best-effort with short timeouts.

- [ ] **Step 1: Write test for router brand extraction**

```go
// k2/engine/network_env_test.go
package engine

import "testing"

func TestExtractRouterBrand(t *testing.T) {
	tests := []struct {
		html  string
		brand string
		model string
	}{
		{`<html><head><title>TL-WR886N</title></head></html>`, "TP-LINK", "TL-WR886N"},
		{`<html><head><title>小米路由器</title></head></html>`, "Xiaomi", ""},
		{`<html><head><title>ASUS Login</title></head></html>`, "ASUS", ""},
		{`<html><head><title>LuCI - OpenWrt</title></head></html>`, "OpenWrt", ""},
		{`<html><head><title>HUAWEI HG8245H</title></head></html>`, "Huawei", "HG8245H"},
		{`<html><head><title>NETGEAR R7000</title></head></html>`, "NETGEAR", "R7000"},
		{`<html><head><title>Tenda AC1200</title></head></html>`, "Tenda", "AC1200"},
		{`<html><head><title>MERCURY MW325R</title></head></html>`, "Mercury", "MW325R"},
		{`<html><head><title>Welcome</title></head></html>`, "", ""},
		{`no html here`, "", ""},
		{``, "", ""},
	}
	for _, tt := range tests {
		brand, model := extractRouterBrand(tt.html)
		if brand != tt.brand || model != tt.model {
			t.Errorf("extractRouterBrand(%q) = (%q, %q), want (%q, %q)",
				tt.html, brand, model, tt.brand, tt.model)
		}
	}
}

func TestParseIPInfo(t *testing.T) {
	body := `{"ip":"223.5.5.5","city":"Shanghai","region":"Shanghai","country":"CN","org":"AS4812 China Telecom"}`
	env := parseIPInfoResponse([]byte(body))
	if env.PublicIP != "223.5.5.5" {
		t.Errorf("PublicIP = %q, want %q", env.PublicIP, "223.5.5.5")
	}
	if env.ISP != "AS4812 China Telecom" {
		t.Errorf("ISP = %q, want %q", env.ISP, "AS4812 China Telecom")
	}
	if env.City != "Shanghai" {
		t.Errorf("City = %q, want %q", env.City, "Shanghai")
	}
	if env.Country != "CN" {
		t.Errorf("Country = %q, want %q", env.Country, "CN")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd k2 && go test -run "TestExtractRouterBrand|TestParseIPInfo" ./engine/ -v`
Expected: FAIL — functions not defined

- [ ] **Step 3: Implement NetworkEnvironment and probe logic**

```go
// k2/engine/network_env.go
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/sasha-s/go-deadlock"
)

// NetworkEnvironment holds cached network context for connection quality reporting.
type NetworkEnvironment struct {
	PublicIP     string `json:"publicIP,omitempty"`
	ISP          string `json:"isp,omitempty"`
	City         string `json:"city,omitempty"`
	Country      string `json:"country,omitempty"`
	RouterBrand  string `json:"routerBrand,omitempty"`
	RouterModel  string `json:"routerModel,omitempty"`
	GatewayIP    string `json:"gatewayIP,omitempty"`
	NetworkType  string `json:"networkType,omitempty"` // wifi, cellular, ethernet
	ProbeTime    int64  `json:"probeTime,omitempty"`   // unix seconds
}

// networkEnvCache holds the latest probe result.
type networkEnvCache struct {
	mu  deadlock.Mutex
	env *NetworkEnvironment
}

func (c *networkEnvCache) get() *NetworkEnvironment {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.env == nil {
		return nil
	}
	cp := *c.env
	return &cp
}

func (c *networkEnvCache) set(env *NetworkEnvironment) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.env = env
}

// probeNetworkEnv runs best-effort network environment probing.
// All steps are independent and failures are silently ignored.
func probeNetworkEnv(ctx context.Context, event *NetEvent) *NetworkEnvironment {
	env := &NetworkEnvironment{
		ProbeTime: time.Now().Unix(),
	}

	// Derive network type from event.
	if event != nil {
		switch {
		case event.IsWifi:
			env.NetworkType = "wifi"
		case event.IsCellular:
			env.NetworkType = "cellular"
		default:
			env.NetworkType = "ethernet"
		}
	}

	// Step 1: Get default gateway IP.
	gw := detectGateway()
	if gw != "" {
		env.GatewayIP = gw
	}

	// Step 2: Probe gateway HTTP for router brand (1.5s timeout).
	if env.GatewayIP != "" {
		gwCtx, gwCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		brand, model := probeGatewayHTTP(gwCtx, env.GatewayIP)
		gwCancel()
		env.RouterBrand = brand
		env.RouterModel = model
	}

	// Step 3: Query ipinfo.io for public IP + ISP + geo (2s timeout).
	ipCtx, ipCancel := context.WithTimeout(ctx, 2*time.Second)
	ipEnv := queryIPInfo(ipCtx)
	ipCancel()
	if ipEnv != nil {
		env.PublicIP = ipEnv.PublicIP
		env.ISP = ipEnv.ISP
		env.City = ipEnv.City
		env.Country = ipEnv.Country
	}

	return env
}

// probeGatewayHTTP fetches the gateway's HTTP page and extracts router brand/model.
func probeGatewayHTTP(ctx context.Context, gatewayIP string) (brand, model string) {
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("http://%s/", gatewayIP), nil)
	if err != nil {
		return "", ""
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", ""
	}
	defer resp.Body.Close()

	// Read first 4KB only.
	buf := make([]byte, 4096)
	n, _ := io.ReadAtLeast(resp.Body, buf, 1)
	if n == 0 {
		return "", ""
	}

	return extractRouterBrand(string(buf[:n]))
}

// titleRegex extracts <title>...</title> content.
var titleRegex = regexp.MustCompile(`(?i)<title[^>]*>(.*?)</title>`)

// brandSignatures maps patterns to brand names.
var brandSignatures = []struct {
	patterns []string
	brand    string
}{
	{[]string{"TL-", "TP-LINK", "tplink"}, "TP-LINK"},
	{[]string{"小米路由", "miwifi", "Xiaomi"}, "Xiaomi"},
	{[]string{"HUAWEI", "HiLink"}, "Huawei"},
	{[]string{"ASUS", "RT-"}, "ASUS"},
	{[]string{"LuCI", "OpenWrt"}, "OpenWrt"},
	{[]string{"NETGEAR"}, "NETGEAR"},
	{[]string{"Linksys"}, "Linksys"},
	{[]string{"D-Link"}, "D-Link"},
	{[]string{"MERCURY", "Mercury"}, "Mercury"},
	{[]string{"FAST", "迅捷"}, "FAST"},
	{[]string{"Tenda", "腾达"}, "Tenda"},
}

// modelRegex tries to extract model numbers like "TL-WR886N", "HG8245H", "R7000", "AC1200", "MW325R".
var modelRegex = regexp.MustCompile(`(?i)\b([A-Z]{1,3}[\-]?[A-Z0-9]{3,10})\b`)

func extractRouterBrand(html string) (brand, model string) {
	match := titleRegex.FindStringSubmatch(html)
	if len(match) < 2 {
		return "", ""
	}
	title := strings.TrimSpace(match[1])
	if title == "" {
		return "", ""
	}

	for _, sig := range brandSignatures {
		for _, pat := range sig.patterns {
			if strings.Contains(title, pat) {
				brand = sig.brand
				// Try to extract model from title.
				models := modelRegex.FindAllString(title, -1)
				for _, m := range models {
					upper := strings.ToUpper(m)
					// Skip the brand pattern itself (e.g., "ASUS", "NETGEAR").
					isBrand := false
					for _, p := range sig.patterns {
						if strings.EqualFold(m, p) {
							isBrand = true
							break
						}
					}
					if !isBrand && len(upper) >= 4 {
						model = m
						break
					}
				}
				return brand, model
			}
		}
	}
	return "", ""
}

// ipInfoResponse is the JSON response from ipinfo.io.
type ipInfoResponse struct {
	IP      string `json:"ip"`
	City    string `json:"city"`
	Region  string `json:"region"`
	Country string `json:"country"`
	Org     string `json:"org"`
}

func parseIPInfoResponse(data []byte) *NetworkEnvironment {
	var info ipInfoResponse
	if err := json.Unmarshal(data, &info); err != nil {
		return nil
	}
	return &NetworkEnvironment{
		PublicIP: info.IP,
		ISP:     info.Org,
		City:    info.City,
		Country: info.Country,
	}
}

func queryIPInfo(ctx context.Context) *NetworkEnvironment {
	client := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", "https://ipinfo.io/json", nil)
	if err != nil {
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		slog.Debug("network_env: ipinfo.io query failed", "err", err)
		return nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return nil
	}
	return parseIPInfoResponse(body)
}
```

- [ ] **Step 4: Create gateway detection stub (platform-specific files will follow)**

```go
// k2/engine/network_env_gateway.go
//go:build !darwin && !windows && !linux
// +build !darwin,!windows,!linux

package engine

func detectGateway() string {
	return ""
}
```

```go
// k2/engine/network_env_gateway_unix.go
//go:build darwin || linux
// +build darwin linux

package engine

import (
	"net"
	"os/exec"
	"strings"
)

func detectGateway() string {
	// Try: ip route show default (Linux) or route -n get default (macOS)
	out, err := exec.Command("ip", "route", "show", "default").Output()
	if err == nil {
		// "default via 192.168.1.1 dev en0"
		fields := strings.Fields(string(out))
		for i, f := range fields {
			if f == "via" && i+1 < len(fields) {
				gw := fields[i+1]
				if net.ParseIP(gw) != nil {
					return gw
				}
			}
		}
	}

	// macOS fallback
	out, err = exec.Command("route", "-n", "get", "default").Output()
	if err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "gateway:") {
				gw := strings.TrimSpace(strings.TrimPrefix(line, "gateway:"))
				if net.ParseIP(gw) != nil {
					return gw
				}
			}
		}
	}
	return ""
}
```

```go
// k2/engine/network_env_gateway_windows.go
//go:build windows

package engine

import (
	"net"
	"os/exec"
	"strings"
)

func detectGateway() string {
	out, err := exec.Command("cmd", "/c", "route", "print", "0.0.0.0").Output()
	if err != nil {
		return ""
	}
	// Parse "0.0.0.0  0.0.0.0  192.168.1.1  ..."
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 3 && fields[0] == "0.0.0.0" && fields[1] == "0.0.0.0" {
			gw := fields[2]
			if net.ParseIP(gw) != nil {
				return gw
			}
		}
	}
	return ""
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd k2 && go test -run "TestExtractRouterBrand|TestParseIPInfo" ./engine/ -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
cd k2 && git add engine/network_env.go engine/network_env_test.go engine/network_env_gateway.go engine/network_env_gateway_unix.go engine/network_env_gateway_windows.go
git commit -m "feat(engine): add NetworkEnvironment struct and probe logic

Gateway HTTP title parsing, ipinfo.io client, and platform-specific
gateway detection for connection quality reporting."
```

---

### Task 2: Engine — Wire NetworkEnvironment into Engine and netCoordinator

**Files:**
- Modify: `k2/engine/engine.go` — add `envCache` field, `NetworkEnv()` method, trigger probe on network change
- Modify: `k2/engine/netmon.go` — pass event to probe callback
- Create: `k2/engine/network_env_integration_test.go`

- [ ] **Step 1: Write test for Engine.NetworkEnv()**

```go
// k2/engine/network_env_integration_test.go
package engine

import "testing"

func TestEngineNetworkEnv_NilBeforeStart(t *testing.T) {
	e := New()
	env := e.NetworkEnv()
	if env != nil {
		t.Errorf("NetworkEnv() before start should be nil, got %+v", env)
	}
}

func TestNetworkEnvCache_SetGet(t *testing.T) {
	cache := &networkEnvCache{}

	// Initially nil.
	if got := cache.get(); got != nil {
		t.Errorf("expected nil, got %+v", got)
	}

	// Set and get.
	env := &NetworkEnvironment{PublicIP: "1.2.3.4", ISP: "Test ISP"}
	cache.set(env)
	got := cache.get()
	if got == nil {
		t.Fatal("expected non-nil after set")
	}
	if got.PublicIP != "1.2.3.4" || got.ISP != "Test ISP" {
		t.Errorf("got %+v, want PublicIP=1.2.3.4, ISP=Test ISP", got)
	}

	// Returned value is a copy — mutating doesn't affect cache.
	got.PublicIP = "changed"
	got2 := cache.get()
	if got2.PublicIP != "1.2.3.4" {
		t.Errorf("cache was mutated: got %q, want %q", got2.PublicIP, "1.2.3.4")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd k2 && go test -run "TestEngineNetworkEnv_NilBeforeStart|TestNetworkEnvCache_SetGet" ./engine/ -v`
Expected: FAIL — `NetworkEnv()` not defined

- [ ] **Step 3: Add envCache to Engine and NetworkEnv() method**

Add to `engine.go` Engine struct fields (after `ruleEngine *rule.Engine`):

```go
envCache *networkEnvCache // cached network environment from last probe
```

Add method to `engine.go` (after `StatusJSON()` method):

```go
// NetworkEnv returns the cached network environment from the last probe.
// Returns nil if no probe has completed yet.
func (e *Engine) NetworkEnv() *NetworkEnvironment {
	e.mu.Lock()
	cache := e.envCache
	e.mu.Unlock()
	if cache == nil {
		return nil
	}
	return cache.get()
}

// NetworkEnvJSON returns the cached network environment as a JSON string.
// Returns "{}" if no probe has completed.
func (e *Engine) NetworkEnvJSON() string {
	env := e.NetworkEnv()
	if env == nil {
		return "{}"
	}
	b, _ := json.Marshal(env)
	return string(b)
}
```

Initialize `envCache` in `Start()` before netCoordinator creation (around line 415):

```go
// Initialize network environment cache.
e.envCache = &networkEnvCache{}
```

- [ ] **Step 4: Modify netCoordinator to trigger network env probe**

In `engine.go`, where `netCoord` is created (around line 416), change the `onReconnect` callback to also trigger a probe:

```go
e.netCoord = newNetCoordinator(
	func() { e.doNetworkReconnect() },
	func() {},
)
```

Add a new method to Engine that triggers the probe asynchronously. Add after `NetworkEnvJSON()`:

```go
// probeNetworkEnvAsync runs network environment probing in a background goroutine.
func (e *Engine) probeNetworkEnvAsync(event *NetEvent) {
	e.mu.Lock()
	cache := e.envCache
	e.mu.Unlock()
	if cache == nil {
		return
	}
	safego.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		env := probeNetworkEnv(ctx, event)
		cache.set(env)
		slog.Info("network_env: probe complete",
			"publicIP", env.PublicIP,
			"isp", env.ISP,
			"routerBrand", env.RouterBrand,
			"networkType", env.NetworkType,
		)
	})
}
```

Modify `netCoordinator.handleEvent` in `netmon.go` to accept an `onProbe` callback. Instead, it's simpler to hook into the existing reconnect flow in `engine.go`. In `doNetworkReconnect()`, add the probe trigger:

Find `doNetworkReconnect` in engine.go and add the probe call at the end (after the reconnect logic):

```go
// Trigger network environment probe on network change.
e.probeNetworkEnvAsync(nil)
```

Also add an initial probe in `Start()` right after `envCache` initialization:

```go
// Initial network environment probe.
e.probeNetworkEnvAsync(nil)
```

- [ ] **Step 5: Run tests**

Run: `cd k2 && go test -run "TestEngineNetworkEnv_NilBeforeStart|TestNetworkEnvCache_SetGet" ./engine/ -v`
Expected: PASS

- [ ] **Step 6: Run all engine tests to check for regressions**

Run: `cd k2 && go test -short ./engine/ -v -timeout 60s`
Expected: All existing tests pass

- [ ] **Step 7: Commit**

```bash
cd k2 && git add engine/engine.go engine/network_env_integration_test.go
git commit -m "feat(engine): wire NetworkEnvironment cache into Engine

Triggers async probe on Start() and network change events.
Exposes NetworkEnv() and NetworkEnvJSON() for consumers."
```

---

### Task 3: Appext — Export NetworkEnvJSON for gomobile

**Files:**
- Modify: `k2/appext/appext.go` — add `NetworkEnvJSON()` export

- [ ] **Step 1: Add NetworkEnvJSON to appext Engine**

Add to `appext/appext.go` (after `StatusJSON()` method):

```go
// NetworkEnvJSON returns the cached network environment as a JSON string.
// Returns "{}" if engine is not started or no probe has completed.
func (e *Engine) NetworkEnvJSON() (result string) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("appext: NetworkEnvJSON panic", "err", r)
			result = "{}"
		}
	}()
	return e.inner.NetworkEnvJSON()
}
```

- [ ] **Step 2: Verify build**

Run: `cd k2 && go build ./appext/`
Expected: Build succeeds

- [ ] **Step 3: Commit**

```bash
cd k2 && git add appext/appext.go
git commit -m "feat(appext): export NetworkEnvJSON for gomobile"
```

---

### Task 4: Daemon — Expose network env via status response

**Files:**
- Modify: `k2/daemon/api.go` — include network env in status response

- [ ] **Step 1: Add network env to handleStatus response**

In `daemon/api.go`, find `handleStatus` and modify it to include network env data alongside the existing status. The status response `Data` field is `any`, so we can extend it:

```go
func (d *Daemon) handleStatus(w http.ResponseWriter) {
	d.mu.RLock()
	status := d.lastStatus
	eng := d.eng
	d.mu.RUnlock()

	data := map[string]any{
		"state": status.State,
	}
	if status.Error != nil {
		data["error"] = status.Error
	}
	if status.State == engine.StateConnected && !status.ConnectedAt.IsZero() {
		data["connected_at"] = status.ConnectedAt.Format(time.RFC3339)
		data["uptime_seconds"] = int(time.Since(status.ConnectedAt).Seconds())
	}
	// Include network environment if available.
	if eng != nil {
		if env := eng.NetworkEnv(); env != nil {
			data["network_env"] = env
		}
	}

	writeJSON(w, Response{Code: 0, Message: "ok", Data: data})
}
```

The existing `statusInfo()` is in `daemon.go:423` and returns `map[string]any`. The field is `d.engine` (not `d.eng`). Modify `statusInfo()` by adding after the `if cfg != nil` block:

```go
// Include network environment if available.
eng := d.engine  // already read under d.mu.RLock above — need to restructure
```

Actually, `statusInfo()` reads `d.lastStatus` and `d.lastConfig` under RLock but not `d.engine`. We need to also read `d.engine` under the same RLock. Modify the function:

```go
func (d *Daemon) statusInfo() map[string]any {
	d.mu.RLock()
	s := d.lastStatus
	cfg := d.lastConfig
	eng := d.engine
	d.mu.RUnlock()

	info := map[string]any{
		"state": s.State,
	}
	if s.Error != nil {
		info["error"] = s.Error
	}
	if s.State == engine.StateConnected && !s.ConnectedAt.IsZero() {
		info["connected_at"] = s.ConnectedAt.Format(time.RFC3339)
		info["uptime_seconds"] = int(time.Since(s.ConnectedAt).Seconds())
	}
	if cfg != nil {
		info["config"] = cfg
	}
	if eng != nil {
		if env := eng.NetworkEnv(); env != nil {
			info["network_env"] = env
		}
	}
	return info
}
```

- [ ] **Step 2: Verify build**

Run: `cd k2 && go build ./daemon/`
Expected: Build succeeds

- [ ] **Step 3: Commit**

```bash
cd k2 && git add daemon/daemon.go
git commit -m "feat(daemon): include network_env in status response"
```

---

### Task 5: Center API — ConnectionRating model and migration

**Files:**
- Modify: `api/model.go` — add `ConnectionRating` model, add `AutoGenerated` to `FeedbackTicket`
- Modify: `api/migrate.go` — add `ConnectionRating` to AutoMigrate
- Modify: `api/type.go` — add request/response types

- [ ] **Step 1: Add ConnectionRating model to model.go**

Append after the `TicketReply` struct (around line 1039):

```go
// ConnectionRating stores user-submitted connection quality rating.
type ConnectionRating struct {
	ID            uint64    `gorm:"primarykey" json:"id"`
	CreatedAt     time.Time `gorm:"index" json:"createdAt"`
	UserID        uint64    `gorm:"index;not null" json:"userId"`
	Rating        string    `gorm:"type:varchar(8);not null;index" json:"rating"` // good / bad
	FeedbackID    string    `gorm:"type:varchar(36);not null" json:"feedbackId"`
	ServerDomain  string    `gorm:"type:varchar(128);not null;default:''" json:"serverDomain"`
	ServerName    string    `gorm:"type:varchar(64);not null;default:''" json:"serverName"`
	ServerCountry string    `gorm:"type:varchar(8);not null;default:''" json:"serverCountry"`
	ServerSource  string    `gorm:"type:varchar(16);not null;default:''" json:"serverSource"`
	DurationSec   int       `gorm:"not null;default:0" json:"durationSec"`
	RuleMode      string    `gorm:"type:varchar(16);not null;default:''" json:"ruleMode"`
	OS            string    `gorm:"type:varchar(32);not null;default:''" json:"os"`
	AppVersion    string    `gorm:"type:varchar(32);not null;default:''" json:"appVersion"`
	PublicIP      string    `gorm:"type:varchar(45);not null;default:''" json:"publicIP"`
	ISP           string    `gorm:"type:varchar(128);not null;default:''" json:"isp"`
	UserCity      string    `gorm:"type:varchar(64);not null;default:''" json:"userCity"`
	UserCountry   string    `gorm:"type:varchar(8);not null;default:''" json:"userCountry"`
	RouterBrand   string    `gorm:"type:varchar(64);not null;default:''" json:"routerBrand"`
	RouterModel   string    `gorm:"type:varchar(128);not null;default:''" json:"routerModel"`
	GatewayIP     string    `gorm:"type:varchar(45);not null;default:''" json:"gatewayIP"`
	NetworkType   string    `gorm:"type:varchar(16);not null;default:''" json:"networkType"`
}
```

- [ ] **Step 2: Add AutoGenerated to FeedbackTicket**

In `model.go`, add field to `FeedbackTicket` struct (after `UserUnread` field):

```go
AutoGenerated bool `gorm:"not null;default:false" json:"autoGenerated"`
```

- [ ] **Step 3: Add to AutoMigrate in migrate.go**

In `migrate.go`, add `&ConnectionRating{}` after `&TicketReply{}`:

```go
&TicketReply{},
// Connection quality ratings
&ConnectionRating{},
```

- [ ] **Step 4: Add request/response types to type.go**

Append to `type.go`:

```go
// CreateConnectionRatingRequest — user submits connection quality rating.
type CreateConnectionRatingRequest struct {
	Rating     string `json:"rating" binding:"required,oneof=good bad"`
	FeedbackID string `json:"feedbackId" binding:"required"`
	Server     struct {
		Domain  string `json:"domain"`
		Name    string `json:"name"`
		Country string `json:"country"`
		Source  string `json:"source"`
	} `json:"server"`
	Connection struct {
		DurationSec int    `json:"durationSec"`
		RuleMode    string `json:"ruleMode"`
		OS          string `json:"os"`
		AppVersion  string `json:"appVersion"`
	} `json:"connection"`
	Network struct {
		PublicIP    string `json:"publicIP"`
		ISP         string `json:"isp"`
		City        string `json:"city"`
		Country     string `json:"country"`
		RouterBrand string `json:"routerBrand"`
		RouterModel string `json:"routerModel"`
		GatewayIP   string `json:"gatewayIP"`
		NetworkType string `json:"networkType"`
	} `json:"network"`
}

// ConnectionRatingStatisticsResponse — admin dashboard statistics.
type ConnectionRatingStatisticsResponse struct {
	Summary    RatingSummary          `json:"summary"`
	Trend      []RatingTrendItem      `json:"trend"`
	ByServer   []RatingByServer       `json:"byServer"`
	ByISP      []RatingByISP          `json:"byISP"`
	ByRouter   []RatingByRouter       `json:"byRouter"`
	ByPlatform []RatingByPlatform     `json:"byPlatform"`
	ByUser     []RatingByUser         `json:"byUser"`
}

type RatingSummary struct {
	Total    int64   `json:"total"`
	Good     int64   `json:"good"`
	Bad      int64   `json:"bad"`
	GoodRate float64 `json:"goodRate"`
}

type RatingTrendItem struct {
	Date     string  `json:"date"`
	Total    int64   `json:"total"`
	Good     int64   `json:"good"`
	Bad      int64   `json:"bad"`
	GoodRate float64 `json:"goodRate"`
}

type RatingByServer struct {
	Domain   string  `json:"domain"`
	Name     string  `json:"name"`
	Country  string  `json:"country"`
	Total    int64   `json:"total"`
	Good     int64   `json:"good"`
	Bad      int64   `json:"bad"`
	GoodRate float64 `json:"goodRate"`
}

type RatingByISP struct {
	ISP      string  `json:"isp"`
	Country  string  `json:"country"`
	Total    int64   `json:"total"`
	Good     int64   `json:"good"`
	GoodRate float64 `json:"goodRate"`
}

type RatingByRouter struct {
	Brand    string  `json:"brand"`
	Total    int64   `json:"total"`
	Good     int64   `json:"good"`
	GoodRate float64 `json:"goodRate"`
}

type RatingByPlatform struct {
	OS         string  `json:"os"`
	AppVersion string  `json:"appVersion"`
	Total      int64   `json:"total"`
	Good       int64   `json:"good"`
	GoodRate   float64 `json:"goodRate"`
}

type RatingByUser struct {
	UserID   uint64  `json:"userId"`
	Email    string  `json:"email"`
	Total    int64   `json:"total"`
	Good     int64   `json:"good"`
	Bad      int64   `json:"bad"`
	GoodRate float64 `json:"goodRate"`
}
```

- [ ] **Step 5: Verify build**

Run: `cd api && go build ./...`
Expected: Build succeeds

- [ ] **Step 6: Commit**

```bash
cd api && git add model.go migrate.go type.go
git commit -m "feat: add ConnectionRating model, AutoGenerated field on FeedbackTicket"
```

---

### Task 6: Center API — Rating submission endpoint

**Files:**
- Create: `api/api_connection_rating.go`
- Modify: `api/route.go` — register endpoint

- [ ] **Step 1: Implement POST /api/user/connection-rating handler**

```go
// api/api_connection_rating.go
package center

import (
	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// api_create_connection_rating saves a user's connection quality rating.
// POST /api/user/connection-rating
func api_create_connection_rating(c *gin.Context) {
	userID := ReqUserID(c)
	if userID == 0 {
		Error(c, ErrorNotLogin, "authentication required")
		return
	}

	var req CreateConnectionRatingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "api_create_connection_rating: invalid request: %v", err)
		Error(c, ErrorInvalidArgument, "invalid request")
		return
	}

	rating := ConnectionRating{
		UserID:        userID,
		Rating:        req.Rating,
		FeedbackID:    req.FeedbackID,
		ServerDomain:  req.Server.Domain,
		ServerName:    req.Server.Name,
		ServerCountry: req.Server.Country,
		ServerSource:  req.Server.Source,
		DurationSec:   req.Connection.DurationSec,
		RuleMode:      req.Connection.RuleMode,
		OS:            req.Connection.OS,
		AppVersion:    req.Connection.AppVersion,
		PublicIP:      req.Network.PublicIP,
		ISP:           req.Network.ISP,
		UserCity:      req.Network.City,
		UserCountry:   req.Network.Country,
		RouterBrand:   req.Network.RouterBrand,
		RouterModel:   req.Network.RouterModel,
		GatewayIP:     req.Network.GatewayIP,
		NetworkType:   req.Network.NetworkType,
	}

	if err := db.Get().Create(&rating).Error; err != nil {
		log.Errorf(c, "api_create_connection_rating: failed to save: %v", err)
		Error(c, ErrorSystemError, "failed to save rating")
		return
	}

	log.Infof(c, "api_create_connection_rating: user %d rated %s, server=%s",
		userID, req.Rating, req.Server.Domain)
	SuccessEmpty(c)
}
```

- [ ] **Step 2: Register route in route.go**

In `route.go`, find the `/api/user` group and add:

```go
user.POST("/connection-rating", AuthRequired(), api_create_connection_rating)
```

- [ ] **Step 3: Verify build**

Run: `cd api && go build ./...`
Expected: Build succeeds

- [ ] **Step 4: Commit**

```bash
cd api && git add api_connection_rating.go route.go
git commit -m "feat: add POST /api/user/connection-rating endpoint"
```

---

### Task 7: Center API — Auto-ticket hiding (filter user ticket list)

**Files:**
- Modify: `api/api_ticket_reply.go` — filter `auto_generated` in user list and unread
- Modify: `api/api_ticket.go` — accept and store `auto_generated` field

- [ ] **Step 1: Update api_user_list_tickets to filter auto-generated**

In `api_ticket_reply.go`, modify the query in `api_user_list_tickets` (line 23):

Change:
```go
query := db.Get().Model(&FeedbackTicket{}).Where("user_id = ?", userID)
```
To:
```go
query := db.Get().Model(&FeedbackTicket{}).Where("user_id = ? AND auto_generated = ?", userID, false)
```

- [ ] **Step 2: Update api_user_tickets_unread to filter auto-generated**

In `api_ticket_reply.go`, modify `api_user_tickets_unread` (around line 222):

Change:
```go
db.Get().Model(&FeedbackTicket{}).Where("user_id = ?", userID).
	Select("COALESCE(SUM(user_unread), 0)").Scan(&count)
```
To:
```go
db.Get().Model(&FeedbackTicket{}).Where("user_id = ? AND auto_generated = ?", userID, false).
	Select("COALESCE(SUM(user_unread), 0)").Scan(&count)
```

- [ ] **Step 3: Update CreateTicketRequest to accept auto_generated**

In `type.go`, add to `CreateTicketRequest`:

```go
AutoGenerated bool `json:"auto_generated,omitempty"`
```

- [ ] **Step 4: Store auto_generated in api_create_ticket**

In `api_ticket.go`, find where `FeedbackTicket` is created (around line 109) and add:

```go
AutoGenerated: req.AutoGenerated,
```

- [ ] **Step 5: Verify build**

Run: `cd api && go build ./...`
Expected: Build succeeds

- [ ] **Step 6: Commit**

```bash
cd api && git add api_ticket_reply.go api_ticket.go type.go
git commit -m "feat: hide auto-generated tickets from user ticket list and unread count"
```

---

### Task 8: Center API — Admin statistics endpoint

**Files:**
- Create: `api/api_admin_connection_rating.go`
- Modify: `api/route.go` — register admin endpoint

- [ ] **Step 1: Implement GET /app/connection-ratings/statistics**

```go
// api/api_admin_connection_rating.go
package center

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// api_admin_connection_rating_statistics returns connection quality stats.
// GET /app/connection-ratings/statistics?period=7d|30d|90d
func api_admin_connection_rating_statistics(c *gin.Context) {
	period := c.DefaultQuery("period", "7d")
	var days int
	switch period {
	case "7d":
		days = 7
	case "30d":
		days = 30
	case "90d":
		days = 90
	default:
		Error(c, ErrorInvalidArgument, "period must be 7d, 30d, or 90d")
		return
	}

	since := time.Now().AddDate(0, 0, -days)
	d := db.Get()

	var result ConnectionRatingStatisticsResponse

	// Summary
	d.Model(&ConnectionRating{}).Where("created_at >= ?", since).Count(&result.Summary.Total)
	d.Model(&ConnectionRating{}).Where("created_at >= ? AND rating = ?", since, "good").Count(&result.Summary.Good)
	result.Summary.Bad = result.Summary.Total - result.Summary.Good
	if result.Summary.Total > 0 {
		result.Summary.GoodRate = float64(result.Summary.Good) / float64(result.Summary.Total)
	}

	// Trend (daily)
	type trendRow struct {
		Date  string
		Total int64
		Good  int64
	}
	var trendRows []trendRow
	d.Model(&ConnectionRating{}).
		Select("DATE(created_at) as date, COUNT(*) as total, SUM(CASE WHEN rating = 'good' THEN 1 ELSE 0 END) as good").
		Where("created_at >= ?", since).
		Group("DATE(created_at)").
		Order("date ASC").
		Find(&trendRows)

	result.Trend = make([]RatingTrendItem, len(trendRows))
	for i, r := range trendRows {
		bad := r.Total - r.Good
		var goodRate float64
		if r.Total > 0 {
			goodRate = float64(r.Good) / float64(r.Total)
		}
		result.Trend[i] = RatingTrendItem{
			Date: r.Date, Total: r.Total, Good: r.Good, Bad: bad, GoodRate: goodRate,
		}
	}

	// By server
	type serverRow struct {
		Domain  string
		Name    string
		Country string
		Total   int64
		Good    int64
	}
	var serverRows []serverRow
	d.Model(&ConnectionRating{}).
		Select("server_domain as domain, MAX(server_name) as name, MAX(server_country) as country, COUNT(*) as total, SUM(CASE WHEN rating = 'good' THEN 1 ELSE 0 END) as good").
		Where("created_at >= ? AND server_domain != ''", since).
		Group("server_domain").
		Order("total DESC").
		Limit(50).
		Find(&serverRows)

	result.ByServer = make([]RatingByServer, len(serverRows))
	for i, r := range serverRows {
		var goodRate float64
		if r.Total > 0 {
			goodRate = float64(r.Good) / float64(r.Total)
		}
		result.ByServer[i] = RatingByServer{
			Domain: r.Domain, Name: r.Name, Country: r.Country,
			Total: r.Total, Good: r.Good, Bad: r.Total - r.Good, GoodRate: goodRate,
		}
	}

	// By ISP
	type ispRow struct {
		ISP     string
		Country string
		Total   int64
		Good    int64
	}
	var ispRows []ispRow
	d.Model(&ConnectionRating{}).
		Select("isp, MAX(user_country) as country, COUNT(*) as total, SUM(CASE WHEN rating = 'good' THEN 1 ELSE 0 END) as good").
		Where("created_at >= ? AND isp != ''", since).
		Group("isp").
		Order("total DESC").
		Limit(50).
		Find(&ispRows)

	result.ByISP = make([]RatingByISP, len(ispRows))
	for i, r := range ispRows {
		var goodRate float64
		if r.Total > 0 {
			goodRate = float64(r.Good) / float64(r.Total)
		}
		result.ByISP[i] = RatingByISP{
			ISP: r.ISP, Country: r.Country, Total: r.Total, Good: r.Good, GoodRate: goodRate,
		}
	}

	// By router brand
	type routerRow struct {
		Brand string
		Total int64
		Good  int64
	}
	var routerRows []routerRow
	d.Model(&ConnectionRating{}).
		Select("COALESCE(NULLIF(router_brand, ''), 'Unknown') as brand, COUNT(*) as total, SUM(CASE WHEN rating = 'good' THEN 1 ELSE 0 END) as good").
		Where("created_at >= ?", since).
		Group("COALESCE(NULLIF(router_brand, ''), 'Unknown')").
		Order("total DESC").
		Limit(30).
		Find(&routerRows)

	result.ByRouter = make([]RatingByRouter, len(routerRows))
	for i, r := range routerRows {
		var goodRate float64
		if r.Total > 0 {
			goodRate = float64(r.Good) / float64(r.Total)
		}
		result.ByRouter[i] = RatingByRouter{
			Brand: r.Brand, Total: r.Total, Good: r.Good, GoodRate: goodRate,
		}
	}

	// By platform (os + app_version)
	type platformRow struct {
		OS         string
		AppVersion string
		Total      int64
		Good       int64
	}
	var platformRows []platformRow
	d.Model(&ConnectionRating{}).
		Select("os, app_version, COUNT(*) as total, SUM(CASE WHEN rating = 'good' THEN 1 ELSE 0 END) as good").
		Where("created_at >= ? AND os != ''", since).
		Group("os, app_version").
		Order("total DESC").
		Limit(30).
		Find(&platformRows)

	result.ByPlatform = make([]RatingByPlatform, len(platformRows))
	for i, r := range platformRows {
		var goodRate float64
		if r.Total > 0 {
			goodRate = float64(r.Good) / float64(r.Total)
		}
		result.ByPlatform[i] = RatingByPlatform{
			OS: r.OS, AppVersion: r.AppVersion, Total: r.Total, Good: r.Good, GoodRate: goodRate,
		}
	}

	// By user (top 50 worst good rate, minimum 3 ratings)
	type userRow struct {
		UserID uint64
		Total  int64
		Good   int64
	}
	var userRows []userRow
	d.Model(&ConnectionRating{}).
		Select("user_id, COUNT(*) as total, SUM(CASE WHEN rating = 'good' THEN 1 ELSE 0 END) as good").
		Where("created_at >= ?", since).
		Group("user_id").
		Having("COUNT(*) >= 3").
		Order("(SUM(CASE WHEN rating = 'good' THEN 1 ELSE 0 END) * 1.0 / COUNT(*)) ASC, COUNT(*) DESC").
		Limit(50).
		Find(&userRows)

	result.ByUser = make([]RatingByUser, len(userRows))
	for i, r := range userRows {
		var goodRate float64
		if r.Total > 0 {
			goodRate = float64(r.Good) / float64(r.Total)
		}
		// Lookup user email.
		email := fmt.Sprintf("user#%d", r.UserID)
		if identify, err := GetEmailIdentifyByUserID(c, int64(r.UserID)); err == nil && identify != nil {
			if dec, err := secretDecryptString(c, identify.EncryptedValue); err == nil {
				email = dec
			}
		}
		result.ByUser[i] = RatingByUser{
			UserID: r.UserID, Email: email,
			Total: r.Total, Good: r.Good, Bad: r.Total - r.Good, GoodRate: goodRate,
		}
	}

	log.Infof(c, "api_admin_connection_rating_statistics: period=%s total=%d goodRate=%.2f",
		period, result.Summary.Total, result.Summary.GoodRate)
	Success(c, &result)
}
```

- [ ] **Step 2: Register admin route**

In `route.go`, add to the `admin` group (the one using `AdminRequired()`):

```go
admin.GET("/connection-ratings/statistics", api_admin_connection_rating_statistics)
```

- [ ] **Step 3: Verify build**

Run: `cd api && go build ./...`
Expected: Build succeeds

- [ ] **Step 4: Commit**

```bash
cd api && git add api_admin_connection_rating.go route.go
git commit -m "feat: add GET /app/connection-ratings/statistics admin endpoint"
```

---

### Task 9: Webapp — Update DisconnectFeedbackDialog

**Files:**
- Modify: `webapp/src/components/DisconnectFeedbackDialog.tsx`
- Modify: `webapp/src/components/__tests__/DisconnectFeedbackDialog.test.tsx`

- [ ] **Step 1: Update DisconnectFeedbackDialog to submit ratings**

Replace `submitNegativeFeedback` with two functions and update handlers:

```typescript
// Replace the existing submitNegativeFeedback function and add submitRating.
// Keep generateFeedbackId() and formatConnectionInfo() as-is.

async function getNetworkEnv(): Promise<Record<string, string>> {
  try {
    const result = await window._k2.run<Record<string, string>>('network-env');
    return result.data ?? {};
  } catch {
    return {};
  }
}

async function submitRating(
  rating: 'good' | 'bad',
  info: LastConnectionInfo,
  feedbackId: string,
): Promise<void> {
  const networkEnv = await getNetworkEnv();
  try {
    await cloudApi.post('/api/user/connection-rating', {
      rating,
      feedbackId,
      server: {
        domain: info.domain,
        name: info.name,
        country: info.country,
        source: info.source,
      },
      connection: {
        durationSec: info.durationSec,
        ruleMode: info.ruleMode,
        os: info.os,
        appVersion: info.appVersion,
      },
      network: networkEnv,
    });
  } catch (err) {
    console.warn('[DisconnectFeedback] rating submission failed:', err);
  }
}

async function submitNegativeFeedback(info: LastConnectionInfo): Promise<void> {
  const feedbackId = generateFeedbackId();
  let s3Keys: Array<{ name: string; s3Key: string }> = [];

  // Step 1: Upload logs (best-effort)
  if (window._platform?.uploadLogs) {
    try {
      const result = await window._platform.uploadLogs({
        email: null,
        reason: 'disconnect_feedback_bad',
        platform: window._platform.os,
        version: window._platform.version,
        feedbackId,
      });
      if (result.success && result.s3Keys?.length) {
        s3Keys = result.s3Keys;
      }
    } catch (err) {
      console.warn('[DisconnectFeedback] uploadLogs failed:', err);
    }
  }

  // Step 2: Submit ticket with auto_generated flag
  try {
    await cloudApi.post('/api/user/ticket', {
      content: `[Auto] User reported bad connection experience after disconnect.\n\n${formatConnectionInfo(info)}`,
      feedbackId,
      os: info.os,
      app_version: info.appVersion,
      auto_generated: true,
    });
  } catch (err) {
    console.warn('[DisconnectFeedback] ticket submission failed:', err);
  }

  // Step 3: Register log metadata
  if (s3Keys.length > 0) {
    try {
      const udid = await getDeviceUdid();
      await cloudApi.post('/api/user/device-log', {
        udid,
        feedbackId,
        s3Keys,
        reason: 'disconnect_feedback_bad',
        meta: {
          os: info.os,
          appVersion: info.appVersion,
          channel: window._platform?.updater?.channel ?? 'stable',
        },
      });
    } catch (err) {
      console.warn('[DisconnectFeedback] device-log registration failed:', err);
    }
  }

  // Step 4: Slack notification
  try {
    await cloudApi.post('/api/user/feedback-notify', {
      reason: 'disconnect_feedback_bad',
      platform: info.os,
      version: info.appVersion,
      feedbackId,
      s3Keys,
    });
  } catch (err) {
    console.warn('[DisconnectFeedback] feedback-notify failed:', err);
  }

  // Step 5: Submit rating
  await submitRating('bad', info, feedbackId);
}
```

Update `handleGood` callback:

```typescript
const handleGood = useCallback(() => {
  setOpen(false);
  const info = connectionInfoRef.current;
  connectionInfoRef.current = null;

  if (info) {
    const feedbackId = generateFeedbackId();
    submitRating('good', info, feedbackId).catch((err) => {
      console.error('[DisconnectFeedback] good rating error:', err);
    });
  }
}, []);
```

- [ ] **Step 2: Update tests**

In `DisconnectFeedbackDialog.test.tsx`, add `_k2` mock to `beforeEach`:

```typescript
(window as any)._k2 = {
  run: vi.fn().mockResolvedValue({
    code: 0,
    data: { publicIP: '1.2.3.4', isp: 'Test ISP', routerBrand: 'TP-LINK' },
  }),
};
```

Add to `afterEach`:

```typescript
delete (window as any)._k2;
```

**Change the "good" test** — it now submits a rating:

```typescript
it('点击"好"关闭 dialog 并提交 good rating', async () => {
  mockStoreState.pendingFeedback = true;
  mockStoreState.lastConnectionInfo = {
    domain: 'test.example.com',
    name: 'Tokyo-01',
    country: 'JP',
    source: 'cloud',
    durationSec: 60,
    ruleMode: 'global',
    os: 'macos',
    appVersion: '0.4.0',
  };

  render(<DisconnectFeedbackDialog />);
  fireEvent.click(screen.getByText('Good'));

  await waitFor(() => {
    expect(screen.queryByText('How was your connection?')).not.toBeInTheDocument();
  });
  // No toast for good
  expect(mockShowAlert).not.toHaveBeenCalled();
  // Rating submitted
  await waitFor(() => {
    expect(mockPost).toHaveBeenCalledWith(
      '/api/user/connection-rating',
      expect.objectContaining({ rating: 'good' }),
    );
  });
  // No ticket created
  const ticketCalls = mockPost.mock.calls.filter(
    (call: any[]) => call[0] === '/api/user/ticket',
  );
  expect(ticketCalls).toHaveLength(0);
});
```

**Update the "bad" test** — verify `auto_generated: true` and rating:

```typescript
it('点击"不好"提交 ticket (auto_generated) + rating', async () => {
  mockStoreState.pendingFeedback = true;
  mockStoreState.lastConnectionInfo = {
    domain: 'test.example.com',
    name: 'Tokyo-01',
    country: 'JP',
    source: 'cloud',
    durationSec: 300,
    ruleMode: 'chnroute',
    os: 'macos',
    appVersion: '0.4.0',
  };

  render(<DisconnectFeedbackDialog />);
  fireEvent.click(screen.getByText('Bad'));

  await waitFor(() => {
    expect(screen.queryByText('How was your connection?')).not.toBeInTheDocument();
  });
  expect(mockShowAlert).toHaveBeenCalledWith('Thanks', 'info');

  // Ticket with auto_generated flag
  await waitFor(() => {
    expect(mockPost).toHaveBeenCalledWith(
      '/api/user/ticket',
      expect.objectContaining({ auto_generated: true }),
    );
  });

  // Rating also submitted
  await waitFor(() => {
    expect(mockPost).toHaveBeenCalledWith(
      '/api/user/connection-rating',
      expect.objectContaining({ rating: 'bad' }),
    );
  });
});
```

- [ ] **Step 3: Run tests**

Run: `cd webapp && npx vitest run src/components/__tests__/DisconnectFeedbackDialog.test.tsx --reporter=verbose`
Expected: All tests pass

- [ ] **Step 4: Commit**

```bash
cd webapp && git add src/components/DisconnectFeedbackDialog.tsx src/components/__tests__/DisconnectFeedbackDialog.test.tsx
git commit -m "feat: submit connection ratings on good/bad, mark auto-tickets"
```

---

### Task 10: Webapp — Bridge layer getNetworkEnv

**Files:**
- Modify: `webapp/src/services/tauri-k2.ts`
- Modify: `webapp/src/services/standalone-k2.ts`

The `getNetworkEnv()` call in Task 9 uses `window._k2.run('network-env')` which routes through the existing bridge action dispatch. No new bridge method needed — the daemon's `handleCore` already routes unknown actions to a 400 error, but we need to add `network-env` as a recognized action in the daemon.

Actually, the simpler approach: the webapp calls `_k2.run('network-env')` which goes through the existing bridge. For Tauri, this invokes `daemon_exec` → daemon `handleCore` → needs a new case. For Capacitor, the K2Plugin needs to call `engine.NetworkEnvJSON()`. For standalone, it returns empty.

The daemon action is handled in Task 4 above. For the bridges, no changes needed since they already route arbitrary actions through the generic `run()` method. The only adjustment: standalone bridge should return `{ code: 0, data: {} }` for `network-env` instead of hitting the daemon.

- [ ] **Step 1: Add network-env handling to standalone bridge**

In `standalone-k2.ts`, the existing `coreExec` function sends all actions via HTTP POST. For standalone mode (no daemon), `network-env` will fail with "Service unavailable". This is acceptable — the network env data will just be empty. No changes needed.

However, for Capacitor, we need the K2Plugin to handle this. This will be done when the mobile build targets are updated (K2Plugin.swift and K2Plugin.kt). For now, `network-env` returns an error on mobile, which is handled gracefully (empty data).

- [ ] **Step 2: Add network-env action to daemon handleCore**

In `k2/daemon/api.go`, add to the `handleCore` switch:

```go
case "network-env":
	d.handleNetworkEnv(w)
```

Add handler:

```go
func (d *Daemon) handleNetworkEnv(w http.ResponseWriter) {
	d.mu.RLock()
	eng := d.engine
	d.mu.RUnlock()

	if eng == nil {
		writeJSON(w, Response{Code: 0, Message: "ok", Data: map[string]any{}})
		return
	}
	env := eng.NetworkEnv()
	if env == nil {
		writeJSON(w, Response{Code: 0, Message: "ok", Data: map[string]any{}})
		return
	}
	writeJSON(w, Response{Code: 0, Message: "ok", Data: env})
}
```

- [ ] **Step 3: Verify daemon build**

Run: `cd k2 && go build ./daemon/`
Expected: Build succeeds

- [ ] **Step 4: Commit**

```bash
cd k2 && git add daemon/api.go
git commit -m "feat(daemon): add network-env action to core API"
```

---

### Task 11: Manager Dashboard — API client and types

**Files:**
- Modify: `web/src/lib/api.ts` — add `getConnectionRatingStatistics` method and types

- [ ] **Step 1: Add response types to api.ts**

Add near the other statistics response types:

```typescript
export interface ConnectionRatingStatisticsResponse {
  summary: {
    total: number;
    good: number;
    bad: number;
    goodRate: number;
  };
  trend: Array<{
    date: string;
    total: number;
    good: number;
    bad: number;
    goodRate: number;
  }>;
  byServer: Array<{
    domain: string;
    name: string;
    country: string;
    total: number;
    good: number;
    bad: number;
    goodRate: number;
  }>;
  byISP: Array<{
    isp: string;
    country: string;
    total: number;
    good: number;
    goodRate: number;
  }>;
  byRouter: Array<{
    brand: string;
    total: number;
    good: number;
    goodRate: number;
  }>;
  byPlatform: Array<{
    os: string;
    appVersion: string;
    total: number;
    good: number;
    goodRate: number;
  }>;
  byUser: Array<{
    userId: number;
    email: string;
    total: number;
    good: number;
    bad: number;
    goodRate: number;
  }>;
}
```

- [ ] **Step 2: Add API method**

Add to the `api` object methods:

```typescript
async getConnectionRatingStatistics(
  period: '7d' | '30d' | '90d' = '7d'
): Promise<ConnectionRatingStatisticsResponse> {
  return this.request<ConnectionRatingStatisticsResponse>(
    `/app/connection-ratings/statistics?period=${period}`
  );
},
```

- [ ] **Step 3: Verify TypeScript build**

Run: `cd web && npx tsc --noEmit`
Expected: No type errors

- [ ] **Step 4: Commit**

```bash
cd web && git add src/lib/api.ts
git commit -m "feat(web): add connection rating statistics API client"
```

---

### Task 12: Manager Dashboard — Overview KPI card

**Files:**
- Modify: `web/src/app/(manager)/manager/page.tsx` — add Connection Quality card + tab

- [ ] **Step 1: Add state and data fetching**

In `page.tsx`, add state for rating stats:

```typescript
const [ratingStats, setRatingStats] = useState<ConnectionRatingStatisticsResponse | null>(null);
```

Add to `loadData()` Promise.all:

```typescript
const [deviceData, userData, orderData, ratingData] = await Promise.all([
  api.getDeviceStatistics(),
  api.getUserStatistics(),
  api.getOrderStatistics(),
  api.getConnectionRatingStatistics('7d'),
]);
// ...existing sets...
setRatingStats(ratingData);
```

Add import for the type:

```typescript
import {
  api,
  // ...existing imports...
  ConnectionRatingStatisticsResponse,
} from "@/lib/api";
```

- [ ] **Step 2: Add KPI card to Overview tab**

In the Overview tab's grid (the `grid-cols-4` section), add a 5th card. Change grid to `lg:grid-cols-5`:

```tsx
<Card>
  <CardHeader className="pb-2">
    <CardDescription>连接好评率 (7天)</CardDescription>
    <CardTitle className="text-3xl">
      {ratingStats ? `${(ratingStats.summary.goodRate * 100).toFixed(1)}%` : '-'}
    </CardTitle>
  </CardHeader>
  <CardContent>
    <div className="text-sm text-muted-foreground">
      共 {ratingStats?.summary.total ?? 0} 条评价 | 差评 {ratingStats?.summary.bad ?? 0}
    </div>
  </CardContent>
</Card>
```

- [ ] **Step 3: Add "Connection Quality" tab trigger**

Add to TabsList:

```tsx
<TabsTrigger value="quality">连接质量</TabsTrigger>
```

Add placeholder TabsContent (full implementation in Task 13):

```tsx
<TabsContent value="quality" className="space-y-6">
  <ConnectionQualityTab />
</TabsContent>
```

- [ ] **Step 4: Verify dev build**

Run: `cd web && npx tsc --noEmit`
Expected: No type errors (ConnectionQualityTab will be created in next task)

- [ ] **Step 5: Commit**

```bash
cd web && git add src/app/\(manager\)/manager/page.tsx
git commit -m "feat(web): add Connection Quality KPI card and tab to dashboard"
```

---

### Task 13: Manager Dashboard — Connection Quality tab

**Files:**
- Create: `web/src/app/(manager)/manager/connection-quality-tab.tsx`

- [ ] **Step 1: Create ConnectionQualityTab component**

```tsx
"use client";

import { useEffect, useState } from "react";
import { api, ConnectionRatingStatisticsResponse } from "@/lib/api";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

function formatRate(rate: number): string {
  return `${(rate * 100).toFixed(1)}%`;
}

export function ConnectionQualityTab() {
  const [period, setPeriod] = useState<"7d" | "30d" | "90d">("30d");
  const [stats, setStats] = useState<ConnectionRatingStatisticsResponse | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    setLoading(true);
    api
      .getConnectionRatingStatistics(period)
      .then(setStats)
      .catch((err) => console.error("Failed to load rating stats:", err))
      .finally(() => setLoading(false));
  }, [period]);

  if (loading && !stats) {
    return <div className="text-muted-foreground text-center py-12">加载中...</div>;
  }

  if (!stats) {
    return <div className="text-muted-foreground text-center py-12">暂无数据</div>;
  }

  return (
    <div className="space-y-6">
      {/* Period selector */}
      <div className="flex justify-end">
        <Select value={period} onValueChange={(v) => setPeriod(v as "7d" | "30d" | "90d")}>
          <SelectTrigger className="w-32">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="7d">最近 7 天</SelectItem>
            <SelectItem value="30d">最近 30 天</SelectItem>
            <SelectItem value="90d">最近 90 天</SelectItem>
          </SelectContent>
        </Select>
      </div>

      {/* Summary cards */}
      <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">总评价</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{stats.summary.total}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">好评率</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{formatRate(stats.summary.goodRate)}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">好评</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-green-500">{stats.summary.good}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">差评</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-red-500">{stats.summary.bad}</div>
          </CardContent>
        </Card>
      </div>

      {/* Trend — hand-rolled bar chart (matches usages page pattern) */}
      <Card>
        <CardHeader>
          <CardTitle>好评率趋势</CardTitle>
        </CardHeader>
        <CardContent>
          {stats.trend.length === 0 ? (
            <div className="text-muted-foreground text-center py-8">暂无数据</div>
          ) : (
            <div className="flex items-end gap-1 h-48 overflow-x-auto">
              {stats.trend.map((item) => {
                const goodPct = item.total > 0 ? (item.good / item.total) * 100 : 0;
                const badPct = item.total > 0 ? (item.bad / item.total) * 100 : 0;
                return (
                  <div
                    key={item.date}
                    className="flex-shrink-0 flex flex-col items-center gap-1"
                    style={{ width: stats.trend.length > 30 ? '12px' : '24px' }}
                    title={`${item.date}: ${formatRate(item.goodRate)} (${item.good}/${item.total})`}
                  >
                    <div className="text-xs text-muted-foreground">
                      {item.total > 0 ? formatRate(item.goodRate) : ''}
                    </div>
                    <div className="w-full flex flex-col justify-end" style={{ height: '100%' }}>
                      {/* Bad (red) stacked on top of good (green) */}
                      <div
                        className="w-full bg-red-500 rounded-t"
                        style={{ height: `${badPct}%`, minHeight: item.bad > 0 ? '2px' : '0' }}
                      />
                      <div
                        className="w-full bg-green-500"
                        style={{ height: `${goodPct}%`, minHeight: item.good > 0 ? '2px' : '0' }}
                      />
                    </div>
                    <div className="text-xs text-muted-foreground rotate-45 origin-left whitespace-nowrap">
                      {item.date.slice(5, 10)}
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </CardContent>
      </Card>

      {/* By Server */}
      <Card>
        <CardHeader>
          <CardTitle>按服务器</CardTitle>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>服务器</TableHead>
                <TableHead>地区</TableHead>
                <TableHead className="text-right">好评</TableHead>
                <TableHead className="text-right">差评</TableHead>
                <TableHead className="text-right">总计</TableHead>
                <TableHead className="text-right">好评率</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {stats.byServer.map((item) => (
                <TableRow key={item.domain}>
                  <TableCell>{item.name || item.domain}</TableCell>
                  <TableCell>{item.country}</TableCell>
                  <TableCell className="text-right">{item.good}</TableCell>
                  <TableCell className="text-right">{item.bad}</TableCell>
                  <TableCell className="text-right">{item.total}</TableCell>
                  <TableCell className="text-right">{formatRate(item.goodRate)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      {/* By ISP */}
      <Card>
        <CardHeader>
          <CardTitle>按运营商</CardTitle>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>运营商</TableHead>
                <TableHead>国家</TableHead>
                <TableHead className="text-right">总计</TableHead>
                <TableHead className="text-right">好评率</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {stats.byISP.map((item) => (
                <TableRow key={item.isp}>
                  <TableCell>{item.isp}</TableCell>
                  <TableCell>{item.country}</TableCell>
                  <TableCell className="text-right">{item.total}</TableCell>
                  <TableCell className="text-right">{formatRate(item.goodRate)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      {/* By Router */}
      <Card>
        <CardHeader>
          <CardTitle>按路由器品牌</CardTitle>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>品牌</TableHead>
                <TableHead className="text-right">总计</TableHead>
                <TableHead className="text-right">好评率</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {stats.byRouter.map((item) => (
                <TableRow key={item.brand}>
                  <TableCell>{item.brand}</TableCell>
                  <TableCell className="text-right">{item.total}</TableCell>
                  <TableCell className="text-right">{formatRate(item.goodRate)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      {/* By Platform */}
      <Card>
        <CardHeader>
          <CardTitle>按平台与版本</CardTitle>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>系统</TableHead>
                <TableHead>版本</TableHead>
                <TableHead className="text-right">总计</TableHead>
                <TableHead className="text-right">好评率</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {stats.byPlatform.map((item, i) => (
                <TableRow key={`${item.os}-${item.appVersion}-${i}`}>
                  <TableCell>{item.os}</TableCell>
                  <TableCell>{item.appVersion}</TableCell>
                  <TableCell className="text-right">{item.total}</TableCell>
                  <TableCell className="text-right">{formatRate(item.goodRate)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      {/* By User */}
      <Card>
        <CardHeader>
          <CardTitle>按用户 (低好评率 Top 50，最少 3 条评价)</CardTitle>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>用户</TableHead>
                <TableHead className="text-right">好评</TableHead>
                <TableHead className="text-right">差评</TableHead>
                <TableHead className="text-right">总计</TableHead>
                <TableHead className="text-right">好评率</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {stats.byUser.map((item) => (
                <TableRow key={item.userId}>
                  <TableCell>{item.email}</TableCell>
                  <TableCell className="text-right">{item.good}</TableCell>
                  <TableCell className="text-right">{item.bad}</TableCell>
                  <TableCell className="text-right">{item.total}</TableCell>
                  <TableCell className="text-right">{formatRate(item.goodRate)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </div>
  );
}
```

- [ ] **Step 2: Import in page.tsx**

Add import at top of `page.tsx`:

```typescript
import { ConnectionQualityTab } from "./connection-quality-tab";
```

- [ ] **Step 3: Verify TypeScript build**

Run: `cd web && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
cd web && git add src/app/\(manager\)/manager/connection-quality-tab.tsx src/app/\(manager\)/manager/page.tsx
git commit -m "feat(web): add Connection Quality tab with full statistics dashboard"
```

---

### Task 14: Final verification

- [ ] **Step 1: Build k2 submodule**

Run: `cd k2 && go build ./... && go test -short ./engine/ -timeout 60s`
Expected: Build and tests pass

- [ ] **Step 2: Build Center API**

Run: `cd api && go build ./...`
Expected: Build succeeds

- [ ] **Step 3: Build webapp**

Run: `cd webapp && npx tsc --noEmit`
Expected: No type errors

- [ ] **Step 4: Run webapp tests**

Run: `cd webapp && npx vitest run --reporter=verbose`
Expected: All tests pass

- [ ] **Step 5: Build web**

Run: `cd web && npx tsc --noEmit`
Expected: No type errors

- [ ] **Step 6: Commit all remaining changes**

```bash
git add -A && git status
```

Ensure only expected files are staged. Create final integration commit if there are uncommitted changes.
