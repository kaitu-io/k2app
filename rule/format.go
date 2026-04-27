package rule

import (
	"encoding/binary"
	"fmt"
	"io"
	"net/netip"
	"time"
)

const (
	magic      = "K2RB"
	version    = 2
	headerSize = 32
	indexSize  = 36 // per set: v1 (28) + 8 bytes for exclDomainOff/exclDomainCnt
)

// BundleSet is a named collection of domain suffix and CIDR rules.
// For writing, populate DomainSuffixes, ExcludeDomains, and CIDRs. After
// reading, use MatchDomain and MatchIP for lookups backed by the binary data.
//
// Exclude semantics: MatchDomain checks ExcludeDomains first. Entries matching
// an exclude suffix are rejected even if they would otherwise hit a DomainSuffixes
// rule. Used to carve subdomains out of a broader suffix match (e.g. "weibo.com"
// include + "hk.weibo.com" exclude → "m.hk.weibo.com" does not match).
type BundleSet struct {
	Name           string   // e.g. "google"
	DomainSuffixes []string // write input: raw domains ("google.com")
	ExcludeDomains []string // write input: suffixes to exclude from DomainSuffixes
	CIDRs          []string // write input: CIDR strings ("8.8.8.0/24")

	// Read-side: backed by data slice (mmap-friendly).
	domains        domainSection
	excludeDomains domainSection
	ipv4           ipv4Section
	ipv6           ipv6Section
}

// MatchDomain reports whether domain matches any suffix rule in this set.
// Exclude domains take priority: if domain hits the exclude list, returns false.
func (s *BundleSet) MatchDomain(domain string) bool {
	if s.excludeDomains.Match(domain) {
		return false
	}
	return s.domains.Match(domain)
}

// MatchIP reports whether addr falls within any CIDR rule in this set.
func (s *BundleSet) MatchIP(addr netip.Addr) bool {
	if addr.Is4() {
		return s.ipv4.Contains(addr)
	}
	return s.ipv6.Contains(addr)
}

// Bundle is the result of ReadBundle — a collection of sets backed by raw bytes.
type Bundle struct {
	Sets []BundleSet
	data []byte // raw k2b bytes (mmap backing)
}

// WriteBundle serializes sets into k2b v2 binary format. Layout matches the
// canonical publisher at kaitu-io/k2-rules/main.go (writeK2B).
//
// Layout:
//
//	header (32 bytes)
//	index  (36 bytes * setCount)
//	data   (names → domains → ipv4 → ipv6 → excludeDomains)
func WriteBundle(w io.Writer, sets []BundleSet) error {
	setCount := len(sets)

	// Build all data sections up front.
	type builtSet struct {
		nameBytes      []byte
		domainData     []byte
		exclDomainData []byte
		ipv4Data       []byte
		ipv4Count      int
		ipv6Data       []byte
		ipv6Count      int
	}

	built := make([]builtSet, setCount)
	for i := range sets {
		built[i].nameBytes = []byte(sets[i].Name)
		built[i].domainData = buildDomainData(sets[i].DomainSuffixes)
		built[i].exclDomainData = buildDomainData(sets[i].ExcludeDomains)
		built[i].ipv4Data, built[i].ipv4Count = buildIPv4Data(sets[i].CIDRs)
		built[i].ipv6Data, built[i].ipv6Count = buildIPv6Data(sets[i].CIDRs)
	}

	// Compute offsets. Data starts after header + index.
	dataStart := uint32(headerSize + indexSize*setCount)
	offset := dataStart

	type indexEntry struct {
		nameOff       uint32
		nameLen       uint16
		domainOff     uint32
		domainCnt     uint32
		ipv4Off       uint32
		ipv4Cnt       uint16
		ipv6Off       uint32
		ipv6Cnt       uint16
		exclDomainOff uint32
		exclDomainCnt uint32
	}

	entries := make([]indexEntry, setCount)

	// Names section.
	for i := range built {
		entries[i].nameOff = offset
		entries[i].nameLen = uint16(len(built[i].nameBytes))
		offset += uint32(len(built[i].nameBytes))
	}
	// Domain sections.
	for i := range built {
		entries[i].domainOff = offset
		entries[i].domainCnt = uint32(len(sets[i].DomainSuffixes))
		offset += uint32(len(built[i].domainData))
	}
	// IPv4 sections.
	for i := range built {
		entries[i].ipv4Off = offset
		entries[i].ipv4Cnt = uint16(built[i].ipv4Count)
		offset += uint32(len(built[i].ipv4Data))
	}
	// IPv6 sections.
	for i := range built {
		entries[i].ipv6Off = offset
		entries[i].ipv6Cnt = uint16(built[i].ipv6Count)
		offset += uint32(len(built[i].ipv6Data))
	}
	// Exclude domain sections (v2).
	for i := range built {
		entries[i].exclDomainOff = offset
		entries[i].exclDomainCnt = uint32(len(sets[i].ExcludeDomains))
		offset += uint32(len(built[i].exclDomainData))
	}

	// Write header (32 bytes).
	var hdr [headerSize]byte
	copy(hdr[0:4], magic)
	binary.LittleEndian.PutUint32(hdr[4:8], version)
	binary.LittleEndian.PutUint32(hdr[8:12], uint32(setCount))
	binary.LittleEndian.PutUint64(hdr[12:20], uint64(time.Now().Unix()))
	// [20:28] checksum reserved = 0
	// [28:32] reserved = 0
	if _, err := w.Write(hdr[:]); err != nil {
		return fmt.Errorf("write header: %w", err)
	}

	// Write index (36 bytes per set).
	for i := range entries {
		var idx [indexSize]byte
		e := &entries[i]
		binary.LittleEndian.PutUint32(idx[0:4], e.nameOff)
		binary.LittleEndian.PutUint16(idx[4:6], e.nameLen)
		// idx[6:8] reserved
		binary.LittleEndian.PutUint32(idx[8:12], e.domainOff)
		binary.LittleEndian.PutUint32(idx[12:16], e.domainCnt)
		binary.LittleEndian.PutUint32(idx[16:20], e.ipv4Off)
		binary.LittleEndian.PutUint16(idx[20:22], e.ipv4Cnt)
		binary.LittleEndian.PutUint32(idx[22:26], e.ipv6Off)
		binary.LittleEndian.PutUint16(idx[26:28], e.ipv6Cnt)
		binary.LittleEndian.PutUint32(idx[28:32], e.exclDomainOff)
		binary.LittleEndian.PutUint32(idx[32:36], e.exclDomainCnt)
		if _, err := w.Write(idx[:]); err != nil {
			return fmt.Errorf("write index[%d]: %w", i, err)
		}
	}

	// Write data sections: names, domains, ipv4, ipv6, excludeDomains.
	for i := range built {
		if _, err := w.Write(built[i].nameBytes); err != nil {
			return fmt.Errorf("write name[%d]: %w", i, err)
		}
	}
	for i := range built {
		if _, err := w.Write(built[i].domainData); err != nil {
			return fmt.Errorf("write domains[%d]: %w", i, err)
		}
	}
	for i := range built {
		if _, err := w.Write(built[i].ipv4Data); err != nil {
			return fmt.Errorf("write ipv4[%d]: %w", i, err)
		}
	}
	for i := range built {
		if _, err := w.Write(built[i].ipv6Data); err != nil {
			return fmt.Errorf("write ipv6[%d]: %w", i, err)
		}
	}
	for i := range built {
		if _, err := w.Write(built[i].exclDomainData); err != nil {
			return fmt.Errorf("write exclude domains[%d]: %w", i, err)
		}
	}

	return nil
}

// ReadBundle parses k2b v2 binary data into a Bundle. The returned Bundle
// references slices of the input data directly (mmap-friendly, zero-copy).
//
// v1 format is rejected with "unsupported version 1 (want 2)". The CDN-hosted
// bundles at kaitu-io/k2-rules are v2-only since commit 77f117e (2026-04).
func ReadBundle(data []byte) (*Bundle, error) {
	if len(data) < headerSize {
		return nil, fmt.Errorf("k2b: data too short for header (%d bytes)", len(data))
	}

	// Validate magic.
	if string(data[0:4]) != magic {
		return nil, fmt.Errorf("k2b: bad magic %q", string(data[0:4]))
	}

	// Validate version.
	ver := binary.LittleEndian.Uint32(data[4:8])
	if ver != version {
		return nil, fmt.Errorf("k2b: unsupported version %d (want %d)", ver, version)
	}

	setCount := int(binary.LittleEndian.Uint32(data[8:12]))

	// Validate total size for index.
	minSize := headerSize + indexSize*setCount
	if len(data) < minSize {
		return nil, fmt.Errorf("k2b: data too short for %d sets (%d < %d)", setCount, len(data), minSize)
	}

	bundle := &Bundle{
		Sets: make([]BundleSet, setCount),
		data: data,
	}

	for i := 0; i < setCount; i++ {
		idxOff := headerSize + indexSize*i
		idx := data[idxOff : idxOff+indexSize]

		nameOff := int(binary.LittleEndian.Uint32(idx[0:4]))
		nameLen := int(binary.LittleEndian.Uint16(idx[4:6]))
		domainOff := int(binary.LittleEndian.Uint32(idx[8:12]))
		domainCount := int(binary.LittleEndian.Uint32(idx[12:16]))
		ipv4Off := int(binary.LittleEndian.Uint32(idx[16:20]))
		ipv4Count := int(binary.LittleEndian.Uint16(idx[20:22]))
		ipv6Off := int(binary.LittleEndian.Uint32(idx[22:26]))
		ipv6Count := int(binary.LittleEndian.Uint16(idx[26:28]))
		exclDomainOff := int(binary.LittleEndian.Uint32(idx[28:32]))
		exclDomainCount := int(binary.LittleEndian.Uint32(idx[32:36]))

		// Name.
		if nameOff+nameLen > len(data) {
			return nil, fmt.Errorf("k2b: set[%d] name out of bounds", i)
		}
		bundle.Sets[i].Name = string(data[nameOff : nameOff+nameLen])

		// Domain section.
		if domainCount > 0 {
			if domainOff+4 > len(data) {
				return nil, fmt.Errorf("k2b: set[%d] domain data out of bounds", i)
			}
			actualCount := int(binary.LittleEndian.Uint32(data[domainOff : domainOff+4]))
			// Validate offset table fits: 4-byte count header + (actualCount+1)*4 offset entries.
			minRequired := domainOff + 4 + (actualCount+1)*4
			if minRequired > len(data) {
				return nil, fmt.Errorf("k2b: set[%d] domain count %d exceeds data bounds", i, actualCount)
			}
			sectionData := data[domainOff:]
			if err := validateDomainOffsets(sectionData, actualCount); err != nil {
				return nil, fmt.Errorf("k2b: set[%d] domain: %w", i, err)
			}
			bundle.Sets[i].domains = domainSection{data: sectionData, count: actualCount}
		}

		// IPv4 section.
		if ipv4Count > 0 {
			entrySize := 8 // 4+4
			end := ipv4Off + ipv4Count*entrySize
			if end > len(data) {
				return nil, fmt.Errorf("k2b: set[%d] ipv4 data out of bounds", i)
			}
			bundle.Sets[i].ipv4 = ipv4Section{data: data[ipv4Off:end], count: ipv4Count}
		}

		// IPv6 section.
		if ipv6Count > 0 {
			entrySize := 32 // 16+16
			end := ipv6Off + ipv6Count*entrySize
			if end > len(data) {
				return nil, fmt.Errorf("k2b: set[%d] ipv6 data out of bounds", i)
			}
			bundle.Sets[i].ipv6 = ipv6Section{data: data[ipv6Off:end], count: ipv6Count}
		}

		// Exclude domain section (v2).
		if exclDomainCount > 0 {
			if exclDomainOff+4 > len(data) {
				return nil, fmt.Errorf("k2b: set[%d] exclude domain data out of bounds", i)
			}
			actualCount := int(binary.LittleEndian.Uint32(data[exclDomainOff : exclDomainOff+4]))
			minRequired := exclDomainOff + 4 + (actualCount+1)*4
			if minRequired > len(data) {
				return nil, fmt.Errorf("k2b: set[%d] exclude domain count %d exceeds data bounds", i, actualCount)
			}
			sectionData := data[exclDomainOff:]
			if err := validateDomainOffsets(sectionData, actualCount); err != nil {
				return nil, fmt.Errorf("k2b: set[%d] exclude domain: %w", i, err)
			}
			bundle.Sets[i].excludeDomains = domainSection{data: sectionData, count: actualCount}
		}
	}

	return bundle, nil
}

// validateDomainOffsets checks that every entry in a domainSection's offset
// table points within the section's string area and is monotonically
// non-decreasing. Without this guard, a crafted bundle whose offsets exceed
// the data slice triggers a slice-out-of-bounds panic in entryAt during the
// first MatchDomain call. Bundles are downloaded from CDN mirrors with no
// signature verification (rule/downloader.go), so the bytes here are
// untrusted.
func validateDomainOffsets(sectionData []byte, count int) error {
	stringBase := 4 + (count+1)*4
	if stringBase > len(sectionData) {
		return fmt.Errorf("offset table %d bytes exceeds section %d bytes", stringBase, len(sectionData))
	}
	stringAreaLen := len(sectionData) - stringBase
	var prev uint32
	for k := 0; k <= count; k++ {
		off := binary.LittleEndian.Uint32(sectionData[4+k*4 : 4+k*4+4])
		if int(off) > stringAreaLen {
			return fmt.Errorf("offset[%d]=%d exceeds string area (%d bytes)", k, off, stringAreaLen)
		}
		if k > 0 && off < prev {
			return fmt.Errorf("offsets non-monotonic at %d: %d < %d", k, off, prev)
		}
		prev = off
	}
	return nil
}
