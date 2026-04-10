//go:build linux

package gateway

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/kaitu-io/k2/webui"
	"github.com/sasha-s/go-deadlock"
)

// RouterDeviceEntry is a MAC in the allowlist.
type RouterDeviceEntry struct {
	MAC     string `json:"mac"`
	Remark  string `json:"remark"`
	AddedAt int64  `json:"addedAt"`
}

// RouterDeviceManager manages the MAC allowlist and nftables enforcement.
type RouterDeviceManager struct {
	mu          deadlock.RWMutex
	entries     []RouterDeviceEntry
	mode        string // "open" or "allowlist"
	quota       int    // from User Tier MaxLanClient (0=no limit, -1=unlimited, >0=exact)
	lanSubnets  []string
	interceptor Interceptor
	storage     *webui.Storage
	onModeChange func() // called when mode changes, triggers interceptor reinstall
}

// RouterDeviceListResponse is the GET /api/router-devices response.
type RouterDeviceListResponse struct {
	Mode         string             `json:"mode"`
	Quota        int                `json:"quota"`
	Used         int                `json:"used"`
	RouterDevices []RouterDeviceInfo `json:"routerDevices"`
}

// RouterDeviceInfo merges discovered device + allowlist entry.
type RouterDeviceInfo struct {
	MAC      string `json:"mac"`
	IP       string `json:"ip"`
	Hostname string `json:"hostname"`
	Online   bool   `json:"online"`
	Allowed  bool   `json:"allowed"`
	Remark   string `json:"remark"`
}

func (m *RouterDeviceManager) loadFromStorage() {
	if m.storage == nil {
		return
	}
	if val, err := m.storage.Get("router-device-allowlist"); err == nil && val != "" {
		var entries []RouterDeviceEntry
		if json.Unmarshal([]byte(val), &entries) == nil {
			m.entries = entries
		}
	}
	if val, err := m.storage.Get("router-device-mode"); err == nil && val != "" {
		var mode string
		if json.Unmarshal([]byte(val), &mode) == nil && (mode == "open" || mode == "allowlist") {
			m.mode = mode
		}
	}
	if m.mode == "" {
		m.mode = "open"
	}
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

	lanDevices := discoverLANDevices(subnets)

	allowedMACs := make(map[string]RouterDeviceEntry)
	for _, e := range entries {
		allowedMACs[e.MAC] = e
	}

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
		Mode: mode, Quota: quota, Used: len(entries), RouterDevices: result,
	}})
}

func (m *RouterDeviceManager) handleAllow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
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
	// Check quota (>0 = exact limit, 0 or -1 = unlimited)
	if m.quota > 0 && len(m.entries) >= m.quota {
		m.mu.Unlock()
		writeJSON(w, Response{Code: 1, Message: "quotaExceeded"})
		return
	}
	// Check duplicate
	for _, e := range m.entries {
		if e.MAC == mac {
			m.mu.Unlock()
			writeJSON(w, Response{Code: 1, Message: "already in allowlist"})
			return
		}
	}
	entry := RouterDeviceEntry{MAC: mac, Remark: body.Remark, AddedAt: time.Now().Unix()}
	m.entries = append(m.entries, entry)
	inAllowlist := m.mode == "allowlist"
	cb := m.onModeChange
	snapshot := m.persistSnapshot()
	m.mu.Unlock()

	// I/O outside lock: persist to disk + full interceptor reinstall
	m.persistFromSnapshot(snapshot)
	if inAllowlist && cb != nil {
		cb()
	}

	writeJSON(w, Response{Code: 0, Message: "ok"})
}

func (m *RouterDeviceManager) handleRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	var body struct {
		MAC string `json:"mac"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, Response{Code: 1, Message: "bad request"})
		return
	}
	mac := normalizeMAC(body.MAC)

	m.mu.Lock()
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
		m.mu.Unlock()
		writeJSON(w, Response{Code: 1, Message: "not in allowlist"})
		return
	}
	m.entries = remaining
	inAllowlist := m.mode == "allowlist"
	cb := m.onModeChange
	snapshot := m.persistSnapshot()
	m.mu.Unlock()

	// I/O outside lock: persist to disk + full interceptor reinstall
	m.persistFromSnapshot(snapshot)
	if inAllowlist && cb != nil {
		cb()
	}

	writeJSON(w, Response{Code: 0, Message: "ok"})
}

func (m *RouterDeviceManager) handleMode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	var body struct {
		Mode string `json:"mode"`
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
	snapshot := m.persistSnapshot()
	cb := m.onModeChange
	m.mu.Unlock()

	// I/O outside lock
	m.persistFromSnapshot(snapshot)
	if cb != nil {
		cb()
	}

	slog.Info("gateway: router device mode changed", "mode", body.Mode)
	writeJSON(w, Response{Code: 0, Message: "ok"})
}

func (m *RouterDeviceManager) getAllowedMACs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	macs := make([]string, len(m.entries))
	for i, e := range m.entries {
		macs[i] = e.MAC
	}
	return macs
}

type persistData struct {
	entries []RouterDeviceEntry
	mode    string
}

// persistSnapshot captures data under lock for later persistence outside lock.
func (m *RouterDeviceManager) persistSnapshot() persistData {
	entries := make([]RouterDeviceEntry, len(m.entries))
	copy(entries, m.entries)
	return persistData{entries: entries, mode: m.mode}
}

// persistFromSnapshot writes the captured snapshot to storage (I/O — call outside lock).
func (m *RouterDeviceManager) persistFromSnapshot(snap persistData) {
	if m.storage == nil {
		return
	}
	data, _ := json.Marshal(snap.entries)
	_ = m.storage.Set("router-device-allowlist", string(data))
	modeData, _ := json.Marshal(snap.mode)
	_ = m.storage.Set("router-device-mode", string(modeData))
}

func normalizeMAC(s string) string {
	hw, err := net.ParseMAC(s)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X", hw[0], hw[1], hw[2], hw[3], hw[4], hw[5])
}
