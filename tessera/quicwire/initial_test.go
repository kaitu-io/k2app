package quicwire

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// buildInitial assembles a v1 Initial header by hand so the tests do not depend
// on quic-go agreeing with us about what it emits. The end-to-end check against
// a real quic-go datagram lives in the integration test.
func buildInitial(dcid, scid, token []byte) []byte {
	var b []byte
	b = append(b, 0xc0) // long header, fixed bit, type Initial, pn_len 1
	b = binary.BigEndian.AppendUint32(b, Version1)
	b = append(b, byte(len(dcid)))
	b = append(b, dcid...)
	b = append(b, byte(len(scid)))
	b = append(b, scid...)
	b = appendVarint(b, uint64(len(token)))
	b = append(b, token...)
	b = appendVarint(b, 100) // length field
	b = append(b, 0x00)      // packet number
	b = append(b, make([]byte, 99)...)
	return b
}

func appendVarint(b []byte, v uint64) []byte {
	switch {
	case v < 1<<6:
		return append(b, byte(v))
	case v < 1<<14:
		return binary.BigEndian.AppendUint16(b, uint16(v)|0x4000)
	case v < 1<<30:
		return binary.BigEndian.AppendUint32(b, uint32(v)|0x80000000)
	default:
		return binary.BigEndian.AppendUint64(b, v|0xc000000000000000)
	}
}

func TestParseInitialExtractsTheToken(t *testing.T) {
	dcid := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	scid := []byte{9, 10}

	for _, tc := range []struct {
		name  string
		token []byte
	}{
		{"无 token", nil},
		{"1 字节", []byte{0xaa}},
		{"64 字节（Tessera 载荷大小）", bytes.Repeat([]byte{0x5a}, 64)},
		// 64 bytes is where the varint length grows from one byte to two, so it
		// is exactly the boundary Tessera's own token sits on.
		{"63 字节（单字节 varint 上限）", bytes.Repeat([]byte{0x5a}, 63)},
		{"78 字节（quic-go 自签 NEW_TOKEN 实测尺寸）", bytes.Repeat([]byte{0x5a}, 78)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseInitial(buildInitial(dcid, scid, tc.token))
			if !ok {
				t.Fatal("应能解析")
			}
			if !bytes.Equal(got.Token, tc.token) {
				t.Errorf("token = %x，期望 %x", got.Token, tc.token)
			}
			if !bytes.Equal(got.DestConnID, dcid) {
				t.Errorf("DCID = %x，期望 %x", got.DestConnID, dcid)
			}
			if !bytes.Equal(got.SrcConnID, scid) {
				t.Errorf("SCID = %x，期望 %x", got.SrcConnID, scid)
			}
		})
	}
}

func TestParseInitialRejectsWhatIsNotOurs(t *testing.T) {
	good := buildInitial([]byte{1, 2, 3, 4}, []byte{5, 6}, []byte{0xaa, 0xbb})

	withFirstByte := func(b byte) []byte {
		d := append([]byte(nil), good...)
		d[0] = b
		return d
	}
	withVersion := func(v uint32) []byte {
		d := append([]byte(nil), good...)
		binary.BigEndian.PutUint32(d[1:5], v)
		return d
	}
	oversizedDCID := append([]byte(nil), good...)
	oversizedDCID[5] = 21 // RFC 9000 caps connection IDs at 20 bytes

	// A token length that runs past the end of the datagram: the parser must
	// refuse rather than slice out of bounds or hand back short bytes.
	lyingTokenLen := []byte{0xc0}
	lyingTokenLen = binary.BigEndian.AppendUint32(lyingTokenLen, Version1)
	lyingTokenLen = append(lyingTokenLen, 0x00, 0x00) // empty DCID and SCID
	lyingTokenLen = appendVarint(lyingTokenLen, 9999)
	lyingTokenLen = append(lyingTokenLen, 0xaa, 0xbb)

	for _, tc := range []struct {
		name string
		d    []byte
	}{
		{"空数据报", nil},
		{"过短", good[:6]},
		{"短头包（连接迁移后的样子）", withFirstByte(0x40)},
		{"固定位为 0", withFirstByte(0x80)},
		{"长头但类型是 Handshake", withFirstByte(0xe0)},
		{"长头但类型是 0-RTT", withFirstByte(0xd0)},
		{"版本协商（version 0）", withVersion(0)},
		{"QUIC v2", withVersion(0x6b3343cf)},
		{"DCID 长度超过 20", oversizedDCID},
		{"token 长度超出数据报", lyingTokenLen},
		{"头部在 DCID 中途截断", good[:7]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := ParseInitial(tc.d); ok {
				t.Fatal("不应解析成功 —— 解析不出就是借壳路径，这是安全的默认")
			}
		})
	}

	// Control arm: a parser that always returns false would pass everything above.
	if _, ok := ParseInitial(good); !ok {
		t.Fatal("对照组：合法 Initial 应解析成功")
	}
}

// FuzzParseInitial pins the property that matters for a packet parser fed by
// anyone on the internet: it must never panic, and anything it claims to have
// parsed must actually lie inside the datagram it was given.
func FuzzParseInitial(f *testing.F) {
	f.Add(buildInitial([]byte{1, 2, 3, 4}, []byte{5, 6}, []byte{0xaa}))
	f.Add(buildInitial(nil, nil, bytes.Repeat([]byte{7}, 64)))
	f.Add([]byte{0xc0, 0, 0, 0, 1})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, d []byte) {
		got, ok := ParseInitial(d)
		if !ok {
			return
		}
		for name, sub := range map[string][]byte{
			"DestConnID": got.DestConnID,
			"SrcConnID":  got.SrcConnID,
			"Token":      got.Token,
		} {
			if len(sub) > 0 && !isSubslice(d, sub) {
				t.Fatalf("%s 不在输入数据报内", name)
			}
		}
		if len(got.DestConnID) > maxConnIDLen || len(got.SrcConnID) > maxConnIDLen {
			t.Fatalf("连接 ID 超过 %d 字节", maxConnIDLen)
		}
	})
}

// isSubslice reports whether sub aliases memory inside d.
func isSubslice(d, sub []byte) bool {
	if len(sub) > len(d) {
		return false
	}
	for i := 0; i+len(sub) <= len(d); i++ {
		if &d[i] == &sub[0] {
			return true
		}
	}
	return false
}
