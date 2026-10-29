package rule

import (
	"fmt"
	"strconv"
	"strings"
)

// PortRange is an inclusive destination-port range.
// A single port is represented as Lo == Hi.
type PortRange struct {
	Lo, Hi uint16
}

// Contains reports whether p falls in the range. Port 0 never matches:
// ConnMeta.Port uses 0 for "unknown", so treating it as a real port would
// make every range match a connection whose port was never populated.
func (r PortRange) Contains(p uint16) bool {
	if p == 0 {
		return false
	}
	return p >= r.Lo && p <= r.Hi
}

// ParsePortRange parses "443" or "27000-28000" into a PortRange.
func ParsePortRange(s string) (PortRange, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return PortRange{}, fmt.Errorf("rule: empty port spec")
	}
	lo, hi, found := strings.Cut(s, "-")
	loP, err := parsePort(lo)
	if err != nil {
		return PortRange{}, err
	}
	if !found {
		return PortRange{Lo: loP, Hi: loP}, nil
	}
	hiP, err := parsePort(hi)
	if err != nil {
		return PortRange{}, err
	}
	if hiP < loP {
		return PortRange{}, fmt.Errorf("rule: port range %q has hi < lo", s)
	}
	return PortRange{Lo: loP, Hi: hiP}, nil
}

func parsePort(s string) (uint16, error) {
	v, err := strconv.ParseUint(strings.TrimSpace(s), 10, 32)
	if err != nil {
		return 0, fmt.Errorf("rule: bad port %q: %w", s, err)
	}
	if v < 1 || v > 65535 {
		return 0, fmt.Errorf("rule: port %d out of range 1-65535", v)
	}
	return uint16(v), nil
}
