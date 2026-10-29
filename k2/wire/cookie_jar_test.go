package wire

import "testing"

func TestCookieJar_PutGet(t *testing.T) {
	j := NewCookieJar()
	pin := "sha256:AAAA,sha256:BBBB"
	if got := j.Get(pin); got != "" {
		t.Fatalf("empty jar returned %q", got)
	}
	j.Put(pin, "cookie-1")
	if got := j.Get(pin); got != "cookie-1" {
		t.Fatalf("Get = %q, want cookie-1", got)
	}
	j.Put(pin, "cookie-2") // replace
	if got := j.Get(pin); got != "cookie-2" {
		t.Fatalf("Get after replace = %q, want cookie-2", got)
	}
	if got := j.Get("sha256:other"); got != "" {
		t.Fatalf("different pin leaked cookie: %q", got)
	}
}

func TestCookieJar_NilSafe(t *testing.T) {
	var j *CookieJar
	if got := j.Get("pin"); got != "" {
		t.Fatalf("nil jar Get = %q", got)
	}
	j.Put("pin", "c") // must not panic
}

func TestCookieJar_EmptyCookieIgnored(t *testing.T) {
	j := NewCookieJar()
	j.Put("pin", "c1")
	j.Put("pin", "") // an empty response cookie must not erase the stored one
	if got := j.Get("pin"); got != "c1" {
		t.Fatalf("empty Put erased cookie: %q", got)
	}
}

// The contract's structural guarantee: all three race candidates share ONE jar,
// otherwise the three-way race triggers three cold auths.
func TestClients_HoldInjectedJar(t *testing.T) {
	jar := NewCookieJar()
	qc := NewQUICClient(Config{Host: "h", Port: 443}, jar)
	tc := NewTCPWSClient(Config{Host: "h", Port: 443}, jar)
	if qc.jar != jar || tc.jar != jar {
		t.Fatal("constructor did not store the injected jar")
	}
	// nil is legal (probe / p2p paths).
	if c := NewQUICClient(Config{Host: "h", Port: 443}, nil); c.jar != nil {
		t.Fatal("nil jar not preserved")
	}
}
