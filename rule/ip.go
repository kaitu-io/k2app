package rule

import (
	"bytes"
	"net/netip"
	"sort"
)

// ipv4Section performs zero-allocation IPv4 address matching on a sorted byte slice.
//
// Data layout: (from[4] + to[4]) × count, sorted by from.
// Each entry is 8 bytes: 4-byte from address + 4-byte to address (network byte order).
// Binary search finds rightmost range where from <= target, then checks target <= to.
type ipv4Section struct {
	data  []byte
	count int
}

// Contains returns true if addr falls within any stored IPv4 range.
func (s *ipv4Section) Contains(addr netip.Addr) bool {
	if s.count == 0 || !addr.Is4() {
		return false
	}
	raw := addr.As4()
	// Find first index where from > raw, then subtract 1.
	idx := sort.Search(s.count, func(i int) bool {
		off := i * 8
		return bytes.Compare(s.data[off:off+4], raw[:]) > 0
	}) - 1
	if idx < 0 {
		return false
	}
	off := idx*8 + 4
	return bytes.Compare(raw[:], s.data[off:off+4]) <= 0
}

// ipv6Section performs zero-allocation IPv6 address matching on a sorted byte slice.
//
// Data layout: (from[16] + to[16]) × count, sorted by from.
// Each entry is 32 bytes: 16-byte from address + 16-byte to address (network byte order).
type ipv6Section struct {
	data  []byte
	count int
}

// Contains returns true if addr falls within any stored IPv6 range.
func (s *ipv6Section) Contains(addr netip.Addr) bool {
	if s.count == 0 || !addr.Is6() {
		return false
	}
	raw := addr.As16()
	idx := sort.Search(s.count, func(i int) bool {
		off := i * 32
		return bytes.Compare(s.data[off:off+16], raw[:]) > 0
	}) - 1
	if idx < 0 {
		return false
	}
	off := idx*32 + 16
	return bytes.Compare(raw[:], s.data[off:off+16]) <= 0
}

// ipRange holds a from/to pair for sorting and merging.
type ipRange struct {
	from []byte
	to   []byte
}

// buildIPv4Data parses CIDRs into a sorted, merged binary representation for ipv4Section.
func buildIPv4Data(cidrs []string) ([]byte, int) {
	var ranges []ipRange
	for _, s := range cidrs {
		prefix, err := netip.ParsePrefix(s)
		if err != nil || !prefix.Addr().Is4() {
			continue
		}
		from := prefix.Masked().Addr().As4()
		to := from
		for i := prefix.Bits(); i < 32; i++ {
			to[i/8] |= 1 << (7 - i%8)
		}
		ranges = append(ranges, ipRange{from: from[:], to: to[:]})
	}
	return mergeAndSerialize(ranges, 4)
}

// buildIPv6Data parses CIDRs into a sorted, merged binary representation for ipv6Section.
func buildIPv6Data(cidrs []string) ([]byte, int) {
	var ranges []ipRange
	for _, s := range cidrs {
		prefix, err := netip.ParsePrefix(s)
		if err != nil || !prefix.Addr().Is6() {
			continue
		}
		from := prefix.Masked().Addr().As16()
		to := from
		for i := prefix.Bits(); i < 128; i++ {
			to[i/8] |= 1 << (7 - i%8)
		}
		ranges = append(ranges, ipRange{from: from[:], to: to[:]})
	}
	return mergeAndSerialize(ranges, 16)
}

// mergeAndSerialize sorts ranges by from, merges overlapping/adjacent, and serializes.
func mergeAndSerialize(ranges []ipRange, addrLen int) ([]byte, int) {
	if len(ranges) == 0 {
		return nil, 0
	}
	sort.Slice(ranges, func(i, j int) bool {
		return bytes.Compare(ranges[i].from, ranges[j].from) < 0
	})

	// Merge overlapping/adjacent ranges.
	merged := []ipRange{ranges[0]}
	for _, r := range ranges[1:] {
		last := &merged[len(merged)-1]
		if bytes.Compare(r.from, last.to) <= 0 {
			if bytes.Compare(r.to, last.to) > 0 {
				last.to = r.to
			}
		} else {
			merged = append(merged, r)
		}
	}

	entrySize := addrLen * 2
	data := make([]byte, len(merged)*entrySize)
	for i, r := range merged {
		off := i * entrySize
		copy(data[off:off+addrLen], r.from)
		copy(data[off+addrLen:off+entrySize], r.to)
	}
	return data, len(merged)
}
