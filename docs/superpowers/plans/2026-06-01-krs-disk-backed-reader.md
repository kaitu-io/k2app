# krs Disk-Backed Reader (k2-rules) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the krs library a read-only `mmap` reader (`krs.Open`) whose loaded-bundle resident dirty heap is `O(set count)`, not `O(rule count)`, plus the additive on-disk index the reader needs.

**Architecture:** The `.krs` format gains two **additive** index sections (TypeIDs `0x0014`/`0x0015`) holding a per-set directory + `u32` domain offsets. The producer (`WriteBundle`) emits them; the legacy `ReadBundle` ignores them. A new `Open(path)` mmaps the file and answers `Match*` by binary-searching **on the mapped bytes** — domains via the offset index, IP ranges in place (fixed-width). Matching is hoisted to operate on pre-reversed parent suffixes so the consumer pays IDNA + reversal once per lookup.

**Tech Stack:** Go, `golang.org/x/sys/windows` (Windows mmap), `syscall` (unix mmap), `golang.org/x/net/idna` (already a dep).

**Repo:** `/Users/david/projects/kaitu-io/k2-rules` (its own git repo, branch `master`). This plan touches only `k2-rules`; the k2 consumer is a separate plan.

**Governing spec / constitution:** `k2app/docs/superpowers/specs/2026-06-01-krs-disk-backed-memory-constitution-design.md`. Re-read its "Constitution", "Wire Format Extension", and "mmap Match Algorithm" sections before starting.

---

## File Structure

| File | Responsibility |
|---|---|
| `krs/CONSTITUTION.md` (new) | The binding memory constitution, verbatim from the spec |
| `krs/writer.go` (modify) | New TypeIDs; emit domain index sections; `encodeDomainSection` returns payload+index |
| `krs/reader.go` (modify) | Skip the new index TypeIDs silently in `decodeSection` |
| `krs/domain.go` (modify) | `ReversedParents` (normalize-once helper); `matchReversed`; refactor `MatchDomain` onto it |
| `krs/match.go` (modify) | `Matcher` interface; `*NamedSet` implements `MatchDomainReversed` |
| `krs/mmap_unix.go` (new, `//go:build !windows`) | `mmapReadOnly(path) ([]byte, closeFn, err)` |
| `krs/mmap_windows.go` (new, `//go:build windows`) | Windows `mmapReadOnly` via `CreateFileMapping`/`MapViewOfFile` |
| `krs/open.go` (new) | `DiskBundle`, `Open`, `Close`, `parse`, `diskSet`, mmap match, `cmpBS` |
| `krs/open_test.go` (new) | Open/Close, parity vs `ReadBundle`, bounds errors |
| `krs/constitution_test.go` (new) | Heap-slope invariant + allocs gate |

---

## Task 1: Constitution document

**Files:**
- Create: `krs/CONSTITUTION.md`

- [ ] **Step 1: Write the constitution file**

```markdown
# krs Memory Constitution (binding)

The runtime client path (iOS NE, Android Service, desktop daemon) accesses rule
bundles **only** via `krs.Open` (read-only mmap). The full-expand entry points
`Load` / `LoadNamed` / `ReadBundle` are for the producer pipeline, tooling, and
tests **only** — never the client runtime.

1. **On disk only.** Rule corpus AND all indexes live on disk; runtime access is
   exclusively read-only `mmap` (clean / file-backed pages — reclaimable under
   iOS jetsam `phys_footprint`).
2. **Heap invariant (CI-gated).** A loaded bundle's resident dirty heap is
   `O(set count)`, never `O(rule count)`. Budgets: marginal dirty heap per loaded
   region `< 8 KB`; total rule-attributable dirty heap for any config `< 64 KB`.
3. **Load only what is referenced** (config `match.region` ∪ `overseas`). Never
   the whole corpus.
4. **Never touch the whole mapping** on the constrained path: no full scan,
   `canonicalize`, re-sort, or checksum of a mapped bundle. Trust the producer;
   validate structure at publish time.
5. **Hot path O(1).** Normalize/reverse the query once at the consumer boundary;
   matching allocates a small constant per lookup, never per-set, never per-rule.

Prohibited on the client runtime path: calling `Load`/`LoadNamed`/`ReadBundle`;
holding any `[]string` domain table or `[][]byte` IP table resident; loading an
unreferenced region; silently falling back to full-expand when an index is
missing/corrupt (error instead); pre-faulting the whole mapping.

Rationale: the iOS NE has a hard 50 MB `ActiveHard` jetsam ceiling. Read-only
mmap converts rule data from counted dirty heap into reclaimable clean
file-backed pages — the only way to fit a growing corpus in a fixed budget.
```

- [ ] **Step 2: Commit**

```bash
cd /Users/david/projects/kaitu-io/k2-rules
git add krs/CONSTITUTION.md
git commit -m "docs(krs): add binding memory constitution"
```

---

## Task 2: On-disk domain index sections (writer)

**Files:**
- Modify: `krs/writer.go` (TypeIDs near line 19-25; `encodeDomainBySet` → `encodeDomainSection`; `collectSections` near line 96-101)
- Modify: `krs/reader.go` (`decodeSection` switch near line 100-103)
- Test: `krs/writer_test.go`

- [ ] **Step 1: Write the failing test**

Add to `krs/writer_test.go`:

```go
func TestDomainIndexSection_OffsetsPointAtEntries(t *testing.T) {
	b := &Bundle{Sets: []NamedSet{
		{Name: "a", DomainSuffixes: []string{"google.com", "github.com"}},
		{Name: "b", DomainSuffixes: []string{"example.org"}},
	}}
	var buf bytes.Buffer
	if err := WriteBundle(&buf, b); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	// locate the two sections by TypeID via the section index
	payload := sectionPayload(t, data, typeDomainSuffixBySet)
	index := sectionPayload(t, data, typeDomainSuffixIndex)

	setCount := binary.LittleEndian.Uint16(index[0:2])
	if setCount != 2 {
		t.Fatalf("setCount=%d want 2", setCount)
	}
	// directory: 2 sets × {u32 start, u32 count}
	dir := index[2 : 2+int(setCount)*8]
	offsets := index[2+int(setCount)*8:]
	total := 0
	for s := 0; s < int(setCount); s++ {
		cnt := binary.LittleEndian.Uint32(dir[s*8+4:])
		total += int(cnt)
	}
	if total*4 != len(offsets) {
		t.Fatalf("offset table len=%d want %d", len(offsets), total*4)
	}
	// every offset must decode to a valid (set_idx, len, value) entry
	for k := 0; k < total; k++ {
		off := binary.LittleEndian.Uint32(offsets[k*4:])
		if int(off) >= len(payload) {
			t.Fatalf("offset[%d]=%d out of payload (%d)", k, off, len(payload))
		}
		p := payload[off+2:] // skip set_idx
		l, m := binary.Uvarint(p)
		if m <= 0 || int(l) > len(p)-m {
			t.Fatalf("offset[%d] does not point at a valid entry", k)
		}
	}
}

// sectionPayload returns the payload bytes of the first section with typeID.
func sectionPayload(t *testing.T, data []byte, typeID uint16) []byte {
	t.Helper()
	cnt := int(binary.LittleEndian.Uint16(data[6:8]))
	for i := 0; i < cnt; i++ {
		e := data[8+i*10:]
		if binary.LittleEndian.Uint16(e[0:2]) == typeID {
			off := binary.LittleEndian.Uint32(e[2:6])
			ln := binary.LittleEndian.Uint32(e[6:10])
			return data[off : off+ln]
		}
	}
	t.Fatalf("section 0x%04x not found", typeID)
	return nil
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd /Users/david/projects/kaitu-io/k2-rules && go test ./krs/ -run TestDomainIndexSection -v`
Expected: FAIL — `undefined: typeDomainSuffixIndex`.

- [ ] **Step 3: Add the TypeIDs and the combined encoder**

In `krs/writer.go`, add to the TypeID `const` block (after `typeDomainExcludeBySet`):

```go
	typeDomainSuffixIndex  uint16 = 0x0014 // offset index for 0x0012
	typeDomainExcludeIndex uint16 = 0x0015 // offset index for 0x0013
```

Replace `encodeDomainBySet` with `encodeDomainSection` (returns payload AND index):

```go
// encodeDomainSection serializes domains from all sets into a payload plus its
// offset index. Payload entries: [u16 set_idx][uvarint len][reversed-lower
// utf-8], sorted (set_idx ASC, value ASC), per-set dedup. Index layout:
// [u16 setCount][{u32 entryStart, u32 entryCount} × setCount][u32 offset × N],
// offsets relative to payload start, same order as the payload.
//
// exclude=true reads from NamedSet.ExcludeDomains instead of DomainSuffixes.
func encodeDomainSection(sets []NamedSet, exclude bool) (payload, index []byte) {
	type entry struct {
		setIdx uint16
		value  string // reversed-lower
	}
	var entries []entry
	for i, s := range sets {
		src := s.DomainSuffixes
		if exclude {
			src = s.ExcludeDomains
		}
		seen := make(map[string]struct{}, len(src))
		for _, d := range src {
			ascii, ok := toASCIIDomain(d)
			if !ok {
				if strings.TrimSpace(d) != "" {
					slog.Warn("krs: dropping non-IDNA-normalizable domain entry",
						"set", s.Name, "entry", d)
				}
				continue
			}
			r := reverseASCII(ascii)
			if _, dup := seen[r]; dup {
				continue
			}
			seen[r] = struct{}{}
			entries = append(entries, entry{uint16(i), r})
		}
	}
	if len(entries) == 0 {
		return nil, nil
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].setIdx != entries[j].setIdx {
			return entries[i].setIdx < entries[j].setIdx
		}
		return entries[i].value < entries[j].value
	})

	counts := make([]uint32, len(sets))
	offsets := make([]uint32, len(entries))
	var pbuf bytes.Buffer
	var idxb [2]byte
	var vb [binary.MaxVarintLen64]byte
	for k, e := range entries {
		offsets[k] = uint32(pbuf.Len())
		binary.LittleEndian.PutUint16(idxb[:], e.setIdx)
		pbuf.Write(idxb[:])
		n := binary.PutUvarint(vb[:], uint64(len(e.value)))
		pbuf.Write(vb[:n])
		pbuf.WriteString(e.value)
		counts[e.setIdx]++
	}

	var ibuf bytes.Buffer
	var u16b [2]byte
	var u32b [4]byte
	binary.LittleEndian.PutUint16(u16b[:], uint16(len(sets)))
	ibuf.Write(u16b[:])
	var start uint32
	for _, c := range counts {
		binary.LittleEndian.PutUint32(u32b[:], start)
		ibuf.Write(u32b[:])
		binary.LittleEndian.PutUint32(u32b[:], c)
		ibuf.Write(u32b[:])
		start += c
	}
	for _, o := range offsets {
		binary.LittleEndian.PutUint32(u32b[:], o)
		ibuf.Write(u32b[:])
	}
	return pbuf.Bytes(), ibuf.Bytes()
}
```

In `collectSections`, replace the two `encodeDomainBySet` blocks (near lines 96-101) with:

```go
	if pay, idx := encodeDomainSection(b.Sets, false); len(pay) > 0 {
		out = append(out, section{typeDomainSuffixBySet, pay})
		out = append(out, section{typeDomainSuffixIndex, idx})
	}
	if pay, idx := encodeDomainSection(b.Sets, true); len(pay) > 0 {
		out = append(out, section{typeDomainExcludeBySet, pay})
		out = append(out, section{typeDomainExcludeIndex, idx})
	}
```

In `krs/reader.go` `decodeSection`, add an explicit silent-skip case (before `default`):

```go
	case typeDomainSuffixIndex, typeDomainExcludeIndex:
		return nil // index sections are consumed by Open(), ignored by ReadBundle
```

If `bytes`/`encoding/binary` aren't imported in `writer_test.go`, add them.

- [ ] **Step 4: Run to verify it passes + nothing else broke**

Run: `cd /Users/david/projects/kaitu-io/k2-rules && go test ./krs/ -run 'TestDomainIndex|TestWrite|TestRead|RoundTrip' -v`
Expected: PASS. If a pre-existing test calls `encodeDomainBySet`, update it to `encodeDomainSection` (take the first return value).

- [ ] **Step 5: Commit**

```bash
git add krs/writer.go krs/reader.go krs/writer_test.go
git commit -m "feat(krs): emit additive domain offset-index sections (0x0014/0x0015)"
```

---

## Task 3: Normalize-once match primitive (heap reader)

**Files:**
- Modify: `krs/domain.go` (add `ReversedParents`, `matchReversed`; refactor `MatchDomain`)
- Modify: `krs/match.go` (add `Matcher` interface + `NamedSet.MatchDomainReversed`)
- Test: `krs/domain_test.go`

- [ ] **Step 1: Write the failing test**

Add to `krs/domain_test.go`:

```go
func TestMatchDomainReversed_ParityWithMatchDomain(t *testing.T) {
	s := &NamedSet{
		DomainSuffixes: []string{"google.com", "example.org"},
		ExcludeDomains: []string{"safe.google.com"},
	}
	// populate read-side state the way ReadBundle would
	var buf bytes.Buffer
	if err := WriteBundle(&buf, &Bundle{Sets: []NamedSet{*s}}); err != nil {
		t.Fatal(err)
	}
	b, err := ReadBundle(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	set := &b.Sets[0]
	cases := []string{"google.com", "mail.google.com", "safe.google.com",
		"fakegoogle.com", "example.org", "nope.net"}
	for _, host := range cases {
		want := set.MatchDomain(host)
		got := set.MatchDomainReversed(ReversedParents(host))
		if got != want {
			t.Errorf("host=%q reversed=%v want %v", host, got, want)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd /Users/david/projects/kaitu-io/k2-rules && go test ./krs/ -run TestMatchDomainReversed -v`
Expected: FAIL — `undefined: ReversedParents` / `MatchDomainReversed`.

- [ ] **Step 3: Implement**

In `krs/domain.go`, add:

```go
// ReversedParents normalizes host to ASCII-LDH once (IDN→punycode, case-fold,
// strip trailing dot) and returns the reversed forms of host and each parent
// suffix, longest first. Returns nil for non-hostnames. Hoisted out of the
// per-set match path so a consumer pays IDNA + reversal once per lookup, not
// once per set (constitution rule 5).
func ReversedParents(host string) []string {
	h, ok := toASCIIDomain(host)
	if !ok {
		return nil
	}
	var out []string
	for h != "" {
		out = append(out, reverseASCII(h))
		dot := strings.IndexByte(h, '.')
		if dot < 0 {
			break
		}
		h = h[dot+1:]
	}
	return out
}

// matchReversed reports whether any pre-reversed parent suffix exactly hits
// this section's sorted table. Allocation-free.
func (s *domainSection) matchReversed(parents []string) bool {
	if len(s.reversed) == 0 {
		return false
	}
	for _, rq := range parents {
		idx := sort.SearchStrings(s.reversed, rq)
		if idx < len(s.reversed) && s.reversed[idx] == rq {
			return true
		}
	}
	return false
}
```

Refactor `MatchDomain` (and keep behavior identical) to reuse the primitive:

```go
// MatchDomain reports whether host should be routed by this set. Excludes take
// priority. Convenience wrapper — the hot path uses MatchDomainReversed with
// parents computed once by ReversedParents.
func (s *NamedSet) MatchDomain(host string) bool {
	return s.MatchDomainReversed(ReversedParents(host))
}
```

Delete the old body of `domainSection.Match` callers? No — keep `domainSection.Match(host)` if other code/tests use it, but it now can delegate:

```go
func (s *domainSection) Match(host string) bool {
	return s.matchReversed(ReversedParents(host))
}
```

In `krs/match.go`, add:

```go
import "net/netip"

// Matcher is the per-set routing-match surface shared by the heap reader
// (*NamedSet) and the mmap reader (*diskSet). Domain matching takes
// pre-reversed parent suffixes (ReversedParents) so the consumer normalizes
// once per lookup.
type Matcher interface {
	MatchDomainReversed(reversedParents []string) bool
	MatchIP(addr netip.Addr) bool
}

// MatchDomainReversed: excludes take priority over suffixes.
func (s *NamedSet) MatchDomainReversed(reversedParents []string) bool {
	if s.excludeSection.matchReversed(reversedParents) {
		return false
	}
	return s.domainSection.matchReversed(reversedParents)
}

var _ Matcher = (*NamedSet)(nil)
```

(`netip` may already be imported in `match.go`; merge imports.)

- [ ] **Step 4: Run to verify it passes**

Run: `cd /Users/david/projects/kaitu-io/k2-rules && go test ./krs/ -run 'TestMatchDomain|Parity' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add krs/domain.go krs/match.go krs/domain_test.go
git commit -m "feat(krs): ReversedParents + Matcher interface (normalize once per lookup)"
```

---

## Task 4: Cross-platform read-only mmap

**Files:**
- Create: `krs/mmap_unix.go`, `krs/mmap_windows.go`
- Test: `krs/mmap_test.go`

- [ ] **Step 1: Write the failing test**

Create `krs/mmap_test.go`:

```go
package krs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMmapReadOnly_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "blob")
	want := []byte("K2RL-mmap-test-payload")
	if err := os.WriteFile(p, want, 0o644); err != nil {
		t.Fatal(err)
	}
	data, closeFn, err := mmapReadOnly(p)
	if err != nil {
		t.Fatal(err)
	}
	defer closeFn()
	if string(data) != string(want) {
		t.Fatalf("mmap content=%q want %q", data, want)
	}
}

func TestMmapReadOnly_EmptyFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	data, closeFn, err := mmapReadOnly(p)
	if err != nil {
		t.Fatal(err)
	}
	defer closeFn()
	if len(data) != 0 {
		t.Fatalf("empty file mapped to %d bytes", len(data))
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd /Users/david/projects/kaitu-io/k2-rules && go test ./krs/ -run TestMmapReadOnly -v`
Expected: FAIL — `undefined: mmapReadOnly`.

- [ ] **Step 3: Implement both platforms**

Create `krs/mmap_unix.go`:

```go
//go:build !windows

package krs

import (
	"fmt"
	"os"
	"syscall"
)

// mmapReadOnly maps path read-only and returns the bytes plus an unmap closer.
// The file descriptor is closed immediately; the mapping persists until the
// closer runs. Empty files map to a nil slice with a no-op closer.
func mmapReadOnly(path string) (data []byte, closeFn func() error, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	n := fi.Size()
	if n == 0 {
		return nil, func() error { return nil }, nil
	}
	if n > 1<<31-1 {
		return nil, nil, fmt.Errorf("krs: file too large to mmap: %d bytes", n)
	}
	data, err = syscall.Mmap(int(f.Fd()), 0, int(n), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return nil, nil, fmt.Errorf("krs: mmap %s: %w", path, err)
	}
	return data, func() error { return syscall.Munmap(data) }, nil
}
```

Create `krs/mmap_windows.go`:

```go
//go:build windows

package krs

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func mmapReadOnly(path string) (data []byte, closeFn func() error, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	n := fi.Size()
	if n == 0 {
		return nil, func() error { return nil }, nil
	}
	h, err := windows.CreateFileMapping(windows.Handle(f.Fd()), nil,
		windows.PAGE_READONLY, 0, 0, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("krs: CreateFileMapping %s: %w", path, err)
	}
	addr, err := windows.MapViewOfFile(h, windows.FILE_MAP_READ, 0, 0, uintptr(n))
	if err != nil {
		windows.CloseHandle(h)
		return nil, nil, fmt.Errorf("krs: MapViewOfFile %s: %w", path, err)
	}
	data = unsafe.Slice((*byte)(unsafe.Pointer(addr)), n)
	return data, func() error {
		e := windows.UnmapViewOfFile(addr)
		windows.CloseHandle(h)
		return e
	}, nil
}
```

If `golang.org/x/sys` is not yet a dependency:

```bash
cd /Users/david/projects/kaitu-io/k2-rules && go get golang.org/x/sys/windows && go mod tidy
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd /Users/david/projects/kaitu-io/k2-rules && go test ./krs/ -run TestMmapReadOnly -v`
Expected: PASS (on the dev host's OS; the other platform compiles via build tags).

- [ ] **Step 5: Commit**

```bash
git add krs/mmap_unix.go krs/mmap_windows.go krs/mmap_test.go go.mod go.sum
git commit -m "feat(krs): cross-platform read-only mmap helper"
```

---

## Task 5: DiskBundle, Open, Close, parse

**Files:**
- Create: `krs/open.go`
- Test: `krs/open_test.go`

- [ ] **Step 1: Write the failing test**

Create `krs/open_test.go`:

```go
package krs

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// writeTmpBundle writes b to a temp .krs and returns its path.
func writeTmpBundle(t *testing.T, b *Bundle) string {
	t.Helper()
	var buf bytes.Buffer
	if err := WriteBundle(&buf, b); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "b.krs")
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestOpen_NamesAndClose(t *testing.T) {
	b := &Bundle{Sets: []NamedSet{
		{Name: "cn", DomainSuffixes: []string{"qq.com"}, CIDRs: []string{"1.1.1.0/24"}},
		{Name: "x", DomainSuffixes: []string{"foo.org"}},
	}}
	db, err := Open(writeTmpBundle(t, b))
	if err != nil {
		t.Fatal(err)
	}
	if got := db.SetNames(); len(got) != 2 || got[0] != "cn" || got[1] != "x" {
		t.Fatalf("SetNames=%v", got)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestOpen_BadMagic(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.krs")
	if err := os.WriteFile(p, []byte("NOPExxxx"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(p); err == nil {
		t.Fatal("expected error on bad magic")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd /Users/david/projects/kaitu-io/k2-rules && go test ./krs/ -run TestOpen -v`
Expected: FAIL — `undefined: Open`.

- [ ] **Step 3: Implement open.go (Open + parse + descriptors)**

Create `krs/open.go`:

```go
package krs

import (
	"encoding/binary"
	"fmt"
)

// DiskBundle is a read-only, mmap-backed bundle. Resident dirty heap is
// O(set count): only per-set descriptors pointing into the mmap, never the
// rules themselves. Constitution: see krs/CONSTITUTION.md.
type DiskBundle struct {
	data  []byte       // the whole mmap (clean / file-backed)
	close func() error // unmap
	names []string
	sets  []diskSet
}

// diskSet implements Matcher over mmap slices for one named set.
type diskSet struct {
	name    string
	suffix  domainBlock
	exclude domainBlock
	v4      ipBlock
	v6      ipBlock
}

// domainBlock points at one set's slice of the domain offset index plus the
// shared domain payload. Both are sub-slices of the mmap.
type domainBlock struct {
	payload []byte // whole domain section payload (mmap)
	offsets []byte // this set's u32 offsets (entryCount*4 bytes, mmap)
}

// ipBlock points at one set's contiguous run of fixed-width IP entries.
type ipBlock struct {
	payload []byte // this set's entries: [u16 set_idx][start][end] × count (mmap)
	addrLen int    // 4 or 16
}

// Open maps path read-only and builds per-set descriptors. The returned bundle
// must be Closed to unmap. Errors (not silent fallback) on bad magic, truncated
// index, out-of-bounds section, or a domain payload missing its index section.
func Open(path string) (*DiskBundle, error) {
	data, closeFn, err := mmapReadOnly(path)
	if err != nil {
		return nil, err
	}
	db := &DiskBundle{data: data, close: closeFn}
	if err := db.parse(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// Close unmaps the bundle. Safe to call once.
func (db *DiskBundle) Close() error {
	if db.close != nil {
		c := db.close
		db.close = nil
		return c()
	}
	return nil
}

// SetNames returns the set names in bundle order.
func (db *DiskBundle) SetNames() []string { return db.names }

// Sets exposes per-set matchers (pointers into db; valid until Close).
func (db *DiskBundle) Sets() []Matcher {
	out := make([]Matcher, len(db.sets))
	for i := range db.sets {
		out[i] = &db.sets[i]
	}
	return out
}

func (db *DiskBundle) parse() error {
	d := db.data
	if len(d) < headerSize {
		return fmt.Errorf("krs: data too short for header (%d)", len(d))
	}
	if string(d[0:4]) != Magic {
		return fmt.Errorf("krs: bad magic %q", string(d[0:4]))
	}
	sectionCount := int(binary.LittleEndian.Uint16(d[6:8]))
	indexEnd := headerSize + indexEntrySize*sectionCount
	if len(d) < indexEnd {
		return fmt.Errorf("krs: data too short for %d-section index", sectionCount)
	}
	// Collect section payloads (bounds-checked slices into the mmap).
	secs := map[uint16][]byte{}
	for i := 0; i < sectionCount; i++ {
		e := d[headerSize+i*indexEntrySize:]
		typeID := binary.LittleEndian.Uint16(e[0:2])
		off := binary.LittleEndian.Uint32(e[2:6])
		ln := binary.LittleEndian.Uint32(e[6:10])
		if uint64(off)+uint64(ln) > uint64(len(d)) {
			return fmt.Errorf("krs: section 0x%04x out of bounds", typeID)
		}
		secs[typeID] = d[off : off+ln]
	}
	// SetTable first.
	st, ok := secs[typeSetTable]
	if !ok {
		// No sets: an empty/app-only bundle. Nothing to match on.
		return nil
	}
	names, err := decodeSetTable(st)
	if err != nil {
		return err
	}
	db.names = names
	db.sets = make([]diskSet, len(names))
	for i := range db.sets {
		db.sets[i].name = names[i]
	}
	// Domain blocks (payload requires its index; constitution: no fallback).
	if err := db.bindDomain(secs, typeDomainSuffixBySet, typeDomainSuffixIndex, false); err != nil {
		return err
	}
	if err := db.bindDomain(secs, typeDomainExcludeBySet, typeDomainExcludeIndex, true); err != nil {
		return err
	}
	// IP blocks.
	if p, ok := secs[typeIPv4RangesBySet]; ok {
		if err := db.bindIP(p, 4); err != nil {
			return err
		}
	}
	if p, ok := secs[typeIPv6RangesBySet]; ok {
		if err := db.bindIP(p, 16); err != nil {
			return err
		}
	}
	return nil
}

// bindDomain attaches each set's offset sub-slice + the shared payload.
func (db *DiskBundle) bindDomain(secs map[uint16][]byte, payID, idxID uint16, exclude bool) error {
	payload, hasPay := secs[payID]
	index, hasIdx := secs[idxID]
	if !hasPay {
		return nil // this bundle has no such domain data
	}
	if !hasIdx {
		return fmt.Errorf("krs: domain section 0x%04x present without index 0x%04x", payID, idxID)
	}
	if len(index) < 2 {
		return fmt.Errorf("krs: domain index 0x%04x too short", idxID)
	}
	setCount := int(binary.LittleEndian.Uint16(index[0:2]))
	if setCount != len(db.sets) {
		return fmt.Errorf("krs: domain index setCount=%d != %d sets", setCount, len(db.sets))
	}
	dirEnd := 2 + setCount*8
	if len(index) < dirEnd {
		return fmt.Errorf("krs: domain index directory truncated")
	}
	offTable := index[dirEnd:]
	for s := 0; s < setCount; s++ {
		start := binary.LittleEndian.Uint32(index[2+s*8:])
		count := binary.LittleEndian.Uint32(index[2+s*8+4:])
		lo := int(start) * 4
		hi := int(start+count) * 4
		if lo > hi || hi > len(offTable) {
			return fmt.Errorf("krs: domain index set %d range out of bounds", s)
		}
		blk := domainBlock{payload: payload, offsets: offTable[lo:hi]}
		if exclude {
			db.sets[s].exclude = blk
		} else {
			db.sets[s].suffix = blk
		}
	}
	return nil
}

// bindIP splits a fixed-width IP section (sorted by set_idx) into per-set
// sub-slices via two boundary binary searches per set — no full scan.
func (db *DiskBundle) bindIP(payload []byte, addrLen int) error {
	entrySize := 2 + 2*addrLen
	if len(payload)%entrySize != 0 {
		return fmt.Errorf("krs: ipv%d section not a multiple of %d", ipFamily(addrLen), entrySize)
	}
	n := len(payload) / entrySize
	setIdxAt := func(i int) int {
		return int(binary.LittleEndian.Uint16(payload[i*entrySize:]))
	}
	// lowerBound returns the first entry index whose set_idx >= target.
	lowerBound := func(target int) int {
		lo, hi := 0, n
		for lo < hi {
			mid := (lo + hi) / 2
			if setIdxAt(mid) < target {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		return lo
	}
	for s := range db.sets {
		lo := lowerBound(s)
		hi := lowerBound(s + 1)
		if lo == hi {
			continue
		}
		blk := ipBlock{payload: payload[lo*entrySize : hi*entrySize], addrLen: addrLen}
		if addrLen == 4 {
			db.sets[s].v4 = blk
		} else {
			db.sets[s].v6 = blk
		}
	}
	return nil
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd /Users/david/projects/kaitu-io/k2-rules && go test ./krs/ -run TestOpen -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add krs/open.go krs/open_test.go
git commit -m "feat(krs): Open/DiskBundle — mmap-backed, O(sets) per-set descriptors"
```

---

## Task 6: mmap matching (diskSet implements Matcher)

**Files:**
- Modify: `krs/open.go` (add `cmpBS`, `domainBlock.matchReversed`, `diskSet.MatchDomainReversed`, `diskSet.MatchIP`)
- Test: `krs/open_test.go`

- [ ] **Step 1: Write the failing parity test**

Add to `krs/open_test.go`:

```go
import "net/netip" // add to existing import block

func TestDiskBundle_ParityWithReadBundle(t *testing.T) {
	b := &Bundle{Sets: []NamedSet{
		{
			Name:           "cn",
			DomainSuffixes: []string{"qq.com", "weixin.qq.com", "taobao.com"},
			ExcludeDomains: []string{"intl.taobao.com"},
			CIDRs:          []string{"1.2.3.0/24", "10.0.0.0/8", "2001:db8::/32"},
		},
		{Name: "os", DomainSuffixes: []string{"google.com"}, CIDRs: []string{"8.8.8.0/24"}},
	}}
	var buf bytes.Buffer
	if err := WriteBundle(&buf, b); err != nil {
		t.Fatal(err)
	}
	heap, err := ReadBundle(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	db, err := Open(writeTmpBundle(t, b))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	disk := db.Sets()

	domains := []string{"qq.com", "weixin.qq.com", "a.weixin.qq.com",
		"taobao.com", "intl.taobao.com", "deep.intl.taobao.com",
		"google.com", "evil.com", "qq.com.evil.com"}
	ips := []string{"1.2.3.4", "1.2.4.1", "10.255.0.1", "8.8.8.8", "9.9.9.9",
		"2001:db8::1", "2001:dead::1"}
	for si := range heap.Sets {
		hs := &heap.Sets[si]
		ds := disk[si]
		for _, host := range domains {
			parents := ReversedParents(host)
			if hs.MatchDomainReversed(parents) != ds.MatchDomainReversed(parents) {
				t.Errorf("set %d domain %q: heap=%v disk=%v", si, host,
					hs.MatchDomainReversed(parents), ds.MatchDomainReversed(parents))
			}
		}
		for _, ipStr := range ips {
			addr := netip.MustParseAddr(ipStr)
			if hs.MatchIP(addr) != ds.MatchIP(addr) {
				t.Errorf("set %d ip %q: heap=%v disk=%v", si, ipStr,
					hs.MatchIP(addr), ds.MatchIP(addr))
			}
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd /Users/david/projects/kaitu-io/k2-rules && go test ./krs/ -run TestDiskBundle_Parity -v`
Expected: FAIL — `*diskSet` has no `MatchDomainReversed` / `MatchIP`.

- [ ] **Step 3: Implement mmap matching**

Add to `krs/open.go`:

```go
import (
	"bytes" // add to existing imports
	"net/netip"
)

var _ Matcher = (*diskSet)(nil)

// MatchDomainReversed: excludes take priority over suffixes.
func (s *diskSet) MatchDomainReversed(reversedParents []string) bool {
	if s.exclude.matchReversed(reversedParents) {
		return false
	}
	return s.suffix.matchReversed(reversedParents)
}

// matchReversed binary-searches this set's offset table for an exact hit on any
// reversed parent suffix. Allocation-free: entry values are compared as mmap
// bytes via cmpBS without materializing strings.
func (b *domainBlock) matchReversed(parents []string) bool {
	n := len(b.offsets) / 4
	if n == 0 {
		return false
	}
	for _, rq := range parents {
		lo, hi := 0, n
		for lo < hi {
			mid := (lo + hi) / 2
			if cmpBS(b.entryBytes(mid), rq) < 0 {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		if lo < n && cmpBS(b.entryBytes(lo), rq) == 0 {
			return true
		}
	}
	return false
}

// entryBytes returns the reversed-domain value bytes of the k-th entry as a
// slice into the mmap (no allocation).
func (b *domainBlock) entryBytes(k int) []byte {
	off := binary.LittleEndian.Uint32(b.offsets[k*4:])
	p := b.payload[off+2:] // skip u16 set_idx
	l, m := binary.Uvarint(p)
	return p[m : m+int(l)]
}

// cmpBS lexicographically compares a byte slice with a string, no allocation.
func cmpBS(b []byte, s string) int {
	n := len(b)
	if len(s) < n {
		n = len(s)
	}
	for i := 0; i < n; i++ {
		if b[i] != s[i] {
			if b[i] < s[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(b) < len(s):
		return -1
	case len(b) > len(s):
		return 1
	default:
		return 0
	}
}

// MatchIP mirrors NamedSet.MatchIP semantics (incl. 4-in-6) over mmap ranges.
func (s *diskSet) MatchIP(addr netip.Addr) bool {
	if addr.Is4() {
		b := addr.As4()
		return s.v4.contains(b[:])
	}
	if addr.Is6() {
		if addr.Is4In6() {
			b4 := addr.Unmap().As4()
			if s.v4.contains(b4[:]) {
				return true
			}
		}
		b := addr.As16()
		return s.v6.contains(b[:])
	}
	return false
}

// contains binary-searches the fixed-width sorted ranges in place on the mmap.
func (blk *ipBlock) contains(raw []byte) bool {
	if blk.addrLen == 0 || len(raw) != blk.addrLen {
		return false
	}
	entrySize := 2 + 2*blk.addrLen
	n := len(blk.payload) / entrySize
	startAt := func(i int) []byte {
		o := i * entrySize
		return blk.payload[o+2 : o+2+blk.addrLen]
	}
	endAt := func(i int) []byte {
		o := i * entrySize
		return blk.payload[o+2+blk.addrLen : o+entrySize]
	}
	// largest start <= raw
	idx := 0
	lo, hi := 0, n
	for lo < hi {
		mid := (lo + hi) / 2
		if bytes.Compare(startAt(mid), raw) > 0 {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	idx = lo
	if idx == 0 {
		return false
	}
	return bytes.Compare(endAt(idx-1), raw) >= 0
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd /Users/david/projects/kaitu-io/k2-rules && go test ./krs/ -run TestDiskBundle_Parity -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add krs/open.go krs/open_test.go
git commit -m "feat(krs): mmap matching — diskSet implements Matcher (alloc-free domain+IP)"
```

---

## Task 7: Constitution gates (heap slope + allocs)

**Files:**
- Create: `krs/constitution_test.go`

- [ ] **Step 1: Write the failing tests**

Create `krs/constitution_test.go`:

```go
package krs

import (
	"fmt"
	"runtime"
	"testing"
)

// bigBundle builds a bundle with nDomains synthetic domains + nCIDRs in one set.
func bigBundle(nDomains, nCIDRs int) *Bundle {
	s := NamedSet{Name: "cn"}
	for i := 0; i < nDomains; i++ {
		s.DomainSuffixes = append(s.DomainSuffixes, fmt.Sprintf("d%d-host.example", i))
	}
	for i := 0; i < nCIDRs; i++ {
		s.CIDRs = append(s.CIDRs, fmt.Sprintf("10.%d.%d.0/24", i/256, i%256))
	}
	return &Bundle{Sets: []NamedSet{s}}
}

// TestConstitution_HeapSlopeFlat asserts that Open's resident heap does NOT
// scale with rule count: a 10x larger bundle must not add >8KB of heap.
func TestConstitution_HeapSlopeFlat(t *testing.T) {
	small := writeTmpBundle(t, bigBundle(1_000, 200))
	large := writeTmpBundle(t, bigBundle(50_000, 5_000))

	heapAfterOpen := func(path string) uint64 {
		runtime.GC()
		var a, b runtime.MemStats
		runtime.ReadMemStats(&a)
		db, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		// touch one lookup so lazy work (if any) is realized
		_ = db.Sets()[0].MatchDomainReversed(ReversedParents("d1-host.example"))
		runtime.GC()
		runtime.ReadMemStats(&b)
		keep := db // keep mapping alive across the measurement
		_ = keep
		h := b.HeapInuse - a.HeapInuse
		db.Close()
		return h
	}
	hSmall := heapAfterOpen(small)
	hLarge := heapAfterOpen(large)

	// 50x more rules must not add >8KB heap (constitution budget per region).
	if hLarge > hSmall+8*1024 {
		t.Fatalf("heap scales with rules: small=%dB large=%dB (delta %dB > 8KB)",
			hSmall, hLarge, hLarge-hSmall)
	}
}

// TestConstitution_MatchAllocs asserts the hot path is allocation-free.
func TestConstitution_MatchAllocs(t *testing.T) {
	db, err := Open(writeTmpBundle(t, bigBundle(20_000, 2_000)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	set := db.Sets()[0]
	parents := ReversedParents("d12345-host.example") // computed once, outside
	allocs := testing.AllocsPerRun(200, func() {
		_ = set.MatchDomainReversed(parents)
	})
	if allocs > 0 {
		t.Fatalf("MatchDomainReversed allocates %.1f/op, want 0", allocs)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd /Users/david/projects/kaitu-io/k2-rules && go test ./krs/ -run TestConstitution -v`
Expected: PASS if Tasks 5-6 are correct. If `TestConstitution_MatchAllocs` FAILS (allocs > 0), the binary search is materializing strings — verify `cmpBS`/`entryBytes` are used (no `string(...)` in the match path). If `TestConstitution_HeapSlopeFlat` FAILS, a per-rule structure leaked into `parse` — audit `bindDomain`/`bindIP` for slices copied out of the mmap.

(This task's tests are the guard; they may pass immediately — that is success, not a TDD violation: the behavior they assert was implemented in Tasks 5-6 and these tests lock it.)

- [ ] **Step 3: (only if a test failed) fix the leak**

No new code if green. If red, remove the offending allocation/copy and re-run.

- [ ] **Step 4: Run the full package to confirm green**

Run: `cd /Users/david/projects/kaitu-io/k2-rules && go test ./krs/ -count=1`
Expected: ok.

- [ ] **Step 5: Commit**

```bash
git add krs/constitution_test.go
git commit -m "test(krs): constitution gates — heap slope flat + alloc-free match"
```

---

## Task 8: Version bump + publish readiness

**Files:**
- Modify: `krs/format.go` (`Version` const)
- Verify: producer (`main.go` / `krs_pipeline.go`) emits via `WriteBundle` (already does)

- [ ] **Step 1: Bump the informational version**

In `krs/format.go`, change:

```go
const Version uint16 = 2
```

(Readers do not reject on version; this records that index sections are present.)

- [ ] **Step 2: Verify the producer round-trips with the index**

Run: `cd /Users/david/projects/kaitu-io/k2-rules && go run . -o /tmp/krsout/ 2>&1 | tail -5`
Then verify one bundle opens:

```bash
cd /Users/david/projects/kaitu-io/k2-rules && cat > /tmp/openchk_test.go <<'EOF'
package krs
import "testing"
func TestOpenProduced(t *testing.T){ db,err:=Open("/tmp/krsout/cn.krs"); if err!=nil{t.Fatal(err)}; defer db.Close(); if len(db.SetNames())==0{t.Fatal("no sets")} }
EOF
cp /tmp/openchk_test.go krs/zz_openchk_test.go
go test ./krs/ -run TestOpenProduced -v
rm krs/zz_openchk_test.go
```
Expected: PASS — the freshly produced `cn.krs` opens via mmap.

- [ ] **Step 3: Full test + vet**

Run: `cd /Users/david/projects/kaitu-io/k2-rules && go vet ./... && go test ./... -count=1`
Expected: ok.

- [ ] **Step 4: Commit + tag**

```bash
git add krs/format.go
git commit -m "feat(krs): bump format Version to 2 (index sections present)"
git tag v0.2.0
```

- [ ] **Step 5: Note for the publish pipeline**

The CI that builds `all.krs.tar.gz` must run on this tag so published bundles
carry the index sections. The k2 consumer plan re-fetches the embedded blob
(`make fetch-rules-embed`) against this version. Do **not** mark this plan done
until `go test ./...` is green and `v0.2.0` is tagged.

---

## Self-Review

- **Spec coverage:** constitution doc (T1), additive index TypeIDs + layout (T2),
  ReversedParents / normalize-once (T3), cross-platform mmap (T4), Open/Close +
  O(sets) descriptors + no-silent-fallback (T5), alloc-free mmap match incl.
  4-in-6 (T6), heap-slope + allocs gates (T7), version + publish readiness (T8).
  IP needs no on-disk index — covered by in-place search (T6). Region-scoping,
  `e.tmp`, engine `Close`, IDNA-hoist *wiring*, embed re-fetch, and the CLAUDE.md
  reference are **consumer-side** and belong to the separate k2 plan.
- **Placeholders:** none — every code step shows full code; commands have
  expected output.
- **Type consistency:** `Matcher.MatchDomainReversed(reversedParents []string)`,
  `MatchIP(netip.Addr)`, `ReversedParents(host) []string`, `domainBlock`,
  `ipBlock`, `diskSet`, `DiskBundle.Sets() []Matcher`, `cmpBS([]byte,string) int`
  are used identically across T3/T5/T6/T7.

## Follow-on (separate plan)

`docs/superpowers/plans/2026-06-01-k2-krs-consumer.md` (to be written after this
lands): bump `k2-rules` to `v0.2.0`; region-scoped `Open` in
`engine.buildRuleEngine`; hold `[]*krs.DiskBundle` + `Engine.Close()` munmap;
hoist `ReversedParents` to `Engine.Match`/`MatchConn`; `e.tmp` allProxy-no-pin +
4096 cap; the heap-budget integration test; `make fetch-rules-embed` re-fetch;
`k2/rule/CLAUDE.md` constitution reference.
