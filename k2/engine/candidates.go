package engine

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/sasha-s/go-deadlock"
)

// ErrCandidatesExhausted is returned when every candidate in the failing
// URL's group is within its exclusion window.
var ErrCandidatesExhausted = errors.New("engine: outbound candidates exhausted")

// ErrNoCandidates is returned when no candidate source knows the failing
// URL — manual mode, or a route with no configured fallbacks.
var ErrNoCandidates = errors.New("engine: no outbound candidates")

// candidateExcludeTTL is how long a failed candidate stays excluded before
// it becomes eligible again.
const candidateExcludeTTL = 10 * time.Minute

// URLOwner is an optional capability of an OutboundProvider: it reports
// whether the provider knows the given URL (i.e. NextURL for it is
// meaningful). subscription.Manager implements it; compositeProvider uses
// it to decide whether to delegate to the shell provider.
type URLOwner interface {
	OwnsURL(url string) bool
}

// staticProvider rotates through ClientConfig.Fallbacks groups. A group is
// a primary via plus its ordered fallbacks; try order is primary first,
// then fallbacks. Failed URLs are excluded for candidateExcludeTTL.
//
// Only k2v5:// fallback entries are kept, because mobile appext and direct
// engine.Start never run config.ValidateClient.
type staticProvider struct {
	groups [][]string // immutable after construction; [0] = primary
	now    func() time.Time

	mu       deadlock.Mutex
	excluded map[string]time.Time // url → time it was excluded
}

func newStaticProvider(fallbacks map[string][]string, now func() time.Time) *staticProvider {
	if now == nil {
		now = time.Now
	}
	primaries := make([]string, 0, len(fallbacks))
	for p := range fallbacks {
		primaries = append(primaries, p)
	}
	sort.Strings(primaries) // deterministic group lookup when a URL is in several groups
	groups := make([][]string, 0, len(primaries))
	for _, p := range primaries {
		if p == "" {
			continue
		}
		group := []string{p}
		for _, u := range fallbacks[p] {
			if strings.HasPrefix(u, "k2v5://") && u != p {
				group = append(group, u)
			}
		}
		if len(group) < 2 {
			continue // no usable fallbacks
		}
		groups = append(groups, group)
	}
	return &staticProvider{groups: groups, now: now, excluded: make(map[string]time.Time)}
}

// groupOf returns the group whose primary is url, else the first group
// listing url as a fallback, else nil.
func (s *staticProvider) groupOf(url string) []string {
	for _, g := range s.groups {
		if g[0] == url {
			return g
		}
	}
	for _, g := range s.groups {
		for _, u := range g[1:] {
			if u == url {
				return g
			}
		}
	}
	return nil
}

// OwnsURL implements URLOwner.
func (s *staticProvider) OwnsURL(url string) bool {
	return s.groupOf(url) != nil
}

// NextURL implements OutboundProvider: excludes current, then returns the
// first group member that is neither current nor within its exclusion window.
func (s *staticProvider) NextURL(current string, reason error) (string, error) {
	group := s.groupOf(current)
	if group == nil {
		return "", ErrNoCandidates
	}
	if u := s.excludeAndPick(group, current, s.now()); u != "" {
		return u, nil
	}
	return "", ErrCandidatesExhausted
}

// excludeAndPick records current as excluded at now and returns the first
// eligible group member, or "" when none is. Fields only under mu.
func (s *staticProvider) excludeAndPick(group []string, current string, now time.Time) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.excluded[current] = now
	for _, u := range group {
		if u == current {
			continue
		}
		if at, ok := s.excluded[u]; ok && now.Sub(at) < candidateExcludeTTL {
			continue
		}
		return u
	}
	return ""
}

// Reset clears all exclusions.
func (s *staticProvider) Reset() {
	s.mu.Lock()
	s.excluded = make(map[string]time.Time)
	s.mu.Unlock()
}

// compositeProvider routes NextURL to whichever source owns the failing URL:
// the static fallback list first, then the shell provider (e.g. the desktop
// subscription manager) when it implements URLOwner and claims the URL.
type compositeProvider struct {
	static *staticProvider
	shell  OutboundProvider
}

func newCompositeProvider(static *staticProvider, shell OutboundProvider) *compositeProvider {
	return &compositeProvider{static: static, shell: shell}
}

// NextURL implements OutboundProvider.
func (c *compositeProvider) NextURL(current string, reason error) (string, error) {
	if c.static != nil && c.static.OwnsURL(current) {
		return c.static.NextURL(current, reason)
	}
	if owner, ok := c.shell.(URLOwner); ok && owner.OwnsURL(current) {
		return c.shell.NextURL(current, reason)
	}
	return "", ErrNoCandidates
}

// Reset clears the exclusions of the whole candidate set: the static
// provider's and, when it can (interface{ Reset() } — subscription.Manager
// does), the shell provider's. Callers Reset when the exclusions may be
// innocent (a Center outage, a network that died mid-failover); resetting
// only the static half left a k2subs session with every tunnel excluded
// (review 3 I-1). Ownership (OwnsURL) is untouched.
func (c *compositeProvider) Reset() {
	if c.static != nil {
		c.static.Reset()
	}
	if r, ok := c.shell.(interface{ Reset() }); ok {
		r.Reset()
	}
}
