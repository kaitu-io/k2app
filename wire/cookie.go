package wire

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/sasha-s/go-deadlock"
)

// Phase 2 of the k2v5 auth redesign (spec §6): node-issued connection cookie.
//
// A cookie is the node's own signed record of a recent Center endorsement,
// verified locally (HMAC, microseconds) so reconnects / transport switches /
// port hops / node restarts inside the window never touch Center.
//
// THE core invariant (spec §6.3): a cookie is NEVER renewed locally. The only
// event that produces a new cookie is a fresh Center cold auth, so AuthAt is
// always a real Center endorsement time and revocation lag is hard-capped at
// cookieTTL no matter how often the cookie is presented.

const (
	// cookieTTL bounds the age of the Center endorsement a cookie carries.
	cookieTTL = 15 * time.Minute

	cookieVersion = 1
	cookieMACLen  = 16 // HMAC-SHA256 truncated to 128 bit (spec §6.2)

	// cookieKeyRotatePeriod: automatic signing-key rotation. Without it, a
	// leaked key stays valid until a human notices (spec §6.7).
	cookieKeyRotatePeriod = 30 * 24 * time.Hour
	// cookieKeyOverlap: the previous key keeps verifying for 2×cookieTTL
	// after a rotation — covers every cookie the old key could have signed.
	cookieKeyOverlap = 30 * time.Minute

	cookieKeyFileName = "cookie.key"
)

// Cookie is the decoded cookie payload (spec §6.2 / shared contract).
//
// SIGNED, NOT ENCRYPTED: the payload is plainly readable by whoever holds the
// cookie. That is fine — the holder is the device itself, and every field is
// something the device already knows or may know. Do NOT add fields that
// assume confidentiality (node-internal identifiers, keys, other users' data).
//
// NEVER LOG a raw cookie or its MAC (spec §6.2/§9): device logs are uploaded
// off-device. Log KeyID, the first 8 bytes of SessionID, and verdicts only —
// TestNoCredentialsInLogs (Task 7) enforces this statically.
type Cookie struct {
	Ver       uint8
	KeyID     uint8
	UDID      string // server-determined device identity
	UserID    uint64 // Center user id
	Mode      string // "" or "gateway" (Center-validated, spec §5.4)
	SessionID [16]byte
	AuthAt    int64 // unix seconds of the Center endorsement
	SvcExp    int64 // Center ServiceExpiredAt; 0 = no subscription clock (users_file)
}

// payload layout (fixed 44 bytes + udid):
// [ver:1][key_id:1][mode:1][session_id:16][user_id:8 BE][auth_at:8 BE][svc_exp:8 BE][udid_len:1][udid…]
const cookieFixedLen = 44

func (c Cookie) marshalPayload() ([]byte, error) {
	if len(c.UDID) > 255 {
		return nil, fmt.Errorf("wire: cookie udid too long: %d", len(c.UDID))
	}
	var modeB byte
	switch c.Mode {
	case "":
		modeB = 0
	case "gateway":
		modeB = 1
	default:
		return nil, fmt.Errorf("wire: cookie mode %q not encodable", c.Mode)
	}
	buf := make([]byte, 0, cookieFixedLen+len(c.UDID))
	buf = append(buf, c.Ver, c.KeyID, modeB)
	buf = append(buf, c.SessionID[:]...)
	buf = binary.BigEndian.AppendUint64(buf, c.UserID)
	buf = binary.BigEndian.AppendUint64(buf, uint64(c.AuthAt))
	buf = binary.BigEndian.AppendUint64(buf, uint64(c.SvcExp))
	buf = append(buf, byte(len(c.UDID)))
	buf = append(buf, c.UDID...)
	return buf, nil
}

func parseCookiePayload(buf []byte) (Cookie, bool) {
	if len(buf) < cookieFixedLen {
		return Cookie{}, false
	}
	var c Cookie
	c.Ver, c.KeyID = buf[0], buf[1]
	if c.Ver != cookieVersion {
		return Cookie{}, false
	}
	switch buf[2] {
	case 0:
		c.Mode = ""
	case 1:
		c.Mode = "gateway"
	default:
		return Cookie{}, false
	}
	copy(c.SessionID[:], buf[3:19])
	c.UserID = binary.BigEndian.Uint64(buf[19:27])
	c.AuthAt = int64(binary.BigEndian.Uint64(buf[27:35]))
	c.SvcExp = int64(binary.BigEndian.Uint64(buf[35:43]))
	udidLen := int(buf[43])
	if len(buf) != cookieFixedLen+udidLen {
		return Cookie{}, false
	}
	c.UDID = string(buf[cookieFixedLen:])
	return c, true
}

// cookieKeyFile is the on-disk state at <cert_dir>/cookie.key (0600).
// Independently generated — NOT derived from NodeSecret: NodeSecret is a
// Center-issued auth credential (separate blast radius, rotates on Center's
// schedule, absent entirely on users_file standalone nodes). Spec §6.7.
type cookieKeyFile struct {
	KeyID     uint8  `json:"key_id"`
	Key       []byte `json:"key"` // 32 bytes
	RotatedAt int64  `json:"rotated_at"`
	PrevKeyID uint8  `json:"prev_key_id,omitempty"`
	PrevKey   []byte `json:"prev_key,omitempty"`
}

// CookieSigner issues and verifies node-local cookies.
//
// Concurrency: mu is a STATE lock (fields only). rotateMu is a SERIALIZATION
// lock held across the rotation's file write; it is never acquired while
// holding mu. Ordering: CookieSigner.rotateMu → CookieSigner.mu (standalone —
// no nesting with any engine/server lock). Registered in k2/CLAUDE.md.
type CookieSigner struct {
	path string
	now  func() time.Time // injectable clock (tests); time.Now in production

	rotateMu  deadlock.Mutex // serialization: at most one rotation; file I/O allowed
	mu        deadlock.Mutex // state
	keyID     uint8
	key       [32]byte
	rotatedAt time.Time
	prevID    uint8
	prevKey   [32]byte
	hasPrev   bool
}

// NewCookieSigner loads dir/cookie.key or generates and persists a fresh key.
func NewCookieSigner(dir string) (*CookieSigner, error) {
	s := &CookieSigner{
		path: filepath.Join(dir, cookieKeyFileName),
		now:  time.Now,
	}
	data, err := os.ReadFile(s.path)
	switch {
	case err == nil:
		var f cookieKeyFile
		if jerr := json.Unmarshal(data, &f); jerr != nil || len(f.Key) != 32 {
			return nil, fmt.Errorf("wire: cookie key %s corrupt: %v", s.path, jerr)
		}
		s.keyID = f.KeyID
		copy(s.key[:], f.Key)
		s.rotatedAt = time.Unix(f.RotatedAt, 0)
		if len(f.PrevKey) == 32 {
			s.prevID = f.PrevKeyID
			copy(s.prevKey[:], f.PrevKey)
			s.hasPrev = true
		}
		return s, nil
	case os.IsNotExist(err):
		var key [32]byte
		if _, rerr := rand.Read(key[:]); rerr != nil {
			return nil, fmt.Errorf("wire: cookie key gen: %w", rerr)
		}
		s.keyID = 1
		s.key = key
		s.rotatedAt = s.now()
		if werr := s.persist(); werr != nil {
			return nil, werr
		}
		return s, nil
	default:
		return nil, fmt.Errorf("wire: cookie key read: %w", err)
	}
}

// persist snapshots state under mu, then writes with NO state lock held
// (file I/O — concurrency Rule #2). rotateMu may be held by the caller.
func (s *CookieSigner) persist() error {
	s.mu.Lock()
	f := cookieKeyFile{KeyID: s.keyID, Key: append([]byte(nil), s.key[:]...), RotatedAt: s.rotatedAt.Unix()}
	if s.hasPrev {
		f.PrevKeyID = s.prevID
		f.PrevKey = append([]byte(nil), s.prevKey[:]...)
	}
	s.mu.Unlock()
	data, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("wire: cookie key marshal: %w", err)
	}
	if err := os.WriteFile(s.path, data, 0600); err != nil {
		return fmt.Errorf("wire: cookie key write: %w", err)
	}
	return nil
}

// maybeRotate rotates the signing key when the 30-day period has elapsed.
// Lazy (checked on Issue) — no background goroutine to manage.
func (s *CookieSigner) maybeRotate() error {
	s.mu.Lock()
	due := s.now().Sub(s.rotatedAt) >= cookieKeyRotatePeriod
	s.mu.Unlock()
	if !due {
		return nil
	}
	s.rotateMu.Lock()
	defer s.rotateMu.Unlock()
	// Re-check: another Issue may have rotated while we waited on rotateMu.
	s.mu.Lock()
	if s.now().Sub(s.rotatedAt) < cookieKeyRotatePeriod {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		return fmt.Errorf("wire: cookie key rotate: %w", err)
	}
	s.mu.Lock()
	s.prevID = s.keyID
	s.prevKey = s.key
	s.hasPrev = true
	s.keyID++ // uint8 wrap is fine: only equality is ever checked
	s.key = key
	s.rotatedAt = s.now()
	newID := s.keyID
	s.mu.Unlock()
	slog.Info("wire: cookie signing key rotated", "keyID", newID)
	return s.persist()
}

// Issue signs c with the current key. Ver and KeyID are stamped here.
func (s *CookieSigner) Issue(c Cookie) (string, error) {
	if err := s.maybeRotate(); err != nil {
		return "", err
	}
	s.mu.Lock()
	c.Ver = cookieVersion
	c.KeyID = s.keyID
	key := s.key
	s.mu.Unlock()

	payload, err := c.marshalPayload()
	if err != nil {
		return "", err
	}
	mac := cookieMAC(key, payload)
	return base64.RawURLEncoding.EncodeToString(append(payload, mac...)), nil
}

// Verify checks signature and expiry: exp = min(AuthAt+cookieTTL, SvcExp);
// SvcExp==0 (no subscription clock) does not participate in the min.
// The previous key verifies only within cookieKeyOverlap of the rotation.
func (s *CookieSigner) Verify(raw string, now time.Time) (Cookie, bool) {
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(data) <= cookieMACLen {
		return Cookie{}, false
	}
	payload, mac := data[:len(data)-cookieMACLen], data[len(data)-cookieMACLen:]
	c, ok := parseCookiePayload(payload)
	if !ok {
		return Cookie{}, false
	}

	s.mu.Lock()
	keyID, key := s.keyID, s.key
	prevID, prevKey, hasPrev := s.prevID, s.prevKey, s.hasPrev
	rotatedAt := s.rotatedAt
	s.mu.Unlock()

	var signKey [32]byte
	switch {
	case c.KeyID == keyID:
		signKey = key
	case hasPrev && c.KeyID == prevID && now.Sub(rotatedAt) <= cookieKeyOverlap:
		signKey = prevKey
	default:
		return Cookie{}, false
	}
	if !hmac.Equal(mac, cookieMAC(signKey, payload)) {
		return Cookie{}, false
	}

	exp := time.Unix(c.AuthAt, 0).Add(cookieTTL)
	if c.SvcExp != 0 {
		if svc := time.Unix(c.SvcExp, 0); svc.Before(exp) {
			exp = svc
		}
	}
	if !now.Before(exp) {
		return Cookie{}, false
	}
	return c, true
}

func cookieMAC(key [32]byte, payload []byte) []byte {
	h := hmac.New(sha256.New, key[:])
	h.Write(payload)
	return h.Sum(nil)[:cookieMACLen]
}
