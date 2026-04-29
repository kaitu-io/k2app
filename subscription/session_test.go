package subscription

import "testing"

func TestSession_CloseIdempotent(t *testing.T) {
	s := &Session{}
	// First close on zero value must not panic.
	s.Close()
	// Repeated close must not panic.
	s.Close()
}

func TestSession_CloseNilReceiver(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil receiver Close panicked: %v", r)
		}
	}()
	var s *Session
	s.Close()
}
