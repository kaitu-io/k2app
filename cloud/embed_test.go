package cloud

import (
	"encoding/json"
	"io"
	"io/fs"
	"testing"
)

func TestWebappFS(t *testing.T) {
	fsys, err := FS()
	if err != nil {
		t.Fatalf("FS() error: %v", err)
	}
	if fsys == nil {
		t.Fatal("FS() returned nil")
	}

	f, err := fsys.Open("index.html")
	if err != nil {
		t.Fatalf("Open(index.html) error: %v", err)
	}
	f.Close()
}

func TestWebappFS_DefaultPlaceholder(t *testing.T) {
	fsys, err := FS()
	if err != nil {
		t.Fatalf("FS() error: %v", err)
	}

	f, err := fsys.Open("index.html")
	if err != nil {
		t.Fatalf("Open(index.html) error: %v", err)
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}

	body := string(data)
	if !contains(body, "k2") {
		t.Errorf("index.html does not contain 'k2'; got:\n%s", body)
	}
}

func TestWebappFS_VersionJSON(t *testing.T) {
	fsys, err := FS()
	if err != nil {
		t.Fatalf("FS() error: %v", err)
	}

	f, err := fsys.Open("version.json")
	if err != nil {
		t.Fatalf("Open(version.json) error: %v", err)
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("version.json is not valid JSON: %v", err)
	}
	if _, ok := m["version"]; !ok {
		t.Error("version.json missing 'version' key")
	}
}

// contains checks if s contains substr.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Verify FS implements fs.FS at compile time.
var _ fs.FS = func() fs.FS {
	f, _ := FS()
	return f
}()
