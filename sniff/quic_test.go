package sniff

import (
	"bytes"
	"crypto"
	"crypto/aes"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"net"
	"sync"
	"testing"

	"golang.org/x/crypto/hkdf"
)

func TestSniffQUIC(t *testing.T) {
	packet := buildTestQUICInitial(t, "example.org")
	sni := SniffQUIC(packet)
	if sni != "example.org" {
		t.Fatalf("expected SNI 'example.org', got %q", sni)
	}
}

func TestSniffQUIC_GoogleCom(t *testing.T) {
	packet := buildTestQUICInitial(t, "google.com")
	sni := SniffQUIC(packet)
	if sni != "google.com" {
		t.Fatalf("expected SNI 'google.com', got %q", sni)
	}
}

func TestSniffQUIC_TooShort(t *testing.T) {
	sni := SniffQUIC([]byte{0xc0, 0x00, 0x00})
	if sni != "" {
		t.Fatalf("expected empty SNI for truncated packet, got %q", sni)
	}
}

func TestSniffQUIC_NotQUIC(t *testing.T) {
	sni := SniffQUIC([]byte("This is not a QUIC packet at all and has no structure"))
	if sni != "" {
		t.Fatalf("expected empty SNI for non-QUIC data, got %q", sni)
	}
}

func TestSniffQUIC_Empty(t *testing.T) {
	sni := SniffQUIC(nil)
	if sni != "" {
		t.Fatalf("expected empty SNI for nil data, got %q", sni)
	}
}

// buildTestQUICInitial constructs a valid QUIC v1 Initial packet containing
// a TLS ClientHello with the given SNI, encrypted with proper QUIC Initial keys.
func buildTestQUICInitial(t *testing.T, sni string) []byte {
	t.Helper()

	// 1. Capture a real TLS ClientHello with the desired SNI.
	clientHello := captureTLSClientHello(t, sni)

	// 2. Wrap in a single QUIC CRYPTO frame.
	var cryptoFrame bytes.Buffer
	cryptoFrame.WriteByte(0x06) // CRYPTO frame type
	writeQuicVarint(&cryptoFrame, 0) // offset = 0
	writeQuicVarint(&cryptoFrame, uint64(len(clientHello)))
	cryptoFrame.Write(clientHello)

	// 3. Pad to at least 1200 bytes of payload (QUIC Initial requirement).
	payload := cryptoFrame.Bytes()
	minPayload := 1200
	if len(payload) < minPayload {
		padding := make([]byte, minPayload-len(payload))
		payload = append(payload, padding...) // PADDING frames = 0x00 bytes
	}

	// 4. Build QUIC v1 Initial header.
	destConnID := make([]byte, 8)
	rand.Read(destConnID)

	var header bytes.Buffer
	// First byte: Long header (0x80) + Fixed bit (0x40) + Initial type (0x00) + PN length 0 (= 1 byte)
	header.WriteByte(0xc0)
	binary.Write(&header, binary.BigEndian, uint32(quicVersion1))
	header.WriteByte(byte(len(destConnID)))
	header.Write(destConnID)
	header.WriteByte(0) // src conn ID length
	writeQuicVarint(&header, 0) // token length
	// Packet length = 1 (PN) + len(payload) + 16 (AEAD tag)
	pktLen := 1 + len(payload) + 16
	writeQuicVarint(&header, uint64(pktLen))
	header.WriteByte(0) // packet number = 0 (1 byte, unprotected for now)

	hdrLen := header.Len()

	// 5. Encrypt payload with QUIC Initial keys.
	salt := quicSaltV1
	initialSecret := hkdf.Extract(crypto.SHA256.New, destConnID, salt)
	secret := quicHKDFExpandLabel(crypto.SHA256, initialSecret, []byte{}, "client in", crypto.SHA256.Size())
	key := quicHKDFExpandLabel(crypto.SHA256, secret, []byte{}, "quic key", 16)
	iv := quicHKDFExpandLabel(crypto.SHA256, secret, []byte{}, "quic iv", 12)
	hpKey := quicHKDFExpandLabel(crypto.SHA256, secret, []byte{}, "quic hp", 16)

	ciph := quicAEADAESGCM(key, iv)
	nonce := make([]byte, ciph.NonceSize()) // all zeros for PN=0
	aad := header.Bytes()
	encrypted := ciph.Seal(nil, nonce, payload, aad)

	// 6. Build full packet (header + encrypted payload).
	packet := make([]byte, 0, hdrLen+len(encrypted))
	packet = append(packet, aad...)
	packet = append(packet, encrypted...)

	// 7. Apply header protection.
	// Sample starts 4 bytes after PN start (PN is last byte of header)
	sampleOffset := hdrLen + 3 // 4 bytes into payload (PN length adjustment: PN is 1 byte, so +3 more from encrypted)
	if sampleOffset+16 > len(packet) {
		t.Fatal("packet too short for HP sample")
	}
	sample := packet[sampleOffset : sampleOffset+16]
	block, _ := aes.NewCipher(hpKey)
	mask := make([]byte, aes.BlockSize)
	block.Encrypt(mask, sample)

	// Protect first byte (lower 4 bits for long header)
	packet[0] ^= mask[0] & 0x0f
	// Protect packet number (1 byte at hdrLen-1)
	packet[hdrLen-1] ^= mask[1]

	return packet
}

// captureTLSClientHello starts a TCP listener and captures the raw TLS
// ClientHello bytes sent by crypto/tls with the given server name.
func captureTLSClientHello(t *testing.T, serverName string) []byte {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var hello []byte
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 4096)
		n, _ := conn.Read(buf)
		if n > 0 {
			hello = make([]byte, n)
			copy(hello, buf[:n])
		}
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	tlsConn := tls.Client(conn, &tls.Config{
		ServerName:         serverName,
		InsecureSkipVerify: true,
	})
	_ = tlsConn.Handshake() // Will fail, but ClientHello is sent
	tlsConn.Close()
	wg.Wait()

	if len(hello) == 0 {
		t.Fatal("failed to capture TLS ClientHello")
	}

	// Strip the TLS record header (5 bytes: type + version + length)
	if len(hello) < 5 || hello[0] != 0x16 {
		t.Fatalf("captured data is not a TLS Handshake record: %s", hex.EncodeToString(hello[:min(10, len(hello))]))
	}
	recordLen := int(binary.BigEndian.Uint16(hello[3:5]))
	if 5+recordLen > len(hello) {
		t.Fatal("TLS record length exceeds captured data")
	}
	return hello[5 : 5+recordLen] // raw ClientHello handshake message
}

// writeQuicVarint writes a QUIC variable-length integer.
func writeQuicVarint(w *bytes.Buffer, v uint64) {
	if v <= 63 {
		w.WriteByte(byte(v))
	} else if v <= 16383 {
		w.WriteByte(byte(v>>8) | 0x40)
		w.WriteByte(byte(v))
	} else if v <= 1073741823 {
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, uint32(v)|0x80000000)
		w.Write(b)
	} else {
		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, v|0xC000000000000000)
		w.Write(b)
	}
}

