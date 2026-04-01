package signal

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec
	"encoding/base64"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/pion/turn/v4"
)

// TURNCredentials holds temporary TURN REST API credentials for ICE relay.
type TURNCredentials struct {
	URLs     []string `json:"urls"`
	Username string   `json:"username"`
	Password string   `json:"password"`
	TTL      int      `json:"ttl"`
}

// GenerateTURNCredentials creates time-limited TURN REST API credentials.
// Format: username = "expiry_unix:udid", password = base64(HMAC-SHA1(username, secret)).
// This follows the TURN REST API spec (draft-uberti-behave-turn-rest-00).
func GenerateTURNCredentials(sharedSecret, udid string, ttl time.Duration) TURNCredentials {
	expiry := time.Now().Add(ttl).Unix()
	username := fmt.Sprintf("%d:%s", expiry, udid)

	mac := hmac.New(sha1.New, []byte(sharedSecret))
	mac.Write([]byte(username))
	password := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return TURNCredentials{
		Username: username,
		Password: password,
		TTL:      int(ttl.Seconds()),
	}
}

// ValidateTURNCredentials verifies TURN REST API credentials against the shared secret.
func ValidateTURNCredentials(sharedSecret, username, password string) bool {
	parts := strings.SplitN(username, ":", 2)
	if len(parts) != 2 {
		return false
	}
	expiry, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return false
	}
	if time.Now().Unix() > expiry {
		return false
	}

	mac := hmac.New(sha1.New, []byte(sharedSecret))
	mac.Write([]byte(username))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(password), []byte(expected))
}

// TURNAuthHandler returns a pion/turn AuthHandler that validates TURN REST API credentials.
// It uses the same HMAC-SHA1 scheme as GenerateTURNCredentials.
func TURNAuthHandler(sharedSecret string) func(username, realm string, srcAddr net.Addr) ([]byte, bool) {
	return func(username, realm string, srcAddr net.Addr) ([]byte, bool) {
		parts := strings.SplitN(username, ":", 2)
		if len(parts) != 2 {
			return nil, false
		}
		expiry, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil || time.Now().Unix() > expiry {
			return nil, false
		}

		mac := hmac.New(sha1.New, []byte(sharedSecret))
		mac.Write([]byte(username))
		password := base64.StdEncoding.EncodeToString(mac.Sum(nil))

		return turn.GenerateAuthKey(username, realm, password), true
	}
}
