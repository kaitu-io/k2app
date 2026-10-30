package wire

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/kaitu-io/k2/safego"
	"github.com/sasha-s/go-deadlock"
)

// Node-level auth-fault alerting (spec §6.4).
//
// §6.4 pins this as a REQUIRED item, not an optional nicety: a node stuck on
// `DIAG: auth-center-unreachable` for more than a few minutes must page,
// because the node itself can never tell "Center is down" from "someone is
// selectively cutting THIS node off from Center" — and the second case is
// exactly how a user who expects to be banned stretches the 15-minute
// revocation SLA into the 6-hour self-governance grace (session.go's
// cookieGraceMax).
//
// The discriminator does not live in any single node; it lives in the alert
// stream. Center genuinely being down makes every node post at once. ONE node
// posting while the rest stay quiet is the anomaly worth waking someone for.
// That is also why the node posts to Slack DIRECTLY instead of reporting the
// condition through Center: the single path known to be in question is the
// path to Center.
//
// Entirely opt-in. NewSlackAlerter returns nil when no webhook is configured
// and every method here is nil-receiver safe, so a node without Slack behaves
// exactly as it did before this file existed.

const (
	// authUnreachableAlertAfter is how long Center must stay undecidable
	// before paging. Spec §6.4 asks for "well under 6 hours (5-10 min
	// suggested)" — long enough to ride out a redeploy or a brief blip, short
	// enough that the 6-hour grace cannot be silently farmed.
	authUnreachableAlertAfter = 5 * time.Minute

	// alertCooldown throttles repeats of the SAME alert key. A Center outage
	// touches every auth on the node; without this, one incident would post
	// thousands of identical messages.
	alertCooldown = 30 * time.Minute

	alertHTTPTimeout = 10 * time.Second
)

// SlackAlerter posts operational alerts to an incoming-webhook URL.
//
// The webhook URL is a credential: it is never logged, and never included in
// an alert body. Only the node label and the event go over the wire.
type SlackAlerter struct {
	webhook string
	node    string
	client  *http.Client

	// mu is a standalone STATE lock guarding lastSent only — registered in
	// k2/CLAUDE.md's Lock Ordering Graph as `wire.SlackAlerter.mu`. The HTTP
	// POST happens strictly OUTSIDE it (constitutional Rule #2), on its own
	// goroutine, so an auth path never blocks on Slack.
	mu       deadlock.Mutex
	lastSent map[string]time.Time
	now      func() time.Time // injectable clock for tests
}

// NewSlackAlerter returns an alerter for webhookURL, or nil when webhookURL is
// empty. A nil *SlackAlerter is a working no-op — callers must NOT branch on
// it. node labels the alert (e.g. the node name or IPv4) so an operator can
// tell at a glance whether one node or the whole fleet is affected.
func NewSlackAlerter(webhookURL, node string) *SlackAlerter {
	if webhookURL == "" {
		return nil
	}
	if node == "" {
		node = "unknown-node"
	}
	return &SlackAlerter{
		webhook:  webhookURL,
		node:     node,
		client:   &http.Client{Timeout: alertHTTPTimeout},
		lastSent: make(map[string]time.Time),
		now:      time.Now,
	}
}

// Fire posts text if key is outside its cooldown, and reports whether it did.
// Safe on a nil receiver (no webhook configured) and never blocks the caller:
// the POST runs on its own goroutine.
func (a *SlackAlerter) Fire(key, text string) bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	last, seen := a.lastSent[key]
	now := a.now()
	if seen && now.Sub(last) < alertCooldown {
		a.mu.Unlock()
		return false
	}
	a.lastSent[key] = now
	a.mu.Unlock()

	body := fmt.Sprintf("[k2s %s] %s", a.node, text)
	safego.Go(func() { a.post(body) })
	return true
}

// post performs the webhook round-trip. Delivery is best-effort: a failure is
// logged and dropped, never retried and never surfaced to the auth path — an
// unreachable Slack must not become a second outage.
func (a *SlackAlerter) post(text string) {
	payload, err := json.Marshal(struct {
		Text string `json:"text"`
	}{Text: text})
	if err != nil {
		slog.Warn("wire: alert marshal failed", "err", err)
		return
	}
	resp, err := a.client.Post(a.webhook, "application/json", bytes.NewReader(payload))
	if err != nil {
		// Deliberately does NOT log a.webhook — it is a credential.
		slog.Warn("wire: alert delivery failed", "err", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		slog.Warn("wire: alert rejected", "status", resp.StatusCode)
	}
}

// authHealth watches a Validator's verdicts and pages when THIS node stops
// being able to authenticate. It observes verdicts only — it never changes
// one, so wrapping a validator can never alter an admission decision.
type authHealth struct {
	alerter *SlackAlerter
	now     func() time.Time

	// mu is a standalone STATE lock guarding the two fields below —
	// registered as `wire.authHealth.mu`. Fire() is called outside it.
	mu               deadlock.Mutex
	unreachableSince time.Time
	alerted          bool
}

// WithAuthHealthAlerts wraps inner so that sustained Center-unreachability and
// node misconfiguration page an operator (spec §6.4). Returns inner unchanged
// when no alerter is configured, so an unconfigured node pays nothing — not
// even a mutex.
//
// Wrap the REMOTE validator, not the whole chain and not the cached chain: the
// remote validator is the only thing that actually talks to Center, so its
// verdicts are the only honest signal. Wrapping outside CachingValidator would
// see cache hits (Center was never asked) and mistake a healthy node for a
// silent one, or vice versa.
func WithAuthHealthAlerts(inner Validator, alerter *SlackAlerter) Validator {
	if alerter == nil {
		return inner
	}
	h := &authHealth{alerter: alerter, now: time.Now}
	return func(token, udid, mode string) AuthVerdict {
		v := inner(token, udid, mode)
		h.observe(v)
		return v
	}
}

// observe applies one verdict to the health state machine.
func (h *authHealth) observe(v AuthVerdict) {
	switch {
	case v.OK || v.Code == 401 || v.Code == 402 || v.Code == 403:
		// Center answered — whatever it said, the path to it works.
		h.recover()

	case v.Code == AuthCodeNodeFault:
		// Not transient and not Center's fault: this node is misconfigured and
		// stays broken until a human fixes it. Page on the first occurrence
		// rather than waiting out a window that would never expire on its own.
		h.alerter.Fire("auth-node-misconfigured",
			"DIAG: auth-node-misconfigured — this node cannot authenticate any device "+
				"(missing K2_NODE_SECRET or unresolvable node IPv4). Every device is being rejected.")

	case v.Code == AuthCodeUndetermined:
		h.markUnreachable()
	}
	// Code == 0 (no opinion) says nothing about Center — leave state alone.
}

func (h *authHealth) markUnreachable() {
	now := h.now()
	h.mu.Lock()
	if h.unreachableSince.IsZero() {
		h.unreachableSince = now
	}
	since := h.unreachableSince
	already := h.alerted
	shouldAlert := !already && now.Sub(since) >= authUnreachableAlertAfter
	if shouldAlert {
		h.alerted = true
	}
	h.mu.Unlock()

	if !shouldAlert {
		return
	}
	h.alerter.Fire("auth-center-unreachable", fmt.Sprintf(
		"DIAG: auth-center-unreachable for %s — this node cannot reach Center to validate "+
			"devices. Live sessions keep running on the 6h grace (spec §6.4). "+
			"If OTHER nodes are quiet, suspect targeted interference with this node's path "+
			"to Center, not a Center outage.",
		now.Sub(since).Round(time.Minute)))
}

func (h *authHealth) recover() {
	h.mu.Lock()
	wasAlerted := h.alerted
	h.unreachableSince = time.Time{}
	h.alerted = false
	h.mu.Unlock()

	if !wasAlerted {
		return
	}
	// Recovery is its own key so it is never swallowed by the outage alert's
	// cooldown — an alert that never clears trains people to ignore it.
	h.alerter.Fire("auth-center-recovered",
		"RECOVERED: Center is answering device-auth again.")
}
