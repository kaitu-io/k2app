package k2p

import (
	"context"
	"testing"
	"time"
)

func TestClassifyNAT_EasyNAT(t *testing.T) {
	_, addr1 := startTestSTUNServer(t)
	_, addr2 := startTestSTUNServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := ClassifyNAT(ctx, []string{addr1, addr2})
	if err != nil {
		t.Fatalf("ClassifyNAT: %v", err)
	}
	if result.Type != NATEasy {
		t.Errorf("expected NATEasy, got %v", result.Type)
	}
	if result.Reflexive == nil {
		t.Fatal("expected non-nil reflexive address")
	}
}

func TestClassifyNAT_NeedsTwoServers(t *testing.T) {
	_, err := ClassifyNAT(context.Background(), []string{"one:3478"})
	if err == nil {
		t.Fatal("expected error with only 1 server")
	}
}

func TestNATType_String(t *testing.T) {
	tests := []struct {
		nat  NATType
		want string
	}{
		{NATEasy, "easy"},
		{NATHard, "hard"},
		{NATOpen, "open"},
		{NATUnknown, "unknown"},
	}
	for _, tt := range tests {
		if got := tt.nat.String(); got != tt.want {
			t.Errorf("NATType(%d).String() = %q, want %q", tt.nat, got, tt.want)
		}
	}
}

func TestParseNATType(t *testing.T) {
	tests := []struct {
		input string
		want  NATType
	}{
		{"easy", NATEasy},
		{"hard", NATHard},
		{"open", NATOpen},
		{"", NATUnknown},
		{"bogus", NATUnknown},
	}
	for _, tt := range tests {
		if got := parseNATType(tt.input); got != tt.want {
			t.Errorf("parseNATType(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
