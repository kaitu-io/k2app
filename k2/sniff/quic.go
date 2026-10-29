package sniff

import (
	"bytes"
	"crypto"
	"crypto/aes"
	"encoding/binary"
	"io"
	"sync"

	"golang.org/x/crypto/hkdf"
)

var quicBufPool = sync.Pool{New: func() any { b := make([]byte, 4096); return &b }}

// IsQUIC reports whether packet is a QUIC long header (Initial/0-RTT/Handshake).
// O(1) check: header form + fixed bit + known version. No decryption.
func IsQUIC(packet []byte) bool {
	if len(packet) < 5 {
		return false
	}
	// Long header: Form bit (7) = 1, Fixed bit (6) = 1
	if packet[0]&0xC0 != 0xC0 {
		return false
	}
	version := binary.BigEndian.Uint32(packet[1:5])
	switch version {
	case quicVersion1, quicVersion2, quicVersionDraft29:
		return true
	}
	return false
}

// SniffQUIC extracts the SNI and identifies the QUIC client from a QUIC Initial
// packet's TLS ClientHello. Returns the server name (or "") and the identified
// client. If parsing fails, returns ("", QUICClientUnknown).
// Ported from github.com/sagernet/sing-box/common/sniff/quic.go.
func SniffQUIC(packet []byte) (string, QUICClient) {
	reader := bytes.NewReader(packet)

	// 1. Parse QUIC long header
	typeByte, err := reader.ReadByte()
	if err != nil {
		return "", QUICClientUnknown
	}
	if typeByte&0x40 == 0 { // Fixed bit must be 1 for long header
		return "", QUICClientUnknown
	}

	var versionNumber uint32
	if err := binary.Read(reader, binary.BigEndian, &versionNumber); err != nil {
		return "", QUICClientUnknown
	}
	if versionNumber != quicVersionDraft29 && versionNumber != quicVersion1 && versionNumber != quicVersion2 {
		return "", QUICClientUnknown
	}

	// Validate packet type (Initial)
	packetType := (typeByte & 0x30) >> 4
	if packetType == 0 && versionNumber == quicVersion2 || packetType == 2 && versionNumber != quicVersion2 || packetType > 2 {
		return "", QUICClientUnknown
	}

	// Destination Connection ID
	destConnIDLen, err := reader.ReadByte()
	if err != nil {
		return "", QUICClientUnknown
	}
	if destConnIDLen == 0 || destConnIDLen > 20 {
		return "", QUICClientUnknown
	}
	var destConnIDBuf [20]byte // max QUIC CID is 20 bytes
	destConnID := destConnIDBuf[:destConnIDLen]
	if _, err := io.ReadFull(reader, destConnID); err != nil {
		return "", QUICClientUnknown
	}

	// Source Connection ID (skip)
	srcConnIDLen, err := reader.ReadByte()
	if err != nil {
		return "", QUICClientUnknown
	}
	if _, err := io.CopyN(io.Discard, reader, int64(srcConnIDLen)); err != nil {
		return "", QUICClientUnknown
	}

	// Token (skip)
	tokenLen, err := quicReadUvarint(reader)
	if err != nil {
		return "", QUICClientUnknown
	}
	if _, err := io.CopyN(io.Discard, reader, int64(tokenLen)); err != nil {
		return "", QUICClientUnknown
	}

	// Packet length
	packetLen, err := quicReadUvarint(reader)
	if err != nil {
		return "", QUICClientUnknown
	}

	hdrLen := int(reader.Size()) - reader.Len()
	if hdrLen+int(packetLen) > len(packet) {
		return "", QUICClientUnknown
	}

	// 2. Read sample for header protection (4 bytes PN + 16 bytes sample)
	if _, err := io.CopyN(io.Discard, reader, 4); err != nil {
		return "", QUICClientUnknown
	}
	var pnBytesBuf [aes.BlockSize]byte
	pnBytes := pnBytesBuf[:]
	if _, err := io.ReadFull(reader, pnBytes); err != nil {
		return "", QUICClientUnknown
	}

	// 3. Derive initial secret and header protection key
	var salt []byte
	switch versionNumber {
	case quicVersion1:
		salt = quicSaltV1
	case quicVersion2:
		salt = quicSaltV2
	default:
		salt = quicSaltOld
	}
	var hpLabel string
	switch versionNumber {
	case quicVersion2:
		hpLabel = hkdfLabelHeaderProtectionV2
	default:
		hpLabel = hkdfLabelHeaderProtectionV1
	}

	initialSecret := hkdf.Extract(crypto.SHA256.New, destConnID, salt)
	secret, err := quicHKDFExpandLabel(crypto.SHA256, initialSecret, []byte{}, "client in", crypto.SHA256.Size())
	if err != nil {
		return "", QUICClientUnknown
	}
	hpKey, err := quicHKDFExpandLabel(crypto.SHA256, secret, []byte{}, hpLabel, 16)
	if err != nil {
		return "", QUICClientUnknown
	}

	// 4. Remove header protection
	block, err := aes.NewCipher(hpKey)
	if err != nil {
		return "", QUICClientUnknown
	}
	var maskBuf [aes.BlockSize]byte
	mask := maskBuf[:]
	block.Encrypt(mask, pnBytes)

	bp := quicBufPool.Get().(*[]byte)
	defer quicBufPool.Put(bp)
	if len(packet) > len(*bp) {
		return "", QUICClientUnknown
	}
	newPacket := (*bp)[:len(packet)]
	copy(newPacket, packet)
	newPacket[0] ^= mask[0] & 0xf
	for i := range newPacket[hdrLen : hdrLen+4] {
		newPacket[hdrLen+i] ^= mask[i+1]
	}

	packetNumberLength := newPacket[0]&0x3 + 1
	if hdrLen+int(packetNumberLength) > int(packetLen)+hdrLen {
		return "", QUICClientUnknown
	}

	var packetNumber uint32
	switch packetNumberLength {
	case 1:
		packetNumber = uint32(newPacket[hdrLen])
	case 2:
		packetNumber = uint32(binary.BigEndian.Uint16(newPacket[hdrLen:]))
	case 3:
		packetNumber = uint32(newPacket[hdrLen+2]) | uint32(newPacket[hdrLen+1])<<8 | uint32(newPacket[hdrLen])<<16
	case 4:
		packetNumber = binary.BigEndian.Uint32(newPacket[hdrLen:])
	default:
		return "", QUICClientUnknown
	}

	extHdrLen := hdrLen + int(packetNumberLength)
	copy(newPacket[extHdrLen:hdrLen+4], packet[extHdrLen:])
	data := newPacket[extHdrLen : int(packetLen)+hdrLen]

	// 5. Decrypt payload
	var keyLabel, ivLabel string
	switch versionNumber {
	case quicVersion2:
		keyLabel = hkdfLabelKeyV2
		ivLabel = hkdfLabelIVV2
	default:
		keyLabel = hkdfLabelKeyV1
		ivLabel = hkdfLabelIVV1
	}

	key, err := quicHKDFExpandLabel(crypto.SHA256, secret, []byte{}, keyLabel, 16)
	if err != nil {
		return "", QUICClientUnknown
	}
	iv, err := quicHKDFExpandLabel(crypto.SHA256, secret, []byte{}, ivLabel, 12)
	if err != nil {
		return "", QUICClientUnknown
	}
	ciph, err := quicAEADAESGCM(key, iv)
	if err != nil {
		return "", QUICClientUnknown
	}
	var nonceBuf [8]byte // xorNonceAEAD.NonceSize() == 8
	nonce := nonceBuf[:]
	binary.BigEndian.PutUint64(nonce, uint64(packetNumber))
	decrypted, err := ciph.Open(newPacket[extHdrLen:extHdrLen], nonce, data, newPacket[:extHdrLen])
	if err != nil {
		return "", QUICClientUnknown
	}

	// 6. Parse QUIC frames, extract CRYPTO fragments and record frame types.
	decryptedReader := bytes.NewReader(decrypted)
	const (
		frameTypePadding         = 0x00
		frameTypePing            = 0x01
		frameTypeAck             = 0x02
		frameTypeAck2            = 0x03
		frameTypeCrypto          = 0x06
		frameTypeConnectionClose = 0x1c
	)

	type cryptoFragment struct {
		offset  uint64
		length  uint64
		payload []byte
	}
	fragments := make([]cryptoFragment, 0, 4)
	frameTypes := make([]uint8, 0, 8)

	for {
		frameType, err := decryptedReader.ReadByte()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", QUICClientUnknown
		}
		switch frameType {
		case frameTypePadding:
			continue // Don't record padding
		case frameTypePing:
			frameTypes = append(frameTypes, frameType)
			continue
		case frameTypeAck, frameTypeAck2:
			frameTypes = append(frameTypes, frameType)
			if _, err := quicReadUvarint(decryptedReader); err != nil { // Largest Acknowledged
				return "", QUICClientUnknown
			}
			if _, err := quicReadUvarint(decryptedReader); err != nil { // ACK Delay
				return "", QUICClientUnknown
			}
			ackRangeCount, err := quicReadUvarint(decryptedReader) // ACK Range Count
			if err != nil {
				return "", QUICClientUnknown
			}
			if _, err := quicReadUvarint(decryptedReader); err != nil { // First ACK Range
				return "", QUICClientUnknown
			}
			for i := uint64(0); i < ackRangeCount; i++ {
				if _, err := quicReadUvarint(decryptedReader); err != nil { // Gap
					return "", QUICClientUnknown
				}
				if _, err := quicReadUvarint(decryptedReader); err != nil { // ACK Range Length
					return "", QUICClientUnknown
				}
			}
			if frameType == frameTypeAck2 {
				if _, err := quicReadUvarint(decryptedReader); err != nil { // ECT0
					return "", QUICClientUnknown
				}
				if _, err := quicReadUvarint(decryptedReader); err != nil { // ECT1
					return "", QUICClientUnknown
				}
				if _, err := quicReadUvarint(decryptedReader); err != nil { // ECN-CE
					return "", QUICClientUnknown
				}
			}
		case frameTypeCrypto:
			frameTypes = append(frameTypes, frameType)
			offset, err := quicReadUvarint(decryptedReader)
			if err != nil {
				return "", QUICClientUnknown
			}
			length, err := quicReadUvarint(decryptedReader)
			if err != nil {
				return "", QUICClientUnknown
			}
			index := len(decrypted) - decryptedReader.Len()
			if index+int(length) > len(decrypted) {
				return "", QUICClientUnknown
			}
			fragments = append(fragments, cryptoFragment{offset, length, decrypted[index : index+int(length)]})
			if _, err := decryptedReader.Seek(int64(length), io.SeekCurrent); err != nil {
				return "", QUICClientUnknown
			}
		case frameTypeConnectionClose:
			frameTypes = append(frameTypes, frameType)
			if _, err := quicReadUvarint(decryptedReader); err != nil { // Error Code
				return "", QUICClientUnknown
			}
			if _, err := quicReadUvarint(decryptedReader); err != nil { // Frame Type
				return "", QUICClientUnknown
			}
			length, err := quicReadUvarint(decryptedReader) // Reason Phrase Length
			if err != nil {
				return "", QUICClientUnknown
			}
			if _, err := decryptedReader.Seek(int64(length), io.SeekCurrent); err != nil {
				return "", QUICClientUnknown
			}
		default:
			return "", QUICClientUnknown
		}
	}

	if len(fragments) == 0 {
		return "", QUICClientUnknown
	}

	// 7. Reassemble CRYPTO fragments into a flat buffer at their stream offsets.
	// Fragments may be non-contiguous (e.g. quic-go splits ClientHello across frames
	// with small gaps). We place each fragment at its declared offset so the full
	// TLS handshake data is reconstructed even with gaps (zeros in undelivered areas).
	var maxEnd uint64
	for _, f := range fragments {
		if end := f.offset + f.length; end > maxEnd {
			maxEnd = end
		}
	}
	if maxEnd > 16384 { // TLS ClientHello max per spec
		return "", QUICClientUnknown
	}

	// Single allocation: 5-byte TLS record header + reassembled CRYPTO stream.
	buf := make([]byte, 5+maxEnd)
	buf[0] = 0x16                         // ContentType: Handshake
	buf[1] = 0x03
	buf[2] = 0x03                         // Version: TLS 1.2
	buf[3] = byte(maxEnd >> 8)
	buf[4] = byte(maxEnd)                 // Length
	for _, f := range fragments {
		copy(buf[5+f.offset:], f.payload)
	}

	// 8. Parse ClientHello for SNI and fingerprinting, identify client.
	// parseQUICClientHello handles SNI extraction directly — no need for the
	// expensive SniffTLS fallback (which creates tls.Server + context + goroutine).
	ch := parseQUICClientHello(buf)
	var sni string
	if ch != nil {
		sni = ch.ServerName
	}

	client := identifyQUICClient(frameTypes, ch)
	return sni, client
}
