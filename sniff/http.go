package sniff

import (
	"bytes"
	"net"
	"strings"
)

// SniffHTTP extracts the Host header value from an HTTP/1.x request.
// Returns the hostname (port stripped), or "" if the data is not a valid HTTP
// request or contains no Host header. Requires the full header block (up to \r\n\r\n).
func SniffHTTP(data []byte) string {
	// Must start with a known HTTP method.
	methods := []string{"GET ", "POST ", "PUT ", "DELETE ", "PATCH ", "HEAD ", "OPTIONS ", "CONNECT ", "TRACE "}
	valid := false
	for _, m := range methods {
		if bytes.HasPrefix(data, []byte(m)) {
			valid = true
			break
		}
	}
	if !valid {
		return ""
	}

	// Need complete headers (terminated by \r\n\r\n).
	end := bytes.Index(data, []byte("\r\n\r\n"))
	if end < 0 {
		return ""
	}
	headers := string(data[:end])

	// Find Host header (case-insensitive).
	for _, line := range strings.Split(headers, "\r\n")[1:] { // skip request line
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		key := strings.TrimSpace(line[:colon])
		if !strings.EqualFold(key, "host") {
			continue
		}
		host := strings.TrimSpace(line[colon+1:])
		// Strip port. Handle IPv6 bracket notation.
		if strings.HasPrefix(host, "[") {
			// IPv6: [::1]:8080
			if bracketEnd := strings.IndexByte(host, ']'); bracketEnd >= 0 {
				return host[1:bracketEnd]
			}
			return host
		}
		h, _, err := net.SplitHostPort(host)
		if err != nil {
			return host // no port
		}
		return h
	}
	return ""
}
