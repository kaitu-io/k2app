package wire

import (
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	utls "github.com/metacubex/utls"
)

// ParseFingerprint maps the fp= URL parameter to a utls ClientHelloID.
func ParseFingerprint(fp string) utls.ClientHelloID {
	switch fp {
	case "firefox":
		return utls.HelloFirefox_120
	case "safari":
		return utls.HelloSafari_16_0
	case "random":
		return utls.HelloRandomizedALPN
	default: // "chrome", "", or unknown
		return utls.HelloChrome_120_PQ
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
func BuildClientTLSConfig(cfg Config) (*tls.Config, utls.ClientHelloID, error) {
	echBytes, err := ParseECHConfig(cfg.ECH)
	if err != nil {
		return nil, utls.ClientHelloID{}, err
	}
	if len(echBytes) == 0 {
		return nil, utls.ClientHelloID{}, errors.New("wire: ECH config required (k2v5 never exposes real SNI)")
	}

	tlsCfg := &tls.Config{
		MinVersion:                       tls.VersionTLS13,
		ServerName:                       cfg.Host, // Inner SNI (real, encrypted by ECH)
		EncryptedClientHelloConfigList:   echBytes, // Contains outer SNI
		InsecureSkipVerify:               false,
	}

	clientHelloID := ParseFingerprint(cfg.Fingerprint)
	return tlsCfg, clientHelloID, nil
}

// PeekClientHelloSNI extracts the SNI (Server Name Indication) from a raw TLS ClientHello.
// This reads the outer SNI (plaintext) from the TLS record without consuming the connection.
// Used server-side to route ECH vs non-ECH connections.
func PeekClientHelloSNI(data []byte) (string, error) {
	if len(data) < 5 {
		return "", errors.New("wire: data too short for TLS record")
	}

	// TLS record: [type:1][version:2][length:2][handshake...]
	if data[0] != 0x16 { // ContentType: Handshake
		return "", errors.New("wire: not a TLS handshake record")
	}
	recordLen := int(data[3])<<8 | int(data[4])
	if len(data) < 5+recordLen {
		return "", errors.New("wire: TLS record truncated")
	}
	handshake := data[5 : 5+recordLen]

	// Handshake: [type:1][length:3][body...]
	if len(handshake) < 4 || handshake[0] != 0x01 { // ClientHello
		return "", errors.New("wire: not a ClientHello")
	}
	bodyLen := int(handshake[1])<<16 | int(handshake[2])<<8 | int(handshake[3])
	if len(handshake) < 4+bodyLen {
		return "", errors.New("wire: ClientHello body truncated")
	}
	body := handshake[4 : 4+bodyLen]

	// ClientHello body:
	// [version:2][random:32][session_id_len:1][session_id][cipher_suites_len:2][...][compression_len:1][...][extensions_len:2][extensions...]
	pos := 2 + 32 // version + random
	if pos >= len(body) {
		return "", errors.New("wire: ClientHello too short")
	}

	// Session ID
	sidLen := int(body[pos])
	pos += 1 + sidLen

	// Cipher suites
	if pos+2 > len(body) {
		return "", errors.New("wire: cipher suites length missing")
	}
	csLen := int(body[pos])<<8 | int(body[pos+1])
	pos += 2 + csLen

	// Compression methods
	if pos >= len(body) {
		return "", errors.New("wire: compression methods missing")
	}
	cmLen := int(body[pos])
	pos += 1 + cmLen

	// Extensions
	if pos+2 > len(body) {
		return "", errors.New("wire: extensions length missing")
	}
	extsLen := int(body[pos])<<8 | int(body[pos+1])
	pos += 2
	extsEnd := pos + extsLen

	for pos+4 <= extsEnd {
		extType := int(body[pos])<<8 | int(body[pos+1])
		extLen := int(body[pos+2])<<8 | int(body[pos+3])
		pos += 4

		if extType == 0x0000 { // server_name
			return parseSNIExtension(body[pos : pos+extLen])
		}
		pos += extLen
	}

	return "", errors.New("wire: no SNI extension found")
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
		log.Printf("wire: reverse proxy dial %s: %v", target, err)
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
		io.Copy(remote, client)
		remote.(*net.TCPConn).CloseWrite()
		done <- struct{}{}
	}()

	io.Copy(client, remote)
	client.(*net.TCPConn).CloseWrite()
	<-done
}
