package appbypass

import (
	"reflect"
	"strings"
	"testing"
)

func TestCompile_LowercasesDesktop(t *testing.T) {
	raw := &rawFile{
		Version: 1, Region: "cn",
		Desktop: rawDesktop{
			ProcessExact:  []string{"WeChat", "QQ", "wechat"},
			ProcessPrefix: []string{"WeChat", "QQ"},
		},
	}
	p, err := compile(raw)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !reflect.DeepEqual([]string{"qq", "wechat"}, p.Desktop.ProcessExact) {
		t.Errorf("ProcessExact lowercased+dedup'd+sorted: got %v", p.Desktop.ProcessExact)
	}
	if !reflect.DeepEqual([]string{"qq", "wechat"}, p.Desktop.ProcessPrefix) {
		t.Errorf("ProcessPrefix: got %v", p.Desktop.ProcessPrefix)
	}
}

func TestCompile_PreservesAndroidCase(t *testing.T) {
	raw := &rawFile{
		Version: 1, Region: "cn",
		Android: rawAndroid{
			PackageExact:  []string{"com.eg.android.AlipayGphone"},
			PackagePrefix: []string{"com.tencent.", "com.alipay."},
		},
	}
	p, _ := compile(raw)
	if !reflect.DeepEqual([]string{"com.eg.android.AlipayGphone"}, p.Android.PackageExact) {
		t.Errorf("PackageExact case preserved: got %v", p.Android.PackageExact)
	}
	if !reflect.DeepEqual([]string{"com.alipay.", "com.tencent."}, p.Android.PackagePrefix) {
		t.Errorf("PackagePrefix sorted+case: got %v", p.Android.PackagePrefix)
	}
}

func TestCompile_RejectsEmptyEntries(t *testing.T) {
	raw := &rawFile{
		Version: 1, Region: "cn",
		Desktop: rawDesktop{ProcessExact: []string{"WeChat", ""}},
	}
	if _, err := compile(raw); err == nil {
		t.Errorf("expected error for empty entry")
	}
}

func TestCompile_RejectsTooLongEntry(t *testing.T) {
	long := strings.Repeat("a", 300)
	raw := &rawFile{
		Version: 1, Region: "cn",
		Desktop: rawDesktop{ProcessExact: []string{long}},
	}
	if _, err := compile(raw); err == nil {
		t.Errorf("expected error for >256-char entry")
	}
}

func TestCompile_RejectsTooManyEntries(t *testing.T) {
	big := make([]string, 600)
	for i := range big {
		big[i] = "entry" + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)) + string(rune('0'+i%10))
	}
	raw := &rawFile{Version: 1, Region: "cn", Desktop: rawDesktop{ProcessPrefix: big}}
	if _, err := compile(raw); err == nil {
		t.Errorf("expected error for >500 entries per section")
	}
}
