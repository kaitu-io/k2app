//go:build linux

package gateway

import (
	"reflect"
	"strings"
	"testing"

	"github.com/kaitu-io/k2/config"
)

func flatten(cmds [][]string) string {
	lines := make([]string, len(cmds))
	for i, c := range cmds {
		lines[i] = strings.Join(c, " ")
	}
	return strings.Join(lines, "\n")
}

func TestWifiSlotCommands(t *testing.T) {
	bindings := []config.SlotBinding{
		{Slot: 1, Country: "ae", Index: 1, TunnelIndex: 0},
		{Slot: 2, Country: "ae", Index: 2, TunnelIndex: 1},
	}
	local := &slotLocalState{TakenOver: map[int]bool{2: true}}
	cmds := wifiSlotCommands(bindings, local)
	joined := flatten(cmds)

	// slot1:默认名下发(argv 直传,值不带引号包裹)
	if !strings.Contains(joined, "uci set wireless.slot1.ssid=overleap-ae-1") {
		t.Fatalf("missing slot1 ssid:\n%s", joined)
	}
	// slot2:客户已接管 → 不改名
	if strings.Contains(joined, "wireless.slot2.ssid=") {
		t.Fatalf("taken-over slot2 must keep customer ssid:\n%s", joined)
	}
	// 绑定槽 enable
	if !strings.Contains(joined, "uci set wireless.slot1.disabled=0") ||
		!strings.Contains(joined, "uci set wireless.slot2.disabled=0") {
		t.Fatalf("bound slots must be enabled:\n%s", joined)
	}
	// 未绑定槽 3..8 disable
	for _, s := range []string{"slot3", "slot4", "slot5", "slot6", "slot7", "slot8"} {
		if !strings.Contains(joined, "uci set wireless."+s+".disabled=1") {
			t.Fatalf("unbound %s must be disabled:\n%s", s, joined)
		}
	}
	// 收尾 commit + reload,在最后且各为独立 argv
	last2 := cmds[len(cmds)-2:]
	if strings.Join(last2[0], " ") != "uci commit wireless" || strings.Join(last2[1], " ") != "wifi reload" {
		t.Fatalf("must end with commit+reload:\n%s", joined)
	}
}

func TestSetSlotCmds_And_Validation(t *testing.T) {
	// argv 向量:含空格的 SSID 是单个参数,无 shell 转义问题
	if cmds := setSlotSSIDCmds(3, "My Studio"); !reflect.DeepEqual(cmds[0], []string{"uci", "set", "wireless.slot3.ssid=My Studio"}) {
		t.Fatalf("ssid cmd: %v", cmds)
	}
	if cmds := setSlotPasswordCmds(3, "s3cretpass"); !reflect.DeepEqual(cmds[0], []string{"uci", "set", "wireless.slot3.key=s3cretpass"}) {
		t.Fatalf("key cmd: %v", cmds)
	}
	// 注入尝试作为字面值传给 uci(argv 直传),但控制字符仍拒
	if validSSID("") || validSSID(strings.Repeat("x", 33)) || validSSID("nl\n") || validSSID("nul\x00") {
		t.Fatal("ssid validation holes")
	}
	if !validSSID("My Studio; rm -rf /") { // 无 shell,分号只是普通字符 → 合法 SSID
		t.Fatal("argv-safe chars must be allowed")
	}
	if validWifiPassword("short") || validWifiPassword(strings.Repeat("x", 64)) || !validWifiPassword("goodpass1") {
		t.Fatal("password validation holes")
	}
}
