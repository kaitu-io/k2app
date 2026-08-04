package rule

import (
	"reflect"
	"testing"
)

// ---------------------------------------------------------------------------
// Structural guard for the criteria predicate family.
//
// WHY THIS EXISTS
//
// Every "does this route have criteria?" predicate hand-writes its own field
// list, and nothing ties those lists to the struct they describe. Adding one
// match dimension means remembering to touch 5-7 separate enumerations:
//
//	MatchConfig.HasCriteria()      (rule/target.go)
//	MatchConfig.HasHostCriteria()  (rule/target.go)
//	RouteEntry.hasHostCriteria()   (rule/engine.go)
//	RouteEntry.hasMetaCriteria()   (rule/engine.go)
//	RouteEntry.hasProcessCriteria()(rule/engine.go)
//	hasAnyCriteria                 (local var in engine/engine.go buildRouteEntries)
//	hasAnyMeta                     (local var in rule/engine.go matchConnLocked)
//
// Every miss so far has failed SILENTLY:
//   - missed in hasAnyCriteria     → a route whose only condition is the new
//     dimension compiles fine, then gets dropped by the append gate. It
//     vanishes from the routing table with no error and no log line.
//   - missed in HasCriteria        → the last route is misread as
//     criteria-less and promoted to fallback, sending every unmatched
//     connection out the wrong outbound.
//   - missed in hasMetaCriteria    → matchConnLocked treats the route as
//     having nothing to check, so it matches connections it should reject.
//
// WHAT THIS GUARD CHECKS
//
// For each field of RouteEntry / MatchConfig it builds a value where ONLY
// that field is non-zero and asserts the field is visible to the relevant
// predicate. Fields that are not match dimensions are listed in an explicit
// exemption map with a written reason — that map is the checklist for the
// next person who adds a field.
//
// BLIND SPOT — READ BEFORE TRUSTING THIS TEST
//
// Reflection can only prove "no predicate sees this field". It CANNOT prove
// "this field is in the RIGHT predicate". The most expensive defect on this
// branch was exactly a misclassification, not an omission: Port/Protocol were
// counted as criteria requiring process attribution, which would have made
// every user on a port- or protocol-only ruleset pay a per-connection
// lsof-equivalent cost for nothing. Both predicates saw the field, so a
// coverage guard stays green through that bug.
//
// Misclassification is covered by a different kind of test — one per
// predicate, asserting its SEMANTIC purpose rather than its field list:
//   - hasProcessCriteria → engine/engine_test.go's
//     TestGateProcessSearcher_PortProtocolOnly_DropsProcessSearcher (negative)
//     and TestGateProcessSearcher_ProcessRule_KeepsProcessSearcher (positive).
//     Those pin what the predicate is FOR: gating ProcessSearcher.
//   - hasHostCriteria vs hasMetaCriteria → the DNS-time/connection-time split
//     in engine_test.go's Match/MatchConn tests.
//
// The one misclassification this file DOES catch is the subset invariant
// below: hasProcessCriteria must never see a field hasMetaCriteria misses.
//
// Also out of reach: the two predicates that are local variables rather than
// methods (hasAnyCriteria in engine/engine.go, hasAnyMeta in matchConnLocked)
// cannot be reflected over. They are pinned by example instead —
// TestBuildRouteEntries_PortOnly / _ProtocolOnly in engine/engine_test.go.
// ---------------------------------------------------------------------------

// routeEntryExempt lists RouteEntry fields that are deliberately NOT match
// criteria. Each entry must say why — an unexplained exemption is how a real
// match dimension gets waved through.
var routeEntryExempt = map[string]string{
	"Target": "the routing DECISION (outbound index) this entry yields on a match, " +
		"not an input to matching. Every entry has one, including catch-alls.",
}

// matchConfigExempt is the same list for the config-side struct. It is empty:
// every MatchConfig field is a match criterion by construction — the struct
// exists solely to express `match:` in the route config.
var matchConfigExempt = map[string]string{}

// TestCriteriaCoverage_RouteEntryFields asserts every RouteEntry field is
// visible to at least one of hasHostCriteria/hasMetaCriteria, and that
// hasProcessCriteria stays a subset of hasMetaCriteria.
func TestCriteriaCoverage_RouteEntryFields(t *testing.T) {
	assertExemptionsAreRealFields(t, reflect.TypeOf(RouteEntry{}), routeEntryExempt)

	// Negative control: a zero RouteEntry must satisfy NEITHER predicate.
	// Without this, a predicate that always returned true would make every
	// per-field assertion below pass vacuously.
	var zero RouteEntry
	if zero.hasHostCriteria() || zero.hasMetaCriteria() || zero.hasProcessCriteria() {
		t.Fatalf("zero RouteEntry must have no criteria, got host=%v meta=%v process=%v",
			zero.hasHostCriteria(), zero.hasMetaCriteria(), zero.hasProcessCriteria())
	}

	rt := reflect.TypeOf(RouteEntry{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if reason, ok := routeEntryExempt[f.Name]; ok {
			t.Logf("exempt: RouteEntry.%s — %s", f.Name, reason)
			continue
		}

		t.Run(f.Name, func(t *testing.T) {
			v := reflect.New(rt).Elem()
			setNonZero(t, v.Field(i), "RouteEntry."+f.Name)
			entry := v.Interface().(RouteEntry)

			host := entry.hasHostCriteria()
			meta := entry.hasMetaCriteria()
			if !host && !meta {
				t.Errorf("RouteEntry.%s is set but neither hasHostCriteria() nor hasMetaCriteria() sees it.\n"+
					"A route whose only condition is this field will be silently dropped by "+
					"buildRouteEntries' append gate, or silently matched by matchConnLocked without "+
					"the condition being checked. Add the field to the right predicate in "+
					"rule/engine.go, or — if it is not a match dimension — to routeEntryExempt "+
					"with a reason.", f.Name)
			}

			// Subset invariant: hasProcessCriteria is the narrow
			// "needs process attribution" gate. Anything it counts must
			// also be counted by hasMetaCriteria, or a route with only
			// that field would spin up the ProcessSearcher and then never
			// be checked at match time.
			if entry.hasProcessCriteria() && !meta {
				t.Errorf("RouteEntry.%s is seen by hasProcessCriteria() but not hasMetaCriteria() — "+
					"hasProcessCriteria must stay a strict subset of hasMetaCriteria", f.Name)
			}
		})
	}
}

// TestCriteriaCoverage_MatchConfigFields asserts every MatchConfig field is
// visible to HasCriteria. Missing one silently promotes the last route to
// fallback (see the header comment).
func TestCriteriaCoverage_MatchConfigFields(t *testing.T) {
	assertExemptionsAreRealFields(t, reflect.TypeOf(MatchConfig{}), matchConfigExempt)

	var zero MatchConfig
	if zero.HasCriteria() || zero.HasHostCriteria() {
		t.Fatalf("zero MatchConfig must have no criteria, got HasCriteria=%v HasHostCriteria=%v",
			zero.HasCriteria(), zero.HasHostCriteria())
	}

	rt := reflect.TypeOf(MatchConfig{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if reason, ok := matchConfigExempt[f.Name]; ok {
			t.Logf("exempt: MatchConfig.%s — %s", f.Name, reason)
			continue
		}

		t.Run(f.Name, func(t *testing.T) {
			v := reflect.New(rt).Elem()
			setNonZero(t, v.Field(i), "MatchConfig."+f.Name)
			cfg := v.Interface().(MatchConfig)

			if !cfg.HasCriteria() {
				t.Errorf("MatchConfig.%s is set but HasCriteria() does not see it.\n"+
					"A LAST route carrying only this field is then read as criteria-less and "+
					"promoted to the fallback, sending every unmatched connection out that "+
					"route's outbound. Add the field to HasCriteria in rule/target.go.", f.Name)
			}

			// HasHostCriteria is the DNS-time subset — it must stay a
			// subset of HasCriteria. (Which fields belong in it is a
			// semantic question reflection cannot answer; see the blind
			// spot note in the header.)
			if cfg.HasHostCriteria() && !cfg.HasCriteria() {
				t.Errorf("MatchConfig.%s is seen by HasHostCriteria() but not HasCriteria() — "+
					"HasHostCriteria must stay a strict subset of HasCriteria", f.Name)
			}
		})
	}
}

// setNonZero writes a distinguishable non-zero value into v.
//
// The default branch fails rather than skipping: a field of an unhandled kind
// is exactly the case where silently doing nothing would hand back a green
// test for an unchecked field.
func setNonZero(t *testing.T, v reflect.Value, name string) {
	t.Helper()
	switch v.Kind() {
	case reflect.Slice:
		// One element is enough — every predicate tests these with len() > 0.
		// The element's own value is irrelevant (and for []krs.Matcher there
		// is no usable zero element anyway).
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
	case reflect.String:
		v.SetString("nonzero")
	case reflect.Bool:
		v.SetBool(true)
	default:
		t.Fatalf("%s: unhandled kind %v — teach setNonZero how to populate it "+
			"instead of letting this field go unchecked", name, v.Kind())
	}
}

// assertExemptionsAreRealFields keeps the exemption maps from rotting: an
// entry naming a field that no longer exists means the map was not revisited
// when the struct changed, and a renamed field would slip through unchecked
// under its old exemption.
func assertExemptionsAreRealFields(t *testing.T, rt reflect.Type, exempt map[string]string) {
	t.Helper()
	for name, reason := range exempt {
		if _, ok := rt.FieldByName(name); !ok {
			t.Errorf("%s exemption names %q, which is not a field of the struct — "+
				"stale exemption, remove it", rt.Name(), name)
		}
		if reason == "" {
			t.Errorf("%s.%s is exempt with no reason — every exemption must say why "+
				"the field is not a match dimension", rt.Name(), name)
		}
	}
}
