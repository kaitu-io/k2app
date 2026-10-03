package center

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChatPlainText(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"production reply shape": {
			in:   "关于购买：\n\n1. **如何购买**：\n   - 打开客户端，进入「购买」页面。\n   - 选择套餐时长。",
			want: "关于购买：\n\n1. 如何购买：\n   · 打开客户端，进入「购买」页面。\n   · 选择套餐时长。",
		},
		"heading":          {in: "## 续费说明\n到期后暂停", want: "续费说明\n到期后暂停"},
		"star bullets":     {in: "* 一\n+ 二", want: "· 一\n· 二"},
		"link":             {in: "见 [安装指南](https://www.kaitu.io/zh-CN/install)", want: "见 安装指南 https://www.kaitu.io/zh-CN/install"},
		"inline code":      {in: "运行 `k2 up` 即可", want: "运行 k2 up 即可"},
		"code fence":       {in: "```bash\nk2 up\n```", want: "k2 up"},
		"underscore bold":  {in: "__重要__", want: "重要"},
		"plain untouched":  {in: "价格 5 * 3 = 15，见 https://a.io/x_y_z", want: "价格 5 * 3 = 15，见 https://a.io/x_y_z"},
		"only syntax kept": {in: "**", want: "**"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, c.want, chatPlainText(c.in))
		})
	}
}

// AI 回复落库的唯一入口 chatAppendAI 存的是清洗后的纯文本：访客和 Slack 镜像都读库里这一份。
func TestChatAI_AppendStoresPlainText(t *testing.T) {
	conv := newAIConv(t)
	require.NoError(t, chatAppendAI(context.Background(), conv, "1. **如何购买**：\n- 进入「购买」页面"))
	msgs := convMessages(t, conv.ID)
	require.Len(t, msgs, 1)
	assert.Equal(t, "1. 如何购买：\n· 进入「购买」页面", msgs[0].Content)
}
