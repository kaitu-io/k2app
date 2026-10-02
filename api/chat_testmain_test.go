package center

import (
	"context"
	"sync"
	"testing"

	"github.com/spf13/viper"
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
	// 品牌白名单默认放开全部注册品牌（生产默认是空 = 全关）；要测白名单的用例用 setChatViper 覆盖。
	// viper.Set 是最高优先级的覆盖层，不受之后加载 config.yml 的影响。
	viper.Set("chat.brands", chatAllBrandNames())
}

func chatAllBrandNames() []string {
	var names []string
	for _, b := range AllBrands() {
		names = append(names, string(b))
	}
	return names
}

var (
	chatMigrateOnce sync.Once
	chatMigrateErr  error
)

// chatMigrated 保证表结构就位，整个测试进程只迁移一次：Migrate() 对全部模型做 AutoMigrate
// （几百条 information_schema 查询，空闲时约 3 秒、高负载下几十秒），每个测试都跑一遍会让套件慢一个数量级。
func chatMigrated(t *testing.T) {
	t.Helper()
	chatMigrateOnce.Do(func() { chatMigrateErr = Migrate() })
	if chatMigrateErr != nil {
		t.Fatalf("migrate: %v", chatMigrateErr)
	}
}
