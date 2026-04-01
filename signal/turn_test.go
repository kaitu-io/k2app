package signal

import (
	"net"
	"testing"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/stun/v3"
	"github.com/pion/turn/v4"
)

func TestTURNCredentials_Generate(t *testing.T) {
	secret := "test-shared-secret"
	udid := "home-abc"

	creds := GenerateTURNCredentials(secret, udid, 1*time.Hour)

	if creds.Username == "" {
		t.Fatal("expected non-empty username")
	}
	if creds.Password == "" {
		t.Fatal("expected non-empty password")
	}
	if creds.TTL != 3600 {
		t.Errorf("TTL = %d, want 3600", creds.TTL)
	}
}

func TestTURNCredentials_Validate(t *testing.T) {
	secret := "test-shared-secret"
	udid := "home-abc"

	creds := GenerateTURNCredentials(secret, udid, 1*time.Hour)

	if !ValidateTURNCredentials(secret, creds.Username, creds.Password) {
		t.Error("expected valid credentials to pass")
	}
	if ValidateTURNCredentials(secret, creds.Username, "wrong") {
		t.Error("expected wrong password to fail")
	}
	if ValidateTURNCredentials("other-secret", creds.Username, creds.Password) {
		t.Error("expected wrong secret to fail")
	}
}

func TestTURNServer_Loopback(t *testing.T) {
	udpListener, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	turnAddr := udpListener.LocalAddr().String()

	sharedSecret := "test-secret"

	turnServer, err := turn.NewServer(turn.ServerConfig{
		Realm:       "k2p",
		AuthHandler: TURNAuthHandler(sharedSecret),
		PacketConnConfigs: []turn.PacketConnConfig{{
			PacketConn: udpListener,
			RelayAddressGenerator: &turn.RelayAddressGeneratorStatic{
				RelayAddress: net.ParseIP("127.0.0.1"),
				Address:      "0.0.0.0",
			},
		}},
	})
	if err != nil {
		t.Fatalf("start TURN server: %v", err)
	}
	defer turnServer.Close()

	creds := GenerateTURNCredentials(sharedSecret, "test", 1*time.Hour)

	turnURL, err := stun.ParseURI("turn:" + turnAddr + "?transport=udp")
	if err != nil {
		t.Fatalf("parse TURN URI: %v", err)
	}
	turnURL.Username = creds.Username
	turnURL.Password = creds.Password

	agent, err := ice.NewAgentWithOptions(
		ice.WithUrls([]*stun.URI{turnURL}),
		ice.WithNetworkTypes([]ice.NetworkType{ice.NetworkTypeUDP4}),
		ice.WithCandidateTypes([]ice.CandidateType{ice.CandidateTypeRelay}),
	)
	if err != nil {
		t.Fatalf("create ICE agent: %v", err)
	}
	defer agent.Close()

	relayCh := make(chan bool, 1)
	agent.OnCandidate(func(c ice.Candidate) {
		if c != nil && c.Type() == ice.CandidateTypeRelay {
			relayCh <- true
		}
	})
	agent.GatherCandidates()

	select {
	case <-relayCh:
		// Success: TURN allocation worked.
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for relay candidate — TURN allocation failed")
	}
}
