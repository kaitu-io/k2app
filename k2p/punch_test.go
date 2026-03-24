package k2p

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestPunchStandard_Loopback(t *testing.T) {
	conn1, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn1.Close()
	conn2, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn2.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	addr1 := conn1.LocalAddr().(*net.UDPAddr)
	addr2 := conn2.LocalAddr().(*net.UDPAddr)

	errCh := make(chan error, 2)
	go func() {
		_, err := punchStandard(ctx, conn1, addr2)
		errCh <- err
	}()
	go func() {
		_, err := punchStandard(ctx, conn2, addr1)
		errCh <- err
	}()

	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			t.Errorf("punch %d: %v", i, err)
		}
	}
}

func TestPunchStandard_Timeout(t *testing.T) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// No one sending punch packets to us — should timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	unreachable := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}
	_, err = punchStandard(ctx, conn, unreachable)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestPunch_SelectsStrategy(t *testing.T) {
	tests := []struct {
		name     string
		local    NATType
		remote   NATType
		expected string
	}{
		{"easy-easy", NATEasy, NATEasy, "standard"},
		{"easy-hard", NATEasy, NATHard, "standard"},
		{"hard-easy", NATHard, NATEasy, "standard"},
		{"hard-hard", NATHard, NATHard, "birthday"},
		{"open-any", NATOpen, NATHard, "standard"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			strategy := selectPunchStrategy(tt.local, tt.remote)
			if strategy != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, strategy)
			}
		})
	}
}

func TestPunchBirthday_Loopback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 2)
	resultCh := make(chan *BirthdayResult, 2)

	go func() {
		r, err := punchBirthday(ctx, net.IPv4(127, 0, 0, 1), net.IPv4(127, 0, 0, 1),
			30000, 30063, 40000, 40063)
		errCh <- err
		resultCh <- r
	}()
	go func() {
		r, err := punchBirthday(ctx, net.IPv4(127, 0, 0, 1), net.IPv4(127, 0, 0, 1),
			40000, 40063, 30000, 30063)
		errCh <- err
		resultCh <- r
	}()

	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			t.Errorf("birthday punch %d: %v", i, err)
		}
	}
	for i := 0; i < 2; i++ {
		r := <-resultCh
		if r != nil && r.Conn == nil {
			t.Errorf("result %d: expected non-nil conn", i)
		}
		if r != nil && r.Conn != nil {
			r.Conn.Close()
		}
	}
}
