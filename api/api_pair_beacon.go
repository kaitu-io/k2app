package center

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/redis"
)

// Pairing beacon / discover (Plan 5b D).
//
// A BYO k2r router periodically POSTs its LAN IP to Center. Center stores the
// candidate keyed by the *public* source IP (c.ClientIP()) with a short TTL.
// The webapp, behind the same public egress, GETs discover and renders an
// "open your router" link for each candidate.
//
// Security boundary: matching is by public IP only. Two different public IPs
// NEVER cross. The endpoint carries no credentials — worst case under CGNAT is
// a wrong "open router" link, never a credential leak.
const (
	beaconTTL      = 10 * time.Minute // candidate freshness window
	beaconMaxPerIP = 5                // cap candidates per public IP (CGNAT bound)
)

type beaconReq struct {
	LanIP string `json:"lanIP"`
	Port  int    `json:"port"`
}

type beaconCandidate struct {
	LanIP string `json:"lanIP"`
	Port  int    `json:"port"`
}

// beaconRedisKey scopes beacons to the reporting network's public IP.
func beaconRedisKey(publicIP string) string { return "pair:beacon:" + publicIP }

// api_pair_beacon — k2r router uplinks its LAN endpoint. No auth: an unconfigured
// router has no credentials yet but must still be discoverable on its LAN.
func api_pair_beacon(c *gin.Context) {
	var req beaconReq
	if err := c.ShouldBindJSON(&req); err != nil || req.LanIP == "" {
		Error(c, ErrorInvalidArgument, "bad beacon")
		return
	}
	if req.Port <= 0 || req.Port > 65535 {
		req.Port = 1779
	}

	ctx := context.Background()
	key := beaconRedisKey(c.ClientIP())
	val, _ := json.Marshal(beaconCandidate{LanIP: req.LanIP, Port: req.Port})

	rdb := redis.Client()
	if err := rdb.SAdd(ctx, key, val).Err(); err != nil {
		log.Warnf(c, "pair beacon SAdd failed for %s: %v", key, err)
		Error(c, ErrorSystemError, "store failed")
		return
	}
	if err := rdb.Expire(ctx, key, beaconTTL).Err(); err != nil {
		log.Warnf(c, "pair beacon Expire failed for %s: %v", key, err)
	}

	// Bound the set so a noisy network can't grow it without limit. Set + TTL
	// is the correctness guarantee; trim is best-effort cleanup.
	if n, err := rdb.SCard(ctx, key).Result(); err == nil && n > beaconMaxPerIP {
		_ = rdb.SPopN(ctx, key, n-beaconMaxPerIP).Err()
	}

	SuccessEmpty(c)
}

// api_pair_discover — webapp lists candidates reported from the same public IP.
func api_pair_discover(c *gin.Context) {
	if ReqUser(c) == nil {
		Error(c, ErrorNotLogin, "not login")
		return
	}

	members, err := redis.Client().SMembers(context.Background(), beaconRedisKey(c.ClientIP())).Result()
	if err != nil {
		log.Warnf(c, "pair discover SMembers failed: %v", err)
		Error(c, ErrorSystemError, "lookup failed")
		return
	}

	cands := make([]beaconCandidate, 0, len(members))
	for _, m := range members {
		var cand beaconCandidate
		if json.Unmarshal([]byte(m), &cand) == nil {
			cands = append(cands, cand)
		}
	}
	Success(c, &gin.H{"candidates": cands})
}
