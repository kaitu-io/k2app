package center

import (
	"crypto/subtle"
	"net/url"
	"os"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// parseServerURLDescriptor extracts ip, pin, and ech from a k2v5 server URL.
//
// Format: k2v5://domain:port?ech=<base64url>&pin=<sha256:...>[,<sha256:...>][&hop=start-end][&ip=x.x.x.x]
//
// If ?ip= is present, it is used as the IP; otherwise the hostname is used as a
// fallback. Returns ok=false when any of ip, pin, or ech are empty — the caller
// should skip entries with !ok rather than surfacing them.
func parseServerURLDescriptor(serverURL string) (ip, pin, ech string, ok bool) {
	u, err := url.Parse(serverURL)
	if err != nil {
		return "", "", "", false
	}
	q := u.Query()
	ech = q.Get("ech")
	pin = q.Get("pin")
	ip = q.Get("ip")
	if ip == "" {
		// Fallback: use hostname portion when ?ip= is absent
		ip = u.Hostname()
	}
	if ech == "" || pin == "" || ip == "" {
		return "", "", "", false
	}
	return ip, pin, ech, true
}

// seedNodeDescriptor is the wire shape for a single relay node in the seed payload.
type seedNodeDescriptor struct {
	IP  string `json:"ip"`
	Pin string `json:"pin"`
	ECH string `json:"ech"`
}

// handleAntiblockSeed returns a JSON seed payload of healthy shared relay nodes
// and control-plane entries for anti-block cold-start bootstrap.
//
// NOTE: This handler intentionally uses real HTTP status codes instead of the
// standard JSON error response (Error(c, code, msg)). It is consumed by CI
// (curl -fsS) and CDN publish pipelines that must fail-loud on error — returning
// HTTP 200 with a JSON error code would silently succeed to curl. This is the
// documented webhook-style exception to the project's "HTTP always 200" rule.
// See api_webhook.go for the same pattern.
//
//   - HTTP 503 — ANTIBLOCK_SEED_KEY env var is not set or empty.
//   - HTTP 401 — X-Antiblock-Seed-Key header is missing or does not match env value.
//   - HTTP 200 — success payload via Success() helper.
func handleAntiblockSeed(c *gin.Context) {
	// Env gate: endpoint is inert when the key is not configured.
	envKey := os.Getenv("ANTIBLOCK_SEED_KEY")
	if envKey == "" {
		log.Warnf(c, "[AntiblockSeed] ANTIBLOCK_SEED_KEY not configured")
		c.AbortWithStatus(503)
		return
	}

	// Constant-time compare guards against timing side-channels.
	reqKey := c.GetHeader("X-Antiblock-Seed-Key")
	if subtle.ConstantTimeCompare([]byte(reqKey), []byte(envKey)) != 1 {
		log.Warnf(c, "[AntiblockSeed] invalid or missing X-Antiblock-Seed-Key")
		c.AbortWithStatus(401)
		return
	}

	// Query all k2v5 non-test tunnels with their nodes.
	// Class and ServerURL filtering is done in-memory (no raw SQL required):
	// class=shared is checked via the preloaded Node.Class field;
	// server_url non-empty is implicitly gated by parseServerURLDescriptor.
	var tunnels []SlaveTunnel
	if err := db.Get().Model(&SlaveTunnel{}).
		Preload("Node").
		Where("node_id IS NOT NULL").
		Where(&SlaveTunnel{Protocol: TunnelProtocolK2V5, IsTest: BoolPtr(false)}).
		Find(&tunnels).Error; err != nil {
		log.Errorf(c, "[AntiblockSeed] failed to query tunnels: %v", err)
		c.AbortWithStatus(503)
		return
	}

	// Batch-load NodeUsage by IP for over-quota/offline gating and scoring.
	nodeIPs := make([]string, 0, len(tunnels))
	for _, t := range tunnels {
		if t.Node != nil {
			nodeIPs = append(nodeIPs, t.Node.Ipv4)
		}
	}
	usageMap := getNodeUsagesByIPs(nodeIPs)
	now := time.Now().Unix()

	type candidate struct {
		desc  seedNodeDescriptor
		score float64
	}
	seen := make(map[string]bool)
	var candidates []candidate

	for _, tunnel := range tunnels {
		if tunnel.Node == nil {
			continue
		}
		// Shared-pool only — private nodes are single-owner and must not be exposed.
		if tunnel.Node.Class != NodeClassShared {
			continue
		}
		// Hide over-quota or offline nodes (same gate as /api/tunnels).
		u := usageMap[tunnel.Node.Ipv4]
		if shouldHideTunnelForUser(u, false, now) {
			continue
		}
		// Parse the k2v5 descriptor; skip entries with missing ip/pin/ech.
		ip, pin, ech, ok := parseServerURLDescriptor(tunnel.ServerURL)
		if !ok {
			continue
		}
		// Dedupe by external IP address.
		if seen[ip] {
			continue
		}
		seen[ip] = true

		// Score via the single authority (ComputeRecommendScore).
		// buildTunnelInstanceDataFromUsage(nil) → nil → neutral 0.5.
		inst := buildTunnelInstanceDataFromUsage(u)
		score := ComputeRecommendScore(inst)

		candidates = append(candidates, candidate{
			desc:  seedNodeDescriptor{IP: ip, Pin: pin, ECH: ech},
			score: score,
		})
	}

	// Sort highest score first so the top-N cap favours healthier nodes.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	const topN = 16
	if len(candidates) > topN {
		candidates = candidates[:topN]
	}

	nodes := make([]seedNodeDescriptor, 0, len(candidates))
	for _, cand := range candidates {
		nodes = append(nodes, cand.desc)
	}

	// Default control-plane entries — parity with antiblock-encrypt.js DEFAULT_ENTRIES.
	entries := []string{"https://k2.52j.me"}

	log.Infof(c, "[AntiblockSeed] returning %d nodes, %d entries", len(nodes), len(entries))

	data := gin.H{
		"entries": entries,
		"nodes":   nodes,
	}
	Success(c, &data)
}
