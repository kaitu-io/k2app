package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The install log is the sole remote evidence when the Windows service
// fails to come back after an upgrade — these tests pin its contract.

func TestInstallLogFormatAndFlush(t *testing.T) {
	il := &installLog{}
	il.logf("k2 service install starting: version=%s elevated=%v", "0.4.8", true)
	il.logf("create service attempt %d: %v", 1, "The specified service has been marked for deletion.")

	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "k2-service-install.log")
	il.flushTo(path) // must create intermediate dirs

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read flushed log: %v", err)
	}
	content := string(data)

	lines := strings.Split(strings.TrimRight(content, "\r\n"), "\r\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 CRLF lines, got %d: %q", len(lines), content)
	}
	if !strings.Contains(lines[0], "version=0.4.8 elevated=true") {
		t.Errorf("line 0 missing header fields: %q", lines[0])
	}
	if !strings.Contains(lines[1], "marked for deletion") {
		t.Errorf("line 1 missing error text: %q", lines[1])
	}
	// Every line starts with an RFC3339 timestamp (T separator at offset 10).
	for i, line := range lines {
		if len(line) < 20 || line[10] != 'T' {
			t.Errorf("line %d lacks RFC3339 timestamp prefix: %q", i, line)
		}
	}
}

func TestInstallLogFlushOverwritesPreviousRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "k2-service-install.log")

	old := &installLog{}
	old.logf("old attempt")
	old.flushTo(path)

	fresh := &installLog{}
	fresh.logf("new attempt")
	fresh.flushTo(path)

	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "old attempt") {
		t.Errorf("flush must overwrite, not append: %q", string(data))
	}
	if !strings.Contains(string(data), "new attempt") {
		t.Errorf("flush lost the latest attempt: %q", string(data))
	}
}

func TestInstallLogFlushToEmptyPathIsNoop(t *testing.T) {
	il := &installLog{}
	il.logf("whatever")
	il.flushTo("") // must not panic
}

// The file name must keep matching the desktop uploader's `k2*.log` glob —
// renaming it silently breaks feedback log collection.
func TestInstallLogPathMatchesUploaderGlob(t *testing.T) {
	path := installLogPath()
	base := filepath.Base(path)
	if !strings.HasPrefix(base, "k2") || !strings.HasSuffix(base, ".log") {
		t.Errorf("basename %q must match the uploader's k2*.log glob", base)
	}
}
