package rule

// MissReporter is an optional sink for rule-fallback events, used to collect
// anonymous domain-suffix feedback for improving the rule corpus.
//
// The implementation MUST be non-blocking in Report — the Engine calls it
// from matchHostLocked / matchConnLocked which are on the DNS / L4 dial
// hot path. A typical implementation is a non-blocking channel send with
// a drop-on-full policy.
//
// Report receives the raw host (FQDN, lowercased) and a protocol hint
// ("tcp", "udp", or ""). It is the reporter's responsibility to extract
// the ETLD+1, hash it, and forward to transport.
//
// Phase 1 (rule-miss telemetry): reporter is nil by default. The engine
// calls it only when the fallback target was returned AND the host looks
// like an FQDN. When nil, the call site short-circuits with zero overhead.
type MissReporter interface {
	Report(host string, protocol string)
}
