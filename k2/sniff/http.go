package sniff

import "bytes"

// httpMethods are valid HTTP/1.x request method prefixes (package-level, zero alloc).
var httpMethods = [][]byte{
	[]byte("GET "),
	[]byte("POST "),
	[]byte("PUT "),
	[]byte("DELETE "),
	[]byte("PATCH "),
	[]byte("HEAD "),
	[]byte("OPTIONS "),
	[]byte("CONNECT "),
	[]byte("TRACE "),
}

var crlf = []byte("\r\n")
var crlfcrlf = []byte("\r\n\r\n")

// SniffHTTP extracts the Host header value from an HTTP/1.x request.
// Returns the hostname (port stripped), or "" if the data is not a valid HTTP
// request or contains no Host header. Zero-allocation on the hot path.
func SniffHTTP(data []byte) string {
	if len(data) == 0 {
		return ""
	}

	// Must start with a known HTTP method.
	valid := false
	for _, m := range httpMethods {
		if bytes.HasPrefix(data, m) {
			valid = true
			break
		}
	}
	if !valid {
		return ""
	}

	// Need complete headers (terminated by \r\n\r\n).
	end := bytes.Index(data, crlfcrlf)
	if end < 0 {
		return ""
	}
	headers := data[:end]

	// Skip request line, scan header lines for "Host:".
	pos := bytes.Index(headers, crlf)
	if pos < 0 {
		return ""
	}
	headers = headers[pos+2:]

	for len(headers) > 0 {
		var line []byte
		if i := bytes.Index(headers, crlf); i >= 0 {
			line = headers[:i]
			headers = headers[i+2:]
		} else {
			line = headers
			headers = nil
		}

		colon := bytes.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		key := bytes.TrimSpace(line[:colon])
		if len(key) != 4 {
			continue
		}
		// Case-insensitive "host" check without allocation.
		if (key[0]|0x20) != 'h' || (key[1]|0x20) != 'o' || (key[2]|0x20) != 's' || (key[3]|0x20) != 't' {
			continue
		}
		host := bytes.TrimSpace(line[colon+1:])
		return stripPort(host)
	}
	return ""
}

// stripPort removes the port from a host byte slice, returning the hostname as string.
// Handles IPv6 bracket notation ([::1]:8080) and plain host:port.
func stripPort(host []byte) string {
	if len(host) == 0 {
		return ""
	}
	if host[0] == '[' {
		// IPv6: [::1]:8080 → ::1
		if i := bytes.IndexByte(host, ']'); i >= 0 {
			return string(host[1:i])
		}
		return string(host)
	}
	// If there's exactly one colon, it's host:port. Multiple colons = bare IPv6.
	if i := bytes.IndexByte(host, ':'); i >= 0 && bytes.IndexByte(host[i+1:], ':') < 0 {
		return string(host[:i])
	}
	return string(host)
}
