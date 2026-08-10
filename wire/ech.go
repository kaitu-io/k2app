package wire

import (
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	tls "github.com/metacubex/utls"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

)

var copyBufPool = sync.Pool{New: func() any { b := make([]byte, 32*1024); return &b }}

// ParseFingerprint maps the fp= URL parameter to a utls ClientHelloID.
func ParseFingerprint(fp string) tls.ClientHelloID {
	switch fp {
	case "firefox":
		return tls.HelloFirefox_120
	case "safari":
		return tls.HelloSafari_16_0
	case "random":
		return tls.HelloRandomizedALPN
	default: // "chrome", "", or unknown
		return tls.HelloChrome_120_PQ
	}
}

// ParseECHConfig decodes a base64-encoded ECH config list.
// Returns nil without error if input is empty.
func ParseECHConfig(encoded string) ([]byte, error) {
	if encoded == "" {
		return nil, nil
	}

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		// Try URL-safe base64
		data, err = base64.URLEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("wire: invalid ECH config base64: %w", err)
		}
	}
	return data, nil
}

// BuildClientTLSConfig creates a TLS config from k2v5 Config for client connections.
// Returns the tls.Config and the utls ClientHelloID to use.
// k2v5 mandates ECH — returns error if ECH config is missing.
func BuildClientTLSConfig(cfg Config) (*tls.Config, tls.ClientHelloID, error) {
	echBytes, err := ParseECHConfig(cfg.ECH)
	if err != nil {
		return nil, tls.ClientHelloID{}, err
	}
	if len(echBytes) == 0 {
		return nil, tls.ClientHelloID{}, errors.New("wire: ECH config required (k2v5 never exposes real SNI)")
	}

	tlsCfg := &tls.Config{
		MinVersion:                       tls.VersionTLS13,
		ServerName:                       cfg.Host, // Inner SNI (real, encrypted by ECH)
		EncryptedClientHelloConfigList:   echBytes, // Contains outer SNI
	}

	// Certificate verification priority: pin > insecure > default CA pool
	if cfg.Pin != "" {
		// Pin verification: check public key hash (overrides everything)
		pins, err := ParsePins(cfg.Pin)
		if err != nil {
			return nil, tls.ClientHelloID{}, err
		}
		tlsCfg.InsecureSkipVerify = true
		tlsCfg.VerifyPeerCertificate = buildPinVerifier(pins)
	} else if cfg.Insecure {
		// Skip all verification
		tlsCfg.InsecureSkipVerify = true
	} else {
		// Default: system CA + Kaitu CA, reject Chinese CAs
		tlsCfg.RootCAs = BuildDefaultCAPool()
		tlsCfg.VerifyPeerCertificate = buildBlockedCAVerifier()
	}

	clientHelloID := ParseFingerprint(cfg.Fingerprint)
	return tlsCfg, clientHelloID, nil
}

// buildPinVerifier returns a VerifyPeerCertificate callback that checks
// the leaf certificate's public key hash against the provided pin hashes.
// As a fallback, it also accepts certificates that chain to the Kaitu CA,
// so cloud k2v5 servers using Kaitu CA signed certs are always verifiable.
func buildPinVerifier(pins [][32]byte) func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		if len(rawCerts) == 0 {
			return errors.New("wire: no certificate received")
		}
		cert, err := x509.ParseCertificate(rawCerts[0])
		if err != nil {
			return fmt.Errorf("wire: parse certificate: %w", err)
		}

		// Fast path: check pin match
		pubKeyDER, err := x509.MarshalPKIXPublicKey(cert.PublicKey)
		if err != nil {
			return fmt.Errorf("wire: marshal public key: %w", err)
		}
		hash := sha256.Sum256(pubKeyDER)
		for _, pin := range pins {
			if hash == pin {
				return nil
			}
		}

		// Fallback: check if cert chains to Kaitu CA
		intermediates := x509.NewCertPool()
		for _, rawCert := range rawCerts[1:] {
			if ic, err := x509.ParseCertificate(rawCert); err == nil {
				intermediates.AddCert(ic)
			}
		}
		opts := x509.VerifyOptions{
			Roots:         kaituCACertPool,
			Intermediates: intermediates,
		}
		if _, err := cert.Verify(opts); err == nil {
			return nil
		}

		return errors.New("wire: public key pin mismatch and not signed by Kaitu CA")
	}
}

// ClientHelloInfo contains information extracted from a TLS ClientHello.
type ClientHelloInfo struct {
	SNI    string
	HasECH bool
}

// PeekClientHello extracts SNI and ECH presence from a raw TLS ClientHello.
// Returns ClientHelloInfo with SNI (outer, plaintext) and whether ECH extension (0xfe0d) is present.
// Used server-side to route ECH vs non-ECH connections.
func PeekClientHello(data []byte) (ClientHelloInfo, error) {
	if len(data) < 5 {
		return ClientHelloInfo{}, errors.New("wire: data too short for TLS record")
	}

	// TLS record: [type:1][version:2][length:2][handshake...]
	if data[0] != 0x16 { // ContentType: Handshake
		return ClientHelloInfo{}, errors.New("wire: not a TLS handshake record")
	}
	recordLen := int(data[3])<<8 | int(data[4])
	if len(data) < 5+recordLen {
		return ClientHelloInfo{}, errors.New("wire: TLS record truncated")
	}
	handshake := data[5 : 5+recordLen]

	// Handshake: [type:1][length:3][body...]
	if len(handshake) < 4 || handshake[0] != 0x01 { // ClientHello
		return ClientHelloInfo{}, errors.New("wire: not a ClientHello")
	}
	bodyLen := int(handshake[1])<<16 | int(handshake[2])<<8 | int(handshake[3])
	if len(handshake) < 4+bodyLen {
		return ClientHelloInfo{}, errors.New("wire: ClientHello body truncated")
	}
	body := handshake[4 : 4+bodyLen]

	// ClientHello body:
	// [version:2][random:32][session_id_len:1][session_id][cipher_suites_len:2][...][compression_len:1][...][extensions_len:2][extensions...]
	pos := 2 + 32 // version + random
	if pos >= len(body) {
		return ClientHelloInfo{}, errors.New("wire: ClientHello too short")
	}

	// Session ID
	sidLen := int(body[pos])
	pos += 1 + sidLen

	// Cipher suites
	if pos+2 > len(body) {
		return ClientHelloInfo{}, errors.New("wire: cipher suites length missing")
	}
	csLen := int(body[pos])<<8 | int(body[pos+1])
	pos += 2 + csLen

	// Compression methods
	if pos >= len(body) {
		return ClientHelloInfo{}, errors.New("wire: compression methods missing")
	}
	cmLen := int(body[pos])
	pos += 1 + cmLen

	// Extensions
	if pos+2 > len(body) {
		return ClientHelloInfo{}, errors.New("wire: extensions length missing")
	}
	extsLen := int(body[pos])<<8 | int(body[pos+1])
	pos += 2
	extsEnd := pos + extsLen

	var sni string
	var hasECH bool

	for pos+4 <= extsEnd && pos+4 <= len(body) {
		extType := int(body[pos])<<8 | int(body[pos+1])
		extLen := int(body[pos+2])<<8 | int(body[pos+3])
		pos += 4

		if pos+extLen > len(body) {
			return ClientHelloInfo{}, fmt.Errorf("wire: extension length %d exceeds body size %d", extLen, len(body)-pos)
		}

		if extType == 0x0000 { // server_name
			var err error
			sni, err = parseSNIExtension(body[pos : pos+extLen])
			if err != nil {
				return ClientHelloInfo{}, err
			}
		} else if extType == 0xfe0d { // encrypted_client_hello
			hasECH = true
		}
		pos += extLen
	}

	if sni == "" {
		return ClientHelloInfo{}, errors.New("wire: no SNI extension found")
	}

	return ClientHelloInfo{SNI: sni, HasECH: hasECH}, nil
}

func parseSNIExtension(data []byte) (string, error) {
	if len(data) < 2 {
		return "", errors.New("wire: SNI extension too short")
	}
	listLen := int(data[0])<<8 | int(data[1])
	if len(data) < 2+listLen {
		return "", errors.New("wire: SNI list truncated")
	}

	pos := 2
	for pos+3 <= 2+listLen {
		nameType := data[pos]
		nameLen := int(data[pos+1])<<8 | int(data[pos+2])
		pos += 3
		if nameType == 0x00 { // host_name
			if pos+nameLen > len(data) {
				return "", errors.New("wire: SNI name truncated")
			}
			return string(data[pos : pos+nameLen]), nil
		}
		pos += nameLen
	}
	return "", errors.New("wire: no host_name in SNI extension")
}

// ReverseProxy forwards a TCP connection transparently to the target address.
// Used for non-ECH connections: proxy to the outer SNI website for camouflage.
func ReverseProxy(client net.Conn, clientData []byte, target string) {
	defer client.Close()

	remote, err := net.DialTimeout("tcp", target, 10*time.Second)
	if err != nil {
		slog.Warn("wire: reverse proxy dial failed", "target", target, "err", err)
		return
	}
	defer remote.Close()

	// Forward the already-read ClientHello data
	if len(clientData) > 0 {
		if _, err := remote.Write(clientData); err != nil {
			return
		}
	}

	// Bidirectional forwarding (qTunnel pattern: two goroutines + channel + half-close)
	done := make(chan struct{})
	go func() {
		bp := copyBufPool.Get().(*[]byte)
		io.CopyBuffer(remote, client, *bp)
		copyBufPool.Put(bp)
		if tc, ok := remote.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
		done <- struct{}{}
	}()

	bp := copyBufPool.Get().(*[]byte)
	io.CopyBuffer(client, remote, *bp)
	copyBufPool.Put(bp)
	if tc, ok := client.(*net.TCPConn); ok {
		tc.CloseWrite()
	}
	<-done
}

// ReverseProxyWithTimeout forwards a TCP connection with idle timeout and max duration.
// idleTimeout closes the connection if no data is transferred for this duration.
// maxDuration is the absolute maximum lifetime of the proxied connection.
func ReverseProxyWithTimeout(client net.Conn, clientData []byte, target string, idleTimeout, maxDuration time.Duration) {
	defer client.Close()

	remote, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		slog.Warn("wire: reverse proxy dial failed", "target", target, "err", err)
		return
	}
	defer remote.Close()

	// Absolute max duration deadline.
	deadline := time.Now().Add(maxDuration)
	client.SetDeadline(deadline)
	remote.SetDeadline(deadline)

	// Forward the already-read ClientHello data.
	if len(clientData) > 0 {
		if _, err := remote.Write(clientData); err != nil {
			return
		}
	}

	// Bidirectional forwarding with idle timeout.
	done := make(chan struct{})
	go func() {
		bp := copyBufPool.Get().(*[]byte)
		copyWithIdleTimeout(remote, client, *bp, idleTimeout)
		copyBufPool.Put(bp)
		if tc, ok := remote.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
		done <- struct{}{}
	}()

	bp := copyBufPool.Get().(*[]byte)
	copyWithIdleTimeout(client, remote, *bp, idleTimeout)
	copyBufPool.Put(bp)
	if tc, ok := client.(*net.TCPConn); ok {
		tc.CloseWrite()
	}
	<-done
}

// copyWithIdleTimeout copies from src to dst, resetting the read deadline on each successful read.
func copyWithIdleTimeout(dst net.Conn, src net.Conn, buf []byte, idle time.Duration) {
	for {
		src.SetReadDeadline(time.Now().Add(idle))
		n, err := src.Read(buf)
		if n > 0 {
			if _, wErr := dst.Write(buf[:n]); wErr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// GenerateECHKey generates a new ECH key pair and config for the given public name.
// Returns the EncryptedClientHelloKey and the ECH config bytes (to be distributed to clients).
func GenerateECHKey(publicName string) (tls.EncryptedClientHelloKey, []byte, error) {
	// Generate X25519 private key (DHKEM(X25519, HKDF-SHA256) = 0x0020)
	privKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return tls.EncryptedClientHelloKey{}, nil, fmt.Errorf("wire: generate X25519 key: %w", err)
	}

	pubKeyBytes := privKey.PublicKey().Bytes()
	privKeyBytes := privKey.Bytes()

	// Build ECHConfig according to draft-ietf-tls-esni-18
	// ECHConfig structure:
	// - version (2 bytes): 0xfe0d
	// - length (2 bytes)
	// - config_id (1 byte): random ID
	// - kem_id (2 bytes): X25519 = 0x0020
	// - public_key length (2 bytes)
	// - public_key (32 bytes for X25519)
	// - cipher_suites length (2 bytes)
	// - cipher_suites (list of KDF+AEAD pairs)
	// - maximum_name_length (1 byte): 0
	// - public_name length (1 byte)
	// - public_name
	// - extensions length (2 bytes): 0

	configID := byte(1) // Use config ID 1

	// Cipher suites: HKDF-SHA256 (0x0001) + AES-128-GCM (0x0001)
	cipherSuites := []byte{
		0x00, 0x01, // KDF: HKDF-SHA256
		0x00, 0x01, // AEAD: AES-128-GCM
	}

	publicNameBytes := []byte(publicName)

	// Calculate inner config length
	// config_id(1) + kem_id(2) + pub_key_len(2) + pub_key(32) + cs_len(2) + cs(4) + max_name_len(1) + pub_name_len(1) + pub_name + ext_len(2)
	innerLen := 1 + 2 + 2 + len(pubKeyBytes) + 2 + len(cipherSuites) + 1 + 1 + len(publicNameBytes) + 2

	echConfig := make([]byte, 0, 4+innerLen)

	// Version: 0xfe0d
	echConfig = append(echConfig, 0xfe, 0x0d)

	// Length of remaining config
	echConfig = append(echConfig, byte(innerLen>>8), byte(innerLen))

	// Config ID
	echConfig = append(echConfig, configID)

	// KEM ID: X25519 (0x0020)
	echConfig = append(echConfig, 0x00, 0x20)

	// Public key length
	echConfig = append(echConfig, byte(len(pubKeyBytes)>>8), byte(len(pubKeyBytes)))
	echConfig = append(echConfig, pubKeyBytes...)

	// Cipher suites length
	echConfig = append(echConfig, byte(len(cipherSuites)>>8), byte(len(cipherSuites)))
	echConfig = append(echConfig, cipherSuites...)

	// Maximum name length: 0 (no restriction)
	echConfig = append(echConfig, 0x00)

	// Public name length
	echConfig = append(echConfig, byte(len(publicNameBytes)))
	echConfig = append(echConfig, publicNameBytes...)

	// Extensions length: 0
	echConfig = append(echConfig, 0x00, 0x00)

	// Build ECHConfigList (wraps the single config)
	// ECHConfigList: [length:2][configs...]
	echConfigList := make([]byte, 2+len(echConfig))
	binary.BigEndian.PutUint16(echConfigList[0:2], uint16(len(echConfig)))
	copy(echConfigList[2:], echConfig)

	key := tls.EncryptedClientHelloKey{
		Config:     echConfig,
		PrivateKey: privKeyBytes,
	}

	return key, echConfigList, nil
}

// SaveECHKey saves an ECH private key and config to a file.
// The file format is a simple concatenation:
// - Private key length (4 bytes, big-endian)
// - Private key bytes
// - ECH config length (4 bytes, big-endian)
// - ECH config bytes
func SaveECHKey(path string, privKey []byte, echConfig []byte) error {
	data := make([]byte, 0, 8+len(privKey)+len(echConfig))

	// Private key length
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, uint32(len(privKey)))
	data = append(data, buf...)
	data = append(data, privKey...)

	// ECH config length
	binary.BigEndian.PutUint32(buf, uint32(len(echConfig)))
	data = append(data, buf...)
	data = append(data, echConfig...)

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("wire: save ECH key: %w", err)
	}

	return nil
}

// LoadECHKey loads an ECH private key and config from a file.
func LoadECHKey(path string) (tls.EncryptedClientHelloKey, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return tls.EncryptedClientHelloKey{}, nil, fmt.Errorf("wire: load ECH key: %w", err)
	}

	if len(data) < 8 {
		return tls.EncryptedClientHelloKey{}, nil, errors.New("wire: ECH key file too short")
	}

	// Read private key
	privKeyLen := binary.BigEndian.Uint32(data[0:4])
	if len(data) < int(4+privKeyLen+4) {
		return tls.EncryptedClientHelloKey{}, nil, errors.New("wire: ECH key file truncated")
	}
	privKey := data[4 : 4+privKeyLen]

	// Read ECH config
	pos := 4 + privKeyLen
	echConfigLen := binary.BigEndian.Uint32(data[pos : pos+4])
	pos += 4
	if len(data) < int(pos+echConfigLen) {
		return tls.EncryptedClientHelloKey{}, nil, errors.New("wire: ECH config truncated")
	}
	echConfig := data[pos : pos+echConfigLen]

	// Extract single ECH config from config list
	// ECHConfigList format: [length:2][config...]
	if len(echConfig) < 2 {
		return tls.EncryptedClientHelloKey{}, nil, errors.New("wire: ECH config list too short")
	}
	configLen := binary.BigEndian.Uint16(echConfig[0:2])
	if len(echConfig) < int(2+configLen) {
		return tls.EncryptedClientHelloKey{}, nil, errors.New("wire: ECH config list truncated")
	}
	singleConfig := echConfig[2 : 2+configLen]

	key := tls.EncryptedClientHelloKey{
		Config:     singleConfig,
		PrivateKey: privKey,
	}

	return key, echConfig, nil
}

// EnsureECHKey loads or generates an ECH key.
// If the key file exists, loads it. Otherwise, generates a new key and saves it.
// Returns a slice of EncryptedClientHelloKey (Go TLS API expects a slice) and the ECH config list bytes.
func EnsureECHKey(keyPath, publicName string) ([]tls.EncryptedClientHelloKey, []byte, error) {
	// Try to load existing key
	if _, err := os.Stat(keyPath); err == nil {
		key, echConfig, err := LoadECHKey(keyPath)
		if err != nil {
			return nil, nil, err
		}
		return []tls.EncryptedClientHelloKey{key}, echConfig, nil
	}

	// Generate new key
	key, echConfig, err := GenerateECHKey(publicName)
	if err != nil {
		return nil, nil, err
	}

	// Save for future use
	if err := SaveECHKey(keyPath, key.PrivateKey, echConfig); err != nil {
		return nil, nil, err
	}

	return []tls.EncryptedClientHelloKey{key}, echConfig, nil
}

// --- ECH key rotation: split storage ---

// SaveECHPrivateKey saves a raw X25519 private key (32 bytes) to a file.
func SaveECHPrivateKey(path string, privKey []byte) error {
	if err := os.WriteFile(path, privKey, 0600); err != nil {
		return fmt.Errorf("wire: save ECH private key: %w", err)
	}
	return nil
}

// LoadECHPrivateKey loads and parses an X25519 private key from a file.
func LoadECHPrivateKey(path string) (*ecdh.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("wire: load ECH private key: %w", err)
	}
	key, err := ecdh.X25519().NewPrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("wire: parse ECH private key: %w", err)
	}
	return key, nil
}

// EnsureECHPrivateKey loads an existing ECH private key or generates a new one.
// If the file exists but is >40 bytes, it is treated as the old combined format
// (key+config blob) and is migrated: the private key is extracted and saved in
// the new 32-byte format, and the config is saved to ech-configs/ alongside it.
func EnsureECHPrivateKey(path string) (*ecdh.PrivateKey, error) {
	info, err := os.Stat(path)
	if err == nil {
		// File exists — check if old format (>40 bytes) or new format (32 bytes)
		if info.Size() > 40 {
			return migrateOldECHKey(path)
		}
		return LoadECHPrivateKey(path)
	}

	// File doesn't exist — generate new key and save
	privKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("wire: generate ECH private key: %w", err)
	}
	if err := SaveECHPrivateKey(path, privKey.Bytes()); err != nil {
		return nil, err
	}
	return privKey, nil
}

// migrateOldECHKey reads the old combined format, extracts the private key
// and ECH config, saves them in the new split format, and returns the key.
func migrateOldECHKey(path string) (*ecdh.PrivateKey, error) {
	key, echConfigList, err := LoadECHKey(path)
	if err != nil {
		return nil, fmt.Errorf("wire: migrate old ECH key: %w", err)
	}

	privKey, err := ecdh.X25519().NewPrivateKey(key.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("wire: parse migrated private key: %w", err)
	}

	// Save private key in new format (overwrite old file)
	if err := SaveECHPrivateKey(path, privKey.Bytes()); err != nil {
		return nil, err
	}

	// Extract raw ECHConfig from the ECHConfigList and save to ech-configs/
	// LoadECHKey returns key.Config as the raw ECHConfig (without list prefix)
	configsDir := filepath.Join(filepath.Dir(path), "ech-configs")
	if err := SaveECHConfig(configsDir, key.Config); err != nil {
		return nil, fmt.Errorf("wire: migrate ECH config: %w", err)
	}

	_ = echConfigList // not needed for migration
	return privKey, nil
}

// SaveECHConfig saves a raw ECHConfig to ech-configs/config-{id}.bin.
// The config_id is extracted from the raw bytes at position: version(2) + length(2) + config_id(1).
// Creates the directory if it doesn't exist.
func SaveECHConfig(dir string, config []byte) error {
	if len(config) < 5 {
		return fmt.Errorf("wire: ECH config too short to extract config_id (%d bytes)", len(config))
	}

	// Verify version
	version := binary.BigEndian.Uint16(config[0:2])
	if version != 0xfe0d {
		return fmt.Errorf("wire: unexpected ECH config version 0x%04x", version)
	}

	configID := config[4] // version(2) + length(2) + config_id(1)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("wire: create ech-configs dir: %w", err)
	}

	filename := fmt.Sprintf("config-%d.bin", configID)
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, config, 0600); err != nil {
		return fmt.Errorf("wire: save ECH config: %w", err)
	}
	return nil
}

// LoadAllECHConfigs loads all .bin files from the ech-configs directory.
// Returns configs sorted by config_id descending (latest first).
// Returns empty slice (no error) if the directory doesn't exist or is empty.
func LoadAllECHConfigs(dir string) ([][]byte, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("wire: read ech-configs dir: %w", err)
	}

	var configs [][]byte
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".bin" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("wire: read ECH config %s: %w", entry.Name(), err)
		}
		if len(data) < 5 {
			continue // skip invalid files
		}
		configs = append(configs, data)
	}

	// Sort by config_id descending (config_id is at byte offset 4)
	sort.Slice(configs, func(i, j int) bool {
		return configs[i][4] > configs[j][4]
	})

	return configs, nil
}

// BuildECHKeys converts a list of raw ECHConfig bytes into tls.EncryptedClientHelloKey entries.
// All entries share the same HPKE private key but have different configs.
func BuildECHKeys(privKey *ecdh.PrivateKey, configs [][]byte) []tls.EncryptedClientHelloKey {
	keys := make([]tls.EncryptedClientHelloKey, 0, len(configs))
	for _, cfg := range configs {
		keys = append(keys, tls.EncryptedClientHelloKey{
			Config:     cfg,
			PrivateKey: privKey.Bytes(),
		})
	}
	return keys
}

// BuildECHConfigList builds an ECHConfigList from multiple raw ECHConfig entries.
// ECHConfigList format: [total_length:2][config1...][config2...]
func BuildECHConfigList(configs [][]byte) []byte {
	totalLen := 0
	for _, cfg := range configs {
		totalLen += len(cfg)
	}

	list := make([]byte, 2+totalLen)
	binary.BigEndian.PutUint16(list[0:2], uint16(totalLen))
	pos := 2
	for _, cfg := range configs {
		copy(list[pos:], cfg)
		pos += len(cfg)
	}
	return list
}
