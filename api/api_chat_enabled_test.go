package center

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 入口探测只读开关：不建 guest、不种任何 cookie；只有正常开放才是 true。
func TestChatEnabled_ReflectsAccessWithoutSideEffects(t *testing.T) {
	cases := []struct {
		name          string
		enabled, prev bool
		brands        []string
		want          bool
	}{
		{"正常开放", true, true, []string{"kaitu"}, true},
		{"仅预览：普通访客看不到入口", false, true, []string{"kaitu"}, false},
		{"硬关", false, false, []string{"kaitu"}, false},
		{"品牌不在名单", true, true, []string{"overleap"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setChatViper(t, "chat.enabled", tc.enabled)
			setChatViper(t, "chat.preview_enabled", tc.prev)
			setChatViper(t, "chat.brands", tc.brands)
			w := NewTestRequest("GET", "/api/chat/enabled").WithHeader("X-K2-Brand", "kaitu").Execute(chatRouter())
			resp, data := chatDecode(t, w)
			require.Equal(t, 0, resp.Code)
			assert.Equal(t, tc.want, data["enabled"])
			assert.Empty(t, w.Result().Cookies(), "探测不得种 cookie")
		})
	}
}
