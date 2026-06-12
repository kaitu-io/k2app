//go:build linux

package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/kaitu-io/k2/safego"
)

// postBeacon POSTs the router's LAN IP + panel port to Center's
// /api/pair/beacon endpoint (no auth) so webapp can discover the router under
// the same public egress IP via GET /api/pair/discover.
func postBeacon(client *http.Client, base, lanIP string, port int) error {
	body, _ := json.Marshal(map[string]any{"lanIP": lanIP, "port": port})
	req, err := http.NewRequest(http.MethodPost, base+"/api/pair/beacon", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("beacon post: %w", err)
	}
	resp.Body.Close()
	return nil
}

// startBeacon 周期性向 Center 上报本机 LAN IP(供 webapp 公网-IP 发现)。每 60s 一次;
// 失败静默重试。即使 k2r 未配置凭证也发——发现是无条件能力。base 为空则 no-op。
func (g *Gateway) startBeacon(base string) {
	if base == "" {
		return
	}
	client := &http.Client{Timeout: 10 * time.Second}
	safego.Go(func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			if ip := mdnsAnswerIP(localPrivateV4s()); ip != nil {
				if err := postBeacon(client, base, ip.String(), 1779); err != nil {
					slog.Debug("beacon failed", "err", err)
				}
			}
			select {
			case <-g.ctx.Done():
				return
			case <-ticker.C:
			}
		}
	})
}
