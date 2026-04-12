package center

import (
	"regexp"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/log"
)

// ------------------------------------------------------------------
// Rule-miss telemetry endpoint (Phase 1 — "dark instrumentation")
//
// Accepts anonymous rule-miss batches from k2 clients and drops them
// on the floor. Phase 1 has no persistence — we only validate the wire
// format, emit an INFO log per accepted batch, and update a metrics
// counter. Phase 2 will add the RuleMiss GORM model + retention worker.
//
// Route: POST /api/telemetry/rule_miss  (UNAUTHENTICATED, rate limited)
//
// Schema matches k2/engine/miss_reporter.go ruleMissBatch. Keep in sync.
// ------------------------------------------------------------------

const (
	ruleMissSchemaVersion = 1
	ruleMissMaxRecords    = 200
)

// RuleMissBatch is the JSON body accepted at /api/telemetry/rule_miss.
// Mirrors k2/engine/miss_reporter.go ruleMissBatch.
type RuleMissBatch struct {
	SchemaVersion int              `json:"schema_version"`
	ClientVersion string           `json:"client_version"`
	RulesVersion  string           `json:"rules_version"`
	SaltDay       string           `json:"salt_day"`
	Records       []RuleMissRecord `json:"records"`
}

// RuleMissRecord is a single hashed rule-miss observation.
// Revealed is always empty in Phase 1 — Phase 2 k-anonymous reveal will
// populate it.
type RuleMissRecord struct {
	Hash16     string `json:"hash16"`
	Country    string `json:"country"`
	WeekBucket string `json:"week_bucket"`
	Protocol   string `json:"protocol"`
	Revealed   string `json:"revealed,omitempty"`
}

// Validation regexes — compiled once at package init.
var (
	ruleMissHash16Re     = regexp.MustCompile(`^[0-9a-f]{16}$`)
	ruleMissCountryRe    = regexp.MustCompile(`^([A-Z]{2}|XX)$`)
	ruleMissWeekBucketRe = regexp.MustCompile(`^\d{4}-W\d{2}$`)
)

// api_telemetry_rule_miss handles POST /api/telemetry/rule_miss.
//
// Phase 1 behavior: validate, log, drop. No DB writes.
//
// Returns 200 on accept (empty body via Success) and an error code on
// validation failure. The HTTP body contract follows the Center
// convention: HTTP status is always 200, business state in JSON code.
func api_telemetry_rule_miss(c *gin.Context) {
	// Basic in-memory rate limit: 10 req/min per source IP. This is a
	// defensive guard — the endpoint is public and we don't want a
	// single IP flooding Center. Phase 2 will add proper metering.
	if !ruleMissRateLimiter.Allow(c.ClientIP()) {
		Error(c, ErrorTooManyRequests, "rate limited")
		return
	}

	var req RuleMissBatch
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, "invalid request body")
		return
	}

	if req.SchemaVersion != ruleMissSchemaVersion {
		Error(c, ErrorInvalidArgument, "unsupported schema_version")
		return
	}

	if len(req.Records) == 0 {
		// Empty batch is a valid no-op — accept silently.
		SuccessEmpty(c)
		return
	}

	if len(req.Records) > ruleMissMaxRecords {
		Error(c, ErrorInvalidArgument, "too many records")
		return
	}

	// Validate every record. Any single malformed record rejects the
	// whole batch — clients shouldn't upload garbage.
	for i, rec := range req.Records {
		if !ruleMissHash16Re.MatchString(rec.Hash16) {
			log.Debugf(c, "rule_miss: bad hash16 at index %d", i)
			Error(c, ErrorInvalidArgument, "invalid hash16")
			return
		}
		if !ruleMissCountryRe.MatchString(rec.Country) {
			log.Debugf(c, "rule_miss: bad country at index %d", i)
			Error(c, ErrorInvalidArgument, "invalid country")
			return
		}
		if !ruleMissWeekBucketRe.MatchString(rec.WeekBucket) {
			log.Debugf(c, "rule_miss: bad week_bucket at index %d", i)
			Error(c, ErrorInvalidArgument, "invalid week_bucket")
			return
		}
		// Protocol is optional (may be "") — no validation needed beyond
		// rejecting arbitrarily long strings.
		if len(rec.Protocol) > 8 {
			Error(c, ErrorInvalidArgument, "invalid protocol")
			return
		}
		// Revealed MUST be empty in Phase 1.
		if rec.Revealed != "" {
			Error(c, ErrorInvalidArgument, "revealed not supported in phase 1")
			return
		}
	}

	// Pick a representative country for the log line. If the batch
	// contains mixed countries, pick the first — we're not persisting
	// anyway.
	sampleCountry := req.Records[0].Country
	log.Infof(c, "rule_miss: got %d records from country=%s client=%s salt_day=%s",
		len(req.Records), sampleCountry, req.ClientVersion, req.SaltDay)

	// TODO(phase-2): persist to DB via GORM RuleMiss model +
	// retention worker (raw 3 days, aggregated 30 days).

	SuccessEmpty(c)
}

// ------------------------------------------------------------------
// In-memory per-IP token bucket rate limiter.
// 10 requests/minute per source IP. Reset each minute.
// Phase 1 only — deliberately simple; Phase 2 can move to Redis.
// ------------------------------------------------------------------

type ruleMissIPLimiter struct {
	mu      sync.Mutex
	buckets map[string]*ruleMissBucket
}

type ruleMissBucket struct {
	resetAt time.Time
	count   int
}

// ruleMissRateLimiter is the package-level limiter shared by the handler.
var ruleMissRateLimiter = &ruleMissIPLimiter{
	buckets: make(map[string]*ruleMissBucket),
}

// Allow returns true if the given IP may send another request. At the
// end of each minute window the bucket resets and the count starts over.
// Map entries are garbage collected lazily — stale buckets live at most
// until their next Allow() call.
func (l *ruleMissIPLimiter) Allow(ip string) bool {
	const window = 1 * time.Minute
	const limit = 10

	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	// Opportunistic cleanup: when the map grows large, sweep expired.
	if len(l.buckets) > 1024 {
		for k, v := range l.buckets {
			if now.After(v.resetAt) {
				delete(l.buckets, k)
			}
		}
	}

	b, ok := l.buckets[ip]
	if !ok || now.After(b.resetAt) {
		l.buckets[ip] = &ruleMissBucket{
			resetAt: now.Add(window),
			count:   1,
		}
		return true
	}
	if b.count >= limit {
		return false
	}
	b.count++
	return true
}
