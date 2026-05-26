package appbypass

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseRaw_FixtureCN(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "app-bypass-cn.yaml"))
	if err != nil { t.Fatalf("read: %v", err) }
	raw, err := parseRaw(data)
	if err != nil { t.Fatalf("parseRaw: %v", err) }

	if raw.Version != 1 {
		t.Errorf("Version = %d, want 1", raw.Version)
	}
	if raw.Region != "cn" {
		t.Errorf("Region = %q, want cn", raw.Region)
	}
	if !reflect.DeepEqual([]string{"com.xiaomi.market", "com.huawei.appmarket"},
		raw.Android.InstallerExact) {
		t.Errorf("InstallerExact: %v", raw.Android.InstallerExact)
	}
	if !reflect.DeepEqual([]string{"WeChat", "QQ", "DingTalk"},
		raw.Desktop.ProcessPrefix) {
		t.Errorf("ProcessPrefix: %v", raw.Desktop.ProcessPrefix)
	}
}

func TestParseRaw_UnsupportedVersion(t *testing.T) {
	_, err := parseRaw([]byte("version: 999\nregion: xx\n"))
	if err == nil {
		t.Fatalf("expected error for version 999")
	}
	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Errorf("expected ErrUnsupportedVersion, got %v", err)
	}
}

func TestParseRaw_MalformedYAML(t *testing.T) {
	_, err := parseRaw([]byte("not: valid: yaml: nesting:"))
	if err == nil {
		t.Fatalf("expected parse error")
	}
}

func TestParseRaw_MissingVersionField(t *testing.T) {
	_, err := parseRaw([]byte("region: cn\n"))
	if err == nil {
		t.Fatalf("expected error for missing version")
	}
}

func TestParseRaw_UnknownField(t *testing.T) {
	// KnownFields(true) rejects typos at publisher side.
	_, err := parseRaw([]byte("version: 1\nregion: cn\nunknown_section: foo\n"))
	if err == nil {
		t.Fatalf("expected strict-mode error for unknown field")
	}
}
