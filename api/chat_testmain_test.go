package center

import (
	"context"

	"github.com/wordgate/qtoolkit/openai/filesearch"
)

// 客服聊天各测试共用：钩子里的异步工作在测试中同步执行，保证确定性。
func init() {
	chatAsync = func(f func()) { f() }
	// AI 钩子默认关闭：免得其它测试追加访客消息时多出 AI 回复；TestChatAI_* 用 enableChatAI(t) 开启
	chatAIHookEnabled = false
	// 任何测试追加访客消息都会触发 AI 钩子：默认换成固定文本，绝不打到真实 OpenAI
	chatAIAsk = func(ctx context.Context, question string, history []filesearch.Message) (string, error) {
		return "(test ai)", nil
	}
}
