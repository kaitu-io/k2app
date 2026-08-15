//go:build linux

package webui

import "testing"

func TestIsNewerVersion(t *testing.T) {
	cases := []struct {
		remote, local string
		want          bool
	}{
		// 数字段比较（对齐 K2PluginUtilsTest.kt）
		{"1.1.0", "1.0.0", true},      // major/minor bump
		{"1.0.0", "1.0.0", false},     // equal
		{"0.9.0", "1.0.0", false},     // older
		{"1.0.1", "1.0.0", true},      // patch bump
		{"1.0", "1.0.0", false},       // 缺段补 0: "1.0" == "1.0.0"
		{"1.0.0.1", "1.0.0", true},    // 多一段 > 0（web OTA 四段版本 vs 三段壳版本）
		{"abc", "1.0.0", false},       // 非数字段当 0
		{"0.4.9.1234", "0.4.9", true}, // OTA 版本 > 壳版本回退值
		{"0.4.9.1234", "0.4.9.1233", true},
		{"0.4.9.1234", "0.4.9.1234", false},
		// pre-release 后缀（对齐 K2PluginUtils splitVersion 语义）
		{"0.5.0", "0.5.0-beta.1", true},        // stable > 同 base beta
		{"0.5.0-beta.1", "0.5.0", false},       // beta < 同 base stable
		{"0.5.0-beta.2", "0.5.0-beta.1", true}, // beta 递增
		{"0.5.0-beta.1", "0.5.0-beta.1", false},
		{"0.5.0-beta.1", "0.4.0", true}, // 跨 base：beta 的 base 更高
		{"0.6.0", "0.5.0-beta.1", true}, // 新 stable > 旧 beta
	}
	for _, tc := range cases {
		if got := isNewerVersion(tc.remote, tc.local); got != tc.want {
			t.Errorf("isNewerVersion(%q, %q) = %v, want %v", tc.remote, tc.local, got, tc.want)
		}
	}
}

func TestMeetsMinVersion(t *testing.T) {
	cases := []struct {
		min, have string
		want      bool
	}{
		{"", "0.4.9", true},             // 空 min 放行（旧 manifest 无此字段）
		{"  ", "0.4.9", true},           // 空白同空
		{"0.4.9", "0.4.9", true},        // 等于即满足
		{"0.4.9", "0.5.0", true},        // 高于满足
		{"0.4.9", "0.4.8", false},       // 低于拒绝
		{"0.4.9", "0.4.9-beta.2", true}, // 只比 base：beta 的 base 满足即放行
		{"0.5", "0.5.0", true},          // 缺段补 0
	}
	for _, tc := range cases {
		if got := meetsMinVersion(tc.min, tc.have); got != tc.want {
			t.Errorf("meetsMinVersion(%q, %q) = %v, want %v", tc.min, tc.have, got, tc.want)
		}
	}
}

func TestMeetsMinBridge(t *testing.T) {
	cases := []struct {
		min, have int
		want      bool
	}{
		{0, 1, true},  // 字段缺省（JSON 零值）放行
		{-1, 1, true}, // 非法负值放行
		{1, 1, true},
		{2, 1, false},
		{1, 2, true},
	}
	for _, tc := range cases {
		if got := meetsMinBridge(tc.min, tc.have); got != tc.want {
			t.Errorf("meetsMinBridge(%d, %d) = %v, want %v", tc.min, tc.have, got, tc.want)
		}
	}
}
