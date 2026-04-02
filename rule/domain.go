package rule

import (
	"encoding/binary"
	"sort"
	"strings"
)

// domainSection performs zero-allocation domain suffix matching on a byte slice.
//
// Data layout:
//
//	count       (4 bytes LE)
//	offsets     [(count+1) * 4 bytes LE] — includes sentinel
//	strings     concatenated reversed domain strings
//
// Domains are stored as reversed, lowercased, dot-prefixed strings, sorted
// lexicographically. For example, "google.com" is stored as "moc.elgoog.".
type domainSection struct {
	data  []byte // raw binary data (mmap-friendly)
	count int    // number of entries
}

// entryAt returns the i-th reversed domain string from the binary layout.
func (ds *domainSection) entryAt(i int) string {
	base := 4 + (ds.count+1)*4 // offset past count + all offset entries
	offStart := 4 + i*4
	offEnd := offStart + 4
	start := int(binary.LittleEndian.Uint32(ds.data[offStart:offEnd]))
	end := int(binary.LittleEndian.Uint32(ds.data[offEnd : offEnd+4]))
	return string(ds.data[base+start : base+end])
}

// Match reports whether domain matches any suffix rule in the section.
func (ds *domainSection) Match(domain string) bool {
	if ds.count == 0 || ds.data == nil {
		return false
	}

	domain = strings.ToLower(domain)

	// Generate all suffixes: the full domain + each parent domain.
	// For "www.google.com" we test: ".www.google.com", ".google.com", ".com"
	// We also test the exact domain: ".www.google.com" covers the full match case.
	for i := -1; i < len(domain); i++ {
		if i != -1 && domain[i] != '.' {
			continue
		}

		var suffix string
		if i == -1 {
			suffix = "." + domain
		} else {
			suffix = domain[i:] // includes the leading dot
		}

		reversed := reverseString(suffix)

		// Binary search for exact match in sorted entries.
		j := sort.Search(ds.count, func(k int) bool {
			return ds.entryAt(k) >= reversed
		})
		if j < ds.count && ds.entryAt(j) == reversed {
			return true
		}
	}

	return false
}

// reverseString reverses a string byte-by-byte (ASCII-safe for domain names).
func reverseString(s string) string {
	n := len(s)
	b := make([]byte, n)
	for i := 0; i < n; i++ {
		b[i] = s[n-1-i]
	}
	return string(b)
}

// buildDomainData builds the binary domain section from a list of domain strings.
// Each domain is dot-prefixed, lowercased, reversed, and sorted. Returns nil for
// empty input.
func buildDomainData(domains []string) []byte {
	if len(domains) == 0 {
		return nil
	}

	// Prepare reversed entries.
	entries := make([]string, len(domains))
	for i, d := range domains {
		entries[i] = reverseString("." + strings.ToLower(d))
	}
	sort.Strings(entries)

	// Calculate total strings size.
	totalLen := 0
	for _, e := range entries {
		totalLen += len(e)
	}

	count := len(entries)
	// Layout: count(4) + offsets((count+1)*4) + strings(totalLen)
	size := 4 + (count+1)*4 + totalLen
	data := make([]byte, size)

	// Write count.
	binary.LittleEndian.PutUint32(data[0:4], uint32(count))

	// Write offsets and strings.
	strBase := 4 + (count+1)*4
	offset := 0
	for i, e := range entries {
		binary.LittleEndian.PutUint32(data[4+i*4:4+i*4+4], uint32(offset))
		copy(data[strBase+offset:], e)
		offset += len(e)
	}
	// Sentinel offset.
	binary.LittleEndian.PutUint32(data[4+count*4:4+count*4+4], uint32(offset))

	return data
}
