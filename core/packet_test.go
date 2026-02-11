package core

import (
	"bytes"
	"testing"
)

func TestPacketRoundtrip(t *testing.T) {
	tests := []struct {
		name    string
		packet  []byte
		wantErr bool
	}{
		{
			name:    "small packet",
			packet:  []byte{0x45, 0x00, 0x00, 0x54},
			wantErr: false,
		},
		{
			name:    "typical IPv4 packet",
			packet:  make([]byte, 100),
			wantErr: false,
		},
		{
			name:    "max size packet",
			packet:  make([]byte, 1500),
			wantErr: false,
		},
		{
			name:    "empty packet",
			packet:  []byte{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer

			// Write packet
			err := WritePacket(&buf, tt.packet)
			if (err != nil) != tt.wantErr {
				t.Errorf("WritePacket() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			// Read packet back
			got, err := ReadPacket(&buf)
			if err != nil {
				t.Errorf("ReadPacket() error = %v", err)
				return
			}

			// Verify packet content
			if !bytes.Equal(got, tt.packet) {
				t.Errorf("packet mismatch: got %d bytes, want %d bytes", len(got), len(tt.packet))
			}
		})
	}
}

func TestReadPacketEOF(t *testing.T) {
	// Test reading from empty buffer
	var buf bytes.Buffer
	_, err := ReadPacket(&buf)
	if err == nil {
		t.Error("expected error when reading from empty buffer")
	}
}

func TestWritePacketTooLarge(t *testing.T) {
	var buf bytes.Buffer
	largePacket := make([]byte, maxPacketSize+1)
	err := WritePacket(&buf, largePacket)
	if err == nil {
		t.Error("expected error for oversized packet")
	}
}
