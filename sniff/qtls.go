// Package sniff QUIC TLS constants and crypto helpers.
// Ported from github.com/sagernet/sing-box/common/sniff/internal/qtls/qtls.go
package sniff

import (
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

const (
	quicVersionDraft29 = 0xff00001d
	quicVersion1       = 0x1
	quicVersion2       = 0x6b3343cf
)

var (
	quicSaltOld = []byte{0xaf, 0xbf, 0xec, 0x28, 0x99, 0x93, 0xd2, 0x4c, 0x9e, 0x97, 0x86, 0xf1, 0x9c, 0x61, 0x11, 0xe0, 0x43, 0x90, 0xa8, 0x99}
	quicSaltV1  = []byte{0x38, 0x76, 0x2c, 0xf7, 0xf5, 0x59, 0x34, 0xb3, 0x4d, 0x17, 0x9a, 0xe6, 0xa4, 0xc8, 0x0c, 0xad, 0xcc, 0xbb, 0x7f, 0x0a}
	quicSaltV2  = []byte{0x0d, 0xed, 0xe3, 0xde, 0xf7, 0x00, 0xa6, 0xdb, 0x81, 0x93, 0x81, 0xbe, 0x6e, 0x26, 0x9d, 0xcb, 0xf9, 0xbd, 0x2e, 0xd9}
)

const (
	hkdfLabelKeyV1              = "quic key"
	hkdfLabelKeyV2              = "quicv2 key"
	hkdfLabelIVV1               = "quic iv"
	hkdfLabelIVV2               = "quicv2 iv"
	hkdfLabelHeaderProtectionV1 = "quic hp"
	hkdfLabelHeaderProtectionV2 = "quicv2 hp"
)

func quicAEADAESGCM(key, nonceMask []byte) (cipher.AEAD, error) {
	if len(nonceMask) != 12 {
		return nil, fmt.Errorf("wrong nonce length: %d", len(nonceMask))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ret := &xorNonceAEAD{aead: aead}
	copy(ret.nonceMask[:], nonceMask)
	return ret, nil
}

type xorNonceAEAD struct {
	nonceMask [12]byte
	aead      cipher.AEAD
}

func (f *xorNonceAEAD) NonceSize() int { return 8 }
func (f *xorNonceAEAD) Overhead() int  { return f.aead.Overhead() }

func (f *xorNonceAEAD) Seal(out, nonce, plaintext, additionalData []byte) []byte {
	for i, b := range nonce {
		f.nonceMask[4+i] ^= b
	}
	result := f.aead.Seal(out, f.nonceMask[:], plaintext, additionalData)
	for i, b := range nonce {
		f.nonceMask[4+i] ^= b
	}
	return result
}

func (f *xorNonceAEAD) Open(out, nonce, ciphertext, additionalData []byte) ([]byte, error) {
	for i, b := range nonce {
		f.nonceMask[4+i] ^= b
	}
	result, err := f.aead.Open(out, f.nonceMask[:], ciphertext, additionalData)
	for i, b := range nonce {
		f.nonceMask[4+i] ^= b
	}
	return result, err
}

func quicHKDFExpandLabel(hash crypto.Hash, secret, context []byte, label string, length int) ([]byte, error) {
	b := make([]byte, 3, 3+6+len(label)+1+len(context))
	binary.BigEndian.PutUint16(b, uint16(length))
	b[2] = uint8(6 + len(label))
	b = append(b, []byte("tls13 ")...)
	b = append(b, []byte(label)...)
	b = b[:3+6+len(label)+1]
	b[3+6+len(label)] = uint8(len(context))
	b = append(b, context...)
	out := make([]byte, length)
	n, err := hkdf.Expand(hash.New, secret, b).Read(out)
	if err != nil {
		return nil, fmt.Errorf("HKDF-Expand-Label failed: read %d/%d: %w", n, length, err)
	}
	if n != length {
		return nil, fmt.Errorf("HKDF-Expand-Label: short read %d/%d", n, length)
	}
	return out, nil
}

func quicReadUvarint(r io.ByteReader) (uint64, error) {
	firstByte, err := r.ReadByte()
	if err != nil {
		return 0, err
	}
	length := 1 << ((firstByte & 0xc0) >> 6)
	b1 := firstByte & (0xff - 0xc0)
	if length == 1 {
		return uint64(b1), nil
	}
	b2, err := r.ReadByte()
	if err != nil {
		return 0, err
	}
	if length == 2 {
		return uint64(b2) + uint64(b1)<<8, nil
	}
	b3, err := r.ReadByte()
	if err != nil {
		return 0, err
	}
	b4, err := r.ReadByte()
	if err != nil {
		return 0, err
	}
	if length == 4 {
		return uint64(b4) + uint64(b3)<<8 + uint64(b2)<<16 + uint64(b1)<<24, nil
	}
	b5, err := r.ReadByte()
	if err != nil {
		return 0, err
	}
	b6, err := r.ReadByte()
	if err != nil {
		return 0, err
	}
	b7, err := r.ReadByte()
	if err != nil {
		return 0, err
	}
	b8, err := r.ReadByte()
	if err != nil {
		return 0, err
	}
	return uint64(b8) + uint64(b7)<<8 + uint64(b6)<<16 + uint64(b5)<<24 + uint64(b4)<<32 + uint64(b3)<<40 + uint64(b2)<<48 + uint64(b1)<<56, nil
}
