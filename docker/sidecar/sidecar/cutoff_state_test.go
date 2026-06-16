package sidecar

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCutoffState_RoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cutoff.state")
	assert.NoError(t, saveCutoffState(p, cutoffState{EpochID: 7, Cut: true}))
	got := loadCutoffState(p)
	assert.Equal(t, int64(7), got.EpochID)
	assert.True(t, got.Cut)
}

func TestCutoffState_MissingFileIsZero(t *testing.T) {
	assert.Equal(t, cutoffState{}, loadCutoffState(filepath.Join(t.TempDir(), "nope.state")))
}

func TestCutoffState_CorruptFileIsZero(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.state")
	assert.NoError(t, os.WriteFile(p, []byte("not json"), 0o600))
	assert.Equal(t, cutoffState{}, loadCutoffState(p))
}
