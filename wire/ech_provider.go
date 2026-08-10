package wire

import (
	"crypto/ecdh"
	"crypto/rand"
	tls "github.com/metacubex/utls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"

	"github.com/miekg/dns"
)

// ECHTemplate holds fields extracted from a real ECH config.
type ECHTemplate struct {
	ConfigID     uint8  `json:"config_id"`
	KemID        uint16 `json:"kem_id"`
	CipherSuites []byte `json:"cipher_suites"` // raw cipher suite bytes
	PublicName   string `json:"public_name"`
	MaxNameLen   uint8  `json:"max_name_len"`
}

// ECHProvider fetches the current ECH config template from an external source.
type ECHProvider interface {
	FetchTemplate() (*ECHTemplate, error)
}

// LocalECHProvider queries DNS HTTPS records to get Cloudflare's ECH config.
type LocalECHProvider struct {
	// DNSServer is the DNS server to query. Default: "1.1.1.1:53"
	DNSServer string
	// Domain is the domain to query HTTPS records for. Default: "crypto.cloudflare.com"
	Domain string
}

// FetchTemplate queries DNS for HTTPS records and extracts the ECH config template.
func (p *LocalECHProvider) FetchTemplate() (*ECHTemplate, error) {
	server := p.DNSServer
	if server == "" {
		server = "1.1.1.1:53"
	}
	domain := p.Domain
	if domain == "" {
		domain = "crypto.cloudflare.com"
	}

	// Ensure domain ends with a dot for DNS query
	if domain[len(domain)-1] != '.' {
		domain += "."
	}

	c := new(dns.Client)
	msg := new(dns.Msg)
	msg.SetQuestion(domain, dns.TypeHTTPS)
	msg.RecursionDesired = true

	resp, _, err := c.Exchange(msg, server)
	if err != nil {
		return nil, fmt.Errorf("wire: DNS query for HTTPS record: %w", err)
	}
	if resp.Rcode != dns.RcodeSuccess {
		return nil, fmt.Errorf("wire: DNS HTTPS query returned rcode %d", resp.Rcode)
	}

	for _, ans := range resp.Answer {
		https, ok := ans.(*dns.HTTPS)
		if !ok {
			continue
		}
		for _, kv := range https.Value {
			ech, ok := kv.(*dns.SVCBECHConfig)
			if !ok {
				continue
			}
			if len(ech.ECH) == 0 {
				continue
			}
			tmpl, err := ParseECHConfigBytes(ech.ECH)
			if err != nil {
				return nil, fmt.Errorf("wire: parse ECH config from DNS: %w", err)
			}
			return tmpl, nil
		}
	}

	return nil, fmt.Errorf("wire: no ECH config found in HTTPS record for %s", domain)
}

// ParseECHConfigBytes parses binary ECH config data (ECHConfigList format) and
// extracts template fields from the first config entry.
//
// ECHConfigList format: [total_len:2][configs...]
// ECHConfig format: [version:2][length:2][config_id:1][kem_id:2][pub_key_len:2][pub_key:N][cs_len:2][cs:M][max_name_len:1][pub_name_len:1][pub_name:P][ext_len:2][ext:Q]
func ParseECHConfigBytes(data []byte) (*ECHTemplate, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("wire: ECH config list too short (%d bytes)", len(data))
	}

	// ECHConfigList: 2-byte total length prefix
	listLen := int(binary.BigEndian.Uint16(data[0:2]))
	data = data[2:]
	if len(data) < listLen {
		return nil, fmt.Errorf("wire: ECH config list truncated: need %d, have %d", listLen, len(data))
	}
	data = data[:listLen]

	// Parse first ECHConfig entry
	if len(data) < 4 {
		return nil, fmt.Errorf("wire: ECH config too short for version+length")
	}

	version := binary.BigEndian.Uint16(data[0:2])
	if version != 0xfe0d {
		return nil, fmt.Errorf("wire: unsupported ECH config version 0x%04x", version)
	}

	configLen := int(binary.BigEndian.Uint16(data[2:4]))
	data = data[4:]
	if len(data) < configLen {
		return nil, fmt.Errorf("wire: ECH config body truncated: need %d, have %d", configLen, len(data))
	}
	body := data[:configLen]
	pos := 0

	// config_id (1 byte)
	if pos+1 > len(body) {
		return nil, fmt.Errorf("wire: ECH config missing config_id")
	}
	configID := body[pos]
	pos++

	// kem_id (2 bytes)
	if pos+2 > len(body) {
		return nil, fmt.Errorf("wire: ECH config missing kem_id")
	}
	kemID := binary.BigEndian.Uint16(body[pos : pos+2])
	pos += 2

	// public_key_len (2 bytes) + public_key (N bytes) — skip
	if pos+2 > len(body) {
		return nil, fmt.Errorf("wire: ECH config missing public_key length")
	}
	pubKeyLen := int(binary.BigEndian.Uint16(body[pos : pos+2]))
	pos += 2
	if pos+pubKeyLen > len(body) {
		return nil, fmt.Errorf("wire: ECH config public_key truncated")
	}
	pos += pubKeyLen // skip public key bytes

	// cipher_suites_len (2 bytes) + cipher_suites (M bytes)
	if pos+2 > len(body) {
		return nil, fmt.Errorf("wire: ECH config missing cipher_suites length")
	}
	csLen := int(binary.BigEndian.Uint16(body[pos : pos+2]))
	pos += 2
	if pos+csLen > len(body) {
		return nil, fmt.Errorf("wire: ECH config cipher_suites truncated")
	}
	cipherSuites := make([]byte, csLen)
	copy(cipherSuites, body[pos:pos+csLen])
	pos += csLen

	// max_name_len (1 byte)
	if pos+1 > len(body) {
		return nil, fmt.Errorf("wire: ECH config missing max_name_len")
	}
	maxNameLen := body[pos]
	pos++

	// public_name_len (1 byte) + public_name (P bytes)
	if pos+1 > len(body) {
		return nil, fmt.Errorf("wire: ECH config missing public_name length")
	}
	pubNameLen := int(body[pos])
	pos++
	if pos+pubNameLen > len(body) {
		return nil, fmt.Errorf("wire: ECH config public_name truncated")
	}
	publicName := string(body[pos : pos+pubNameLen])

	return &ECHTemplate{
		ConfigID:     configID,
		KemID:        kemID,
		CipherSuites: cipherSuites,
		PublicName:   publicName,
		MaxNameLen:   maxNameLen,
	}, nil
}

// GenerateECHKeyFromTemplate builds a forged ECH config using template fields
// from a real ECH config (e.g., Cloudflare's). The config_id is set to
// template's config_id + 1 to avoid collisions while staying plausible.
// Returns the EncryptedClientHelloKey and the ECHConfigList bytes.
func GenerateECHKeyFromTemplate(tmpl *ECHTemplate, privKey *ecdh.PrivateKey) (tls.EncryptedClientHelloKey, []byte, error) {
	if tmpl == nil {
		return tls.EncryptedClientHelloKey{}, nil, fmt.Errorf("wire: nil ECH template")
	}
	if privKey == nil {
		return tls.EncryptedClientHelloKey{}, nil, fmt.Errorf("wire: nil private key")
	}

	pubKeyBytes := privKey.PublicKey().Bytes()
	privKeyBytes := privKey.Bytes()
	publicNameBytes := []byte(tmpl.PublicName)

	// Forged config_id: template + 1 (wrap at 255)
	configID := tmpl.ConfigID + 1

	// Calculate inner config length:
	// config_id(1) + kem_id(2) + pub_key_len(2) + pub_key(N) + cs_len(2) + cs(M) + max_name_len(1) + pub_name_len(1) + pub_name(P) + ext_len(2)
	innerLen := 1 + 2 + 2 + len(pubKeyBytes) + 2 + len(tmpl.CipherSuites) + 1 + 1 + len(publicNameBytes) + 2

	echConfig := make([]byte, 0, 4+innerLen)

	// Version: 0xfe0d
	echConfig = append(echConfig, 0xfe, 0x0d)

	// Length of remaining config
	echConfig = append(echConfig, byte(innerLen>>8), byte(innerLen))

	// Config ID (forged: template + 1)
	echConfig = append(echConfig, configID)

	// KEM ID (copy from template)
	echConfig = append(echConfig, byte(tmpl.KemID>>8), byte(tmpl.KemID))

	// Public key length + public key
	echConfig = append(echConfig, byte(len(pubKeyBytes)>>8), byte(len(pubKeyBytes)))
	echConfig = append(echConfig, pubKeyBytes...)

	// Cipher suites length + cipher suites (copy from template)
	echConfig = append(echConfig, byte(len(tmpl.CipherSuites)>>8), byte(len(tmpl.CipherSuites)))
	echConfig = append(echConfig, tmpl.CipherSuites...)

	// Maximum name length (copy from template)
	echConfig = append(echConfig, tmpl.MaxNameLen)

	// Public name length + public name (copy from template)
	echConfig = append(echConfig, byte(len(publicNameBytes)))
	echConfig = append(echConfig, publicNameBytes...)

	// Extensions length: 0
	echConfig = append(echConfig, 0x00, 0x00)

	// Build ECHConfigList (wraps the single config)
	echConfigList := make([]byte, 2+len(echConfig))
	binary.BigEndian.PutUint16(echConfigList[0:2], uint16(len(echConfig)))
	copy(echConfigList[2:], echConfig)

	key := tls.EncryptedClientHelloKey{
		Config:     echConfig,
		PrivateKey: privKeyBytes,
	}

	return key, echConfigList, nil
}

// FetchTemplateWithFallback tries to fetch an ECH template from the provider,
// caching the result on success. On failure, it falls back to the cache file,
// and finally to hardcoded defaults.
func FetchTemplateWithFallback(provider ECHProvider, cachePath string) (*ECHTemplate, error) {
	// Try live fetch
	tmpl, err := provider.FetchTemplate()
	if err == nil {
		// Cache to disk
		if cachePath != "" {
			if cacheErr := cacheTemplate(cachePath, tmpl); cacheErr != nil {
				// Non-fatal: log but don't fail
				fmt.Fprintf(os.Stderr, "wire: cache ECH template: %v\n", cacheErr)
			}
		}
		return tmpl, nil
	}

	// Try loading from cache
	if cachePath != "" {
		cached, cacheErr := loadCachedTemplate(cachePath)
		if cacheErr == nil {
			return cached, nil
		}
	}

	// Hardcoded defaults (Cloudflare-like)
	return &ECHTemplate{
		ConfigID: 97,
		KemID:    0x0020, // DHKEM(X25519, HKDF-SHA256)
		CipherSuites: []byte{
			0x00, 0x01, // HKDF-SHA256
			0x00, 0x01, // AES-128-GCM
		},
		PublicName: "cloudflare-ech.com",
		MaxNameLen: 0,
	}, nil
}

// GenerateECHKeyForged is a convenience function that fetches a template with
// fallback and generates a forged ECH key in one call.
func GenerateECHKeyForged(provider ECHProvider, cachePath string) (tls.EncryptedClientHelloKey, []byte, error) {
	tmpl, err := FetchTemplateWithFallback(provider, cachePath)
	if err != nil {
		return tls.EncryptedClientHelloKey{}, nil, fmt.Errorf("wire: fetch ECH template: %w", err)
	}

	privKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return tls.EncryptedClientHelloKey{}, nil, fmt.Errorf("wire: generate X25519 key: %w", err)
	}

	return GenerateECHKeyFromTemplate(tmpl, privKey)
}

func cacheTemplate(path string, tmpl *ECHTemplate) error {
	data, err := json.Marshal(tmpl)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}

func loadCachedTemplate(path string) (*ECHTemplate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	var tmpl ECHTemplate
	if err := json.Unmarshal(data, &tmpl); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &tmpl, nil
}
