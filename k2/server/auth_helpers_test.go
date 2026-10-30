package server

import (
	"bufio"
	"os"
	"path/filepath"
	"testing"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/wire"
	smux "github.com/xtaci/smux/v2"
)

const (
	testUDID  = "test-udid"
	testToken = "test-token"
)

// testAuthConfig returns a users_file holding one credential. k2s always
// enforces device auth, so New refuses a config with nothing to check
// credentials against.
func testAuthConfig(t *testing.T) config.AuthConfig {
	t.Helper()
	path := filepath.Join(t.TempDir(), "users")
	if err := os.WriteFile(path, []byte(testUDID+":"+testToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return config.AuthConfig{UsersFile: path}
}

// authenticateTCPWS sends the metadata stream a real client opens before any
// proxy stream; without it the enforce gate rejects every proxy stream.
func authenticateTCPWS(t *testing.T, sess *smux.Session) {
	t.Helper()
	stream, err := sess.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if err := wire.WriteStreamHeader(stream, wire.StreamTypeMeta, ""); err != nil {
		t.Fatal(err)
	}
	if err := wire.WriteMetadataFrame(stream, wire.MetadataFrame{UDID: testUDID, Token: testToken}); err != nil {
		t.Fatal(err)
	}
	ok, _, errMsg, err := wire.ReadStreamResponse(bufio.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("metadata auth rejected: %s", errMsg)
	}
}
