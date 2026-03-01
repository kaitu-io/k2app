package sniff

import "testing"

func TestSniffHTTP(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{
			name: "GET with Host header",
			data: []byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"),
			want: "example.com",
		},
		{
			name: "Host with port stripped",
			data: []byte("GET / HTTP/1.1\r\nHost: example.com:8080\r\n\r\n"),
			want: "example.com",
		},
		{
			name: "POST request",
			data: []byte("POST /api/v1 HTTP/1.1\r\nHost: api.example.com\r\nContent-Length: 0\r\n\r\n"),
			want: "api.example.com",
		},
		{
			name: "TLS data returns empty",
			data: []byte{0x16, 0x03, 0x01, 0x00, 0x05},
			want: "",
		},
		{
			name: "No Host header",
			data: []byte("GET / HTTP/1.1\r\nAccept: */*\r\n\r\n"),
			want: "",
		},
		{
			name: "nil data",
			data: nil,
			want: "",
		},
		{
			name: "empty data",
			data: []byte{},
			want: "",
		},
		{
			name: "truncated request no CRLFCRLF",
			data: []byte("GET / HTTP/1.1\r\nHost: example.com\r\n"),
			want: "",
		},
		{
			name: "case insensitive host header",
			data: []byte("GET / HTTP/1.1\r\nhost: lowercase.example.com\r\n\r\n"),
			want: "lowercase.example.com",
		},
		{
			name: "IP as host",
			data: []byte("GET / HTTP/1.1\r\nHost: 192.168.1.1\r\n\r\n"),
			want: "192.168.1.1",
		},
		{
			name: "IPv6 host with port",
			data: []byte("GET / HTTP/1.1\r\nHost: [::1]:8080\r\n\r\n"),
			want: "::1",
		},
		{
			name: "PUT request",
			data: []byte("PUT /resource HTTP/1.1\r\nHost: put.example.com\r\n\r\n"),
			want: "put.example.com",
		},
		{
			name: "DELETE request",
			data: []byte("DELETE /resource HTTP/1.1\r\nHost: del.example.com\r\n\r\n"),
			want: "del.example.com",
		},
		{
			name: "PATCH request",
			data: []byte("PATCH /resource HTTP/1.1\r\nHost: patch.example.com\r\n\r\n"),
			want: "patch.example.com",
		},
		{
			name: "HEAD request",
			data: []byte("HEAD / HTTP/1.1\r\nHost: head.example.com\r\n\r\n"),
			want: "head.example.com",
		},
		{
			name: "OPTIONS request",
			data: []byte("OPTIONS * HTTP/1.1\r\nHost: options.example.com\r\n\r\n"),
			want: "options.example.com",
		},
		{
			name: "CONNECT request",
			data: []byte("CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"),
			want: "example.com",
		},
		{
			name: "TRACE request",
			data: []byte("TRACE / HTTP/1.1\r\nHost: trace.example.com\r\n\r\n"),
			want: "trace.example.com",
		},
		{
			name: "non-HTTP data",
			data: []byte("HELLO world this is not HTTP\r\n\r\n"),
			want: "",
		},
		{
			name: "IPv6 host without port",
			data: []byte("GET / HTTP/1.1\r\nHost: ::1\r\n\r\n"),
			want: "::1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SniffHTTP(tt.data)
			if got != tt.want {
				t.Errorf("SniffHTTP() = %q, want %q", got, tt.want)
			}
		})
	}
}
