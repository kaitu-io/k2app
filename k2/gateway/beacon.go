//go:build linux

package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/kaitu-io/k2/safego"
)

// postBeacon POSTs the router's LAN IP + panel port to Center's
// /api/pair/beacon endpoint (no auth) so webapp can discover the router under
// the same public egress IP via GET /api/pair/discover. The request is bound to
// ctx so a gateway shutdown cancels an in-flight post immediately.
func postBeacon(ctx context.Context, client *http.Client, base, lanIP string, port int) error {
	body, _ := json.Marshal(map[string]any{"lanIP": lanIP, "port": port})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/pair/beacon", bytes.NewReader(body))
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

// startBeacon 周期性向 Center 上报本机 LAN IP + 面板端口(供 webapp 公网-IP 发现)。
// 每 60s 一次;失败静默重试。即使 k2r 未配置凭证也发——发现是无条件能力。base 为空则 no-op。
// port 是 Run 解析出的真实面板端口(默认 1779),webapp 用它拼 http://lanIP:port。
func (g *Gateway) startBeacon(base string, port int) {
	if base == "" {
		return
	}
	client := &http.Client{Timeout: 10 * time.Second}
	safego.Go(func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			if ip := mdnsAnswerIP(localPrivateV4s()); ip != nil {
				if err := postBeacon(g.ctx, client, base, ip.String(), port); err != nil {
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
