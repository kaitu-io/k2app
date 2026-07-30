package wire

import (
	"context"
	"crypto/tls"
	"errors"
	"testing"
	"time"
)

// metadataConsistencyCases is the SINGLE table both transports must satisfy
// identically. The two client orchestrations diverged once (QUIC returned
// errors, TCP-WS dropped them); this table is the structural guard against a
// second divergence. Phase 2 extends the same table with cookie cases.
var metadataConsistencyCases = []struct {
	name        string
	verdict     AuthVerdict
	enforce     bool
	wantConnect bool // DialTCP-level success expected?
	wantCode    int  // embedded StreamError code when rejected (0 = legacy/uncoded)
}{
	{"ok_enforce", AuthVerdict{OK: true, UserID: 7}, true, true, 0},
	{"reject401_enforce", AuthVerdict{Code: 401}, true, false, 401},
	{"reject402_enforce", AuthVerdict{Code: 402}, true, false, 402},
	{"reject403_enforce", AuthVerdict{Code: 403}, true, false, 403},
	{"undetermined_enforce", AuthVerdict{Code: -1}, true, false, 0},
	{"reject401_permissive", AuthVerdict{Code: 401}, false, true, 0},
	{"undetermined_permissive", AuthVerdict{Code: -1}, false, true, 0},
}

func fixedValidator(v AuthVerdict) Validator {
	return func(token, udid, mode string) AuthVerdict { return v }
}

// assertMetadataOutcome applies the shared expectations to one transport's
// DialTCP result. Rejections must carry ErrMetadataRejected and — when coded —
// the exact server code.
func assertMetadataOutcome(t *testing.T, transport string, err error,
	wantConnect bool, wantCode int) {
	t.Helper()
	if wantConnect {
		if err != nil {
			t.Fatalf("[%s] want connect success, got %v", transport, err)
		}
		return
	}
	if err == nil {
		t.Fatalf("[%s] want connect failure", transport)
	}
	if !errors.Is(err, ErrMetadataRejected) {
		t.Fatalf("[%s] rejection must carry ErrMetadataRejected, got %v", transport, err)
	}
	var se *StreamError
	if wantCode > 0 {
		if !errors.As(err, &se) || se.Code != wantCode {
			t.Fatalf("[%s] want StreamError{%d}, got %v", transport, wantCode, err)
		}
	} else {
		if !errors.Is(err, ErrStreamRejected) {
			t.Fatalf("[%s] uncoded rejection must carry ErrStreamRejected, got %v", transport, err)
		}
	}
}

func TestMetadataConsistency_QUIC(t *testing.T) {
	for _, tc := range metadataConsistencyCases {
		t.Run(tc.name, func(t *testing.T) {
			server, addr, cleanup := startRawQUICServer(t, fixedValidator(tc.verdict), tc.enforce)
			defer cleanup()

			client := NewQUICClient(Config{Host: "127.0.0.1", Port: addr.Port, UDID: "d", Token: "t"}, nil)
			client.tlsConfig = &tls.Config{InsecureSkipVerify: true, NextProtos: []string{"h3"}}
			defer client.Close()

			if tc.wantConnect {
				// Drain AcceptTCP so a successful proxy stream completes.
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					if conn, _, err := server.AcceptTCP(ctx); err == nil {
						WriteStreamResponse(conn, nil)
						conn.Close()
					}
				}()
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			conn, err := client.DialTCP(ctx, "example.com:80", nil)
			if conn != nil {
				conn.Close()
			}
			assertMetadataOutcome(t, "quic", err, tc.wantConnect, tc.wantCode)
		})
	}
}

func TestMetadataConsistency_TCPWS(t *testing.T) {
	for _, tc := range metadataConsistencyCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{Host: "127.0.0.1", Port: 443, UDID: "d", Token: "t"}
			client, server, cleanup := setupTCPWSEnforcePair(t, cfg, tc.enforce, fixedValidator(tc.verdict))
			defer cleanup()

			if tc.wantConnect {
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					if conn, _, err := server.AcceptTCP(ctx); err == nil {
						WriteStreamResponse(conn, nil)
						conn.Close()
					}
				}()
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			conn, err := client.DialTCP(ctx, "example.com:80", nil)
			if conn != nil {
				conn.Close()
			}
			assertMetadataOutcome(t, "tcpws", err, tc.wantConnect, tc.wantCode)
		})
	}
}
