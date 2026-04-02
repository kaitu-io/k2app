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
	version    = 1
	headerSize = 32
	indexSize  = 28 // per set
)

// BundleSet is a named collection of domain suffix and CIDR rules.
// For writing, populate DomainSuffixes and CIDRs. After reading,
// use MatchDomain and MatchIP for lookups backed by the binary data.
type BundleSet struct {
	Name           string   // e.g. "google"
	DomainSuffixes []string // write input: raw domains ("google.com")
	CIDRs          []string // write input: CIDR strings ("8.8.8.0/24")

	// Read-side: backed by data slice (mmap-friendly).
	domains domainSection
	ipv4    ipv4Section
	ipv6    ipv6Section
}

// MatchDomain reports whether domain matches any suffix rule in this set.
func (s *BundleSet) MatchDomain(domain string) bool {
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

// WriteBundle serializes sets into k2b v1 binary format.
func WriteBundle(w io.Writer, sets []BundleSet) error {
	setCount := len(sets)

	// Build all data sections up front.
	type builtSet struct {
		nameBytes  []byte
		domainData []byte
		ipv4Data   []byte
		ipv4Count  int
		ipv6Data   []byte
		ipv6Count  int
	}

	built := make([]builtSet, setCount)
	for i := range sets {
		built[i].nameBytes = []byte(sets[i].Name)
		built[i].domainData = buildDomainData(sets[i].DomainSuffixes)
		built[i].ipv4Data, built[i].ipv4Count = buildIPv4Data(sets[i].CIDRs)
		built[i].ipv6Data, built[i].ipv6Count = buildIPv6Data(sets[i].CIDRs)
	}

	// Compute offsets. Data starts after header + index.
	dataStart := uint32(headerSize + indexSize*setCount)
	offset := dataStart

	type indexEntry struct {
		nameOff    uint32
		nameLen    uint16
		domainOff  uint32
		domainCnt  uint32
		ipv4Off    uint32
		ipv4Cnt    uint16
		ipv6Off    uint32
		ipv6Cnt    uint16
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

	// Write index (28 bytes per set).
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
		if _, err := w.Write(idx[:]); err != nil {
			return fmt.Errorf("write index[%d]: %w", i, err)
		}
	}

	// Write data sections: names, domains, ipv4, ipv6.
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

	return nil
}

// ReadBundle parses k2b v1 binary data into a Bundle. The returned Bundle
// references slices of the input data directly (mmap-friendly, zero-copy).
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
		return nil, fmt.Errorf("k2b: unsupported version %d", ver)
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
			bundle.Sets[i].domains = domainSection{data: data[domainOff:], count: actualCount}
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
	}

	return bundle, nil
}
