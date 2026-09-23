package center

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 漏斗完整性守卫。
//
// api_order_pay_redirect 的每条出口都必须在 302 之前记一行 [PayFunnel]，否则
// "下单→去支付"的**分母会悄悄变小**：漏掉的那条路径上的用户从统计里消失，
// 转化率反而变好看。这类故障不会报错、不会有人发现，所以只能靠结构守卫。
//
// 用源码断言而非日志断言：日志断言要么脆（格式一变就红），要么假绿（只验证
// 某一条路径记了）。这里问的是"有没有哪条 return 路径忘了记"，那是结构问题。
//
// 盲区（诚实记录）：
//   - 只检查 c.Redirect 前是否出现过 funnel(...)，不检查 outcome 取值是否有意义。
//   - 若将来出口改为非 c.Redirect（比如 c.String），本守卫看不见。
func TestPayRedirect_EveryExitLogsFunnel(t *testing.T) {
	src, err := os.ReadFile("api_order_pay_redirect.go")
	require.NoError(t, err)

	// 只看 api_order_pay_redirect 函数体
	body := string(src)
	start := strings.Index(body, "func api_order_pay_redirect(")
	require.Positive(t, start, "找不到 api_order_pay_redirect —— 函数被改名了？守卫要跟着改")
	body = body[start:]

	lines := strings.Split(body, "\n")
	redirectRe := regexp.MustCompile(`c\.Redirect\(`)
	funnelRe := regexp.MustCompile(`funnel\("`)

	var unlogged []string
	for i, line := range lines {
		if !redirectRe.MatchString(line) {
			continue
		}
		// funnel 调用就在同一出口块内，紧挨着 Redirect 之前几行。
		logged := false
		for j := i - 1; j >= 0 && j >= i-8; j-- {
			if funnelRe.MatchString(lines[j]) {
				logged = true
				break
			}
			// 撞到上一个 Redirect 说明已经跨出口了
			if redirectRe.MatchString(lines[j]) {
				break
			}
		}
		if !logged {
			unlogged = append(unlogged, strings.TrimSpace(line))
		}
	}

	require.Empty(t, unlogged,
		"这些 302 出口没有记 [PayFunnel]，漏斗分母会悄悄变小：\n%s",
		strings.Join(unlogged, "\n"))

	// 变异验证的替代品：确认守卫真的扫到了出口，而不是因为 0 个匹配而恒绿。
	var total int
	for _, line := range lines {
		if redirectRe.MatchString(line) {
			total++
		}
	}
	require.GreaterOrEqual(t, total, 7, "出口数量异常少（%d），守卫可能没扫到函数体", total)
}
