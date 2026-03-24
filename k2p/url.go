package k2p

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/kaitu-io/k2/wire"
)

var udidRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,30}[a-z0-9]$`)

// ValidateUDID returns an error if udid does not match the k2p UDID format.
// Valid UDIDs: lowercase alphanumeric with internal hyphens, 3–32 characters total.
func ValidateUDID(udid string) error {
	if !udidRe.MatchString(udid) {
		return fmt.Errorf("k2p: invalid UDID %q: must match [a-z0-9][a-z0-9-]{1,30}[a-z0-9]", udid)
	}
	return nil
}

// ParseURL parses a k2p:// URL into a Config.
//
// URL format:
//
//	k2p://udid:peer-secret@udid.signal-domain:port?pin=...&ip=...&ip6=...&ech=...&fp=...
//
// The host must be "udid.signal-domain[:port]". The first label is stripped to
// derive the signal server domain. SignalServer is set to "https://{domain}:{port}".
func ParseURL(raw string) (Config, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Config{}, fmt.Errorf("k2p: parse URL: %w", err)
	}

	if u.Scheme != "k2p" {
		return Config{}, fmt.Errorf("k2p: unsupported scheme %q, want k2p", u.Scheme)
	}

	if u.User == nil {
		return Config{}, fmt.Errorf("k2p: missing auth (udid:secret@)")
	}

	udid := u.User.Username()
	secret, hasSecret := u.User.Password()
	if !hasSecret || secret == "" {
		return Config{}, fmt.Errorf("k2p: missing peer secret in URL")
	}

	if err := ValidateUDID(udid); err != nil {
		return Config{}, err
	}

	hostname := u.Hostname()
	portStr := u.Port()
	if portStr == "" {
		return Config{}, fmt.Errorf("k2p: missing port in URL")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return Config{}, fmt.Errorf("k2p: invalid port %q: %w", portStr, err)
	}

	// Strip the first label (UDID prefix) to get the signal server domain.
	// e.g. "my-home.signal.example.com" → "signal.example.com"
	_, baseDomain, hasDot := strings.Cut(hostname, ".")
	if !hasDot || baseDomain == "" {
		return Config{}, fmt.Errorf("k2p: host %q must be udid.signal-domain (need at least two labels)", hostname)
	}

	signalServer := fmt.Sprintf("https://%s:%d", baseDomain, port)

	wireCfg := wire.Config{
		Host:        baseDomain,
		Port:        port,
		Fingerprint: "chrome",
	}

	q := u.Query()
	if v := q.Get("pin"); v != "" {
		wireCfg.Pin = v
	}
	if v := q.Get("ip"); v != "" {
		wireCfg.IPv4 = v
	}
	if v := q.Get("ip6"); v != "" {
		wireCfg.IPv6 = v
	}
	if v := q.Get("ech"); v != "" {
		wireCfg.ECH = v
	}
	if v := q.Get("fp"); v != "" {
		wireCfg.Fingerprint = v
	}

	return Config{
		HomeUDID:     udid,
		PeerSecret:   secret,
		Pin:          q.Get("pin"),
		SignalServer: signalServer,
		WireCfg:      wireCfg,
	}, nil
}

// BuildURL constructs a k2p:// URL that round-trips with ParseURL.
//
// Parameters:
//   - udid: home peer UDID
//   - secret: peer secret (password in URL)
//   - signalDomain: signal server base domain (without UDID prefix)
//   - port: signal server port
//   - pin: optional certificate pin (set "" to omit)
//   - extra: additional query parameters (may be nil)
func BuildURL(udid, secret, signalDomain string, port int, pin string, extra url.Values) string {
	host := fmt.Sprintf("%s.%s:%d", udid, signalDomain, port)

	u := &url.URL{
		Scheme: "k2p",
		User:   url.UserPassword(udid, secret),
		Host:   host,
	}

	q := url.Values{}
	if pin != "" {
		q.Set("pin", pin)
	}
	// Merge extra params (pin in extra overrides the explicit pin arg).
	for k, vals := range extra {
		for _, v := range vals {
			q.Set(k, v)
		}
	}
	if len(q) > 0 {
		u.RawQuery = q.Encode()
	}

	return u.String()
}
