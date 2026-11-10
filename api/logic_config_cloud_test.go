package center

import "testing"

// 线上事故（2026-11-10）：bwh 账号的 veid 被改存成不带引号的数字，getString 只认字符串，
// 11 个条目全读成空串被跳过，同步得到 0 台，随后把已有的搬瓦工记录全部标成已删除。
func TestGetString_AcceptsNumbersWrittenWithoutQuotes(t *testing.T) {
	m := map[string]interface{}{
		"s":    "1952237",
		"i":    1952237,
		"i64":  int64(1952237),
		"u64":  uint64(1952237),
		"f":    float64(1952237),
		"frac": 1.5,
		"b":    true,
	}
	for _, key := range []string{"s", "i", "i64", "u64", "f"} {
		if got := getString(m, key); got != "1952237" {
			t.Errorf("getString(%s) = %q, want 1952237", key, got)
		}
	}
	for _, key := range []string{"frac", "b", "missing"} {
		if got := getString(m, key); got != "" {
			t.Errorf("getString(%s) = %q, want empty", key, got)
		}
	}
}

func TestSkipOrphanCleanup(t *testing.T) {
	cases := []struct {
		synced int
		active int64
		want   bool
	}{
		{0, 4, true},  // 厂商返回 0 台，库里还有活跃记录：不清理
		{0, 0, false}, // 全新账号，没有东西可清
		{3, 4, false}, // 正常同步：清理真正消失的那几台
	}
	for _, c := range cases {
		if got := skipOrphanCleanup(c.synced, c.active); got != c.want {
			t.Errorf("skipOrphanCleanup(%d,%d) = %v, want %v", c.synced, c.active, got, c.want)
		}
	}
}
