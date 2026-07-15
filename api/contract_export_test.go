package center

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 跨层契约导出门。
//
// 「品牌」这个概念在三层各定义了一遍（api/brand.go、webapp/src/brand/*.ts、
// web/src/lib/brands.ts），没有任何测试跨层——X-K2-Brand 漏进 CORS allow-list
// 就是这么放出去的（所有浏览器语境客户端 direct 通道静默全断）。
//
// Go 是契约源。本测试把 Go 的**活值**导出成 contracts/api-contract.json，
// 另外两层的测试 readFileSync 这份文件做断言。
//
// 三个数据来源全部取自活值，一行字面量都不许手抄：
//   - brands      → brandRegistry / AllBrands() 真 struct
//   - cors.*      → 启动真 gin router 挂真中间件，从**响应头**解析
//   - errorCodes  → go/ast 解析 response.go 源文件
//
// 手抄的清单会漂移，那正是这个门要杀死的病。

const contractGeneratedNote = "GENERATED from live Go values by api/contract_export_test.go. Do not hand-edit. Regenerate: cd api && UPDATE_CONTRACT=1 go test -run TestExportContract ./..."

const contractRegenCmd = "cd api && UPDATE_CONTRACT=1 go test -run TestExportContract ./..."

// contractRelPath 从测试所在的 api/ 目录解析到仓库根的 contracts/。
var contractRelPath = filepath.Join("..", "contracts", "api-contract.json")

// ===================== 契约形态 =====================
// 字段名/嵌套由 docs 的跨层 schema 钉死，webapp / web 的测试正对着它编程。
// 改这里 = 改三层契约，不要顺手重命名。

type contractBrand struct {
	ID                 string   `json:"id"`
	DisplayName        string   `json:"displayName"`
	Hosts              []string `json:"hosts"`
	WebOrigins         []string `json:"webOrigins"`
	RedirectRootDomain string   `json:"redirectRootDomain"`
	BaseURL            string   `json:"baseURL"`
	SupportEmail       string   `json:"supportEmail"`
	EDMFromName        string   `json:"edmFromName"`
	PaymentChannels    []string `json:"paymentChannels"`
}

type contractCORSGroup struct {
	AllowHeaders []string `json:"allowHeaders"`
}

type contractCORS struct {
	API contractCORSGroup `json:"api"`
	App contractCORSGroup `json:"app"`
}

type contractErrorCode struct {
	Name string `json:"name"`
	Code int    `json:"code"`
}

type apiContract struct {
	Generated  string                   `json:"_generated"`
	Brands     map[string]contractBrand `json:"brands"`
	CORS       contractCORS             `json:"cors"`
	ErrorCodes []contractErrorCode      `json:"errorCodes"`
}

// ===================== brands：从 brandRegistry 活值取 =====================

func exportBrands(t *testing.T) map[string]contractBrand {
	t.Helper()

	all := AllBrands()
	require.NotEmpty(t, all, "AllBrands() 返回空——契约门会变成永远绿")

	out := make(map[string]contractBrand, len(all))
	for _, b := range all {
		cfg := brandRegistry[b]
		require.NotNil(t, cfg, "brand %q 在 AllBrands() 里但不在 brandRegistry 里", b)
		out[string(b)] = contractBrand{
			ID:                 string(cfg.ID),
			DisplayName:        cfg.DisplayName,
			Hosts:              cfg.Hosts,
			WebOrigins:         cfg.WebOrigins,
			RedirectRootDomain: cfg.RedirectRootDomain,
			BaseURL:            cfg.BaseURL,
			SupportEmail:       cfg.SupportEmail,
			EDMFromName:        cfg.EDMFromName,
			PaymentChannels:    cfg.PaymentChannels,
		}
	}
	return out
}

// ===================== cors：从真中间件的响应头解析 =====================

// parseAllowHeaders 把 Access-Control-Allow-Headers 响应头切成数组。
// 空值硬失败：中间件没吐头 = 契约取不到,绝不能静默导出空数组。
func parseAllowHeaders(t *testing.T, raw, what string) []string {
	t.Helper()
	require.NotEmpty(t, raw, "%s 没有返回 Access-Control-Allow-Headers——契约无从导出", what)

	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if h := strings.TrimSpace(p); h != "" {
			out = append(out, h)
		}
	}
	require.NotEmpty(t, out, "%s 的 Access-Control-Allow-Headers 解析后为空: %q", what, raw)
	return out
}

// exportAPIAllowHeaders 打真 /api 预检（OPTIONS + 合法私有 Origin），读响应头。
func exportAPIAllowHeaders(t *testing.T) []string {
	t.Helper()

	router := createApiCORSRouter()
	req, err := http.NewRequest("OPTIONS", "/api/plans", nil)
	require.NoError(t, err)
	req.Header.Set("Origin", "http://localhost:1420")
	req.Header.Set("Access-Control-Request-Method", "GET")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	return parseAllowHeaders(t, w.Header().Get("Access-Control-Allow-Headers"), "/api 预检")
}

// exportAppAllowHeaders：/app 生产上没有预检路径（网站同源；直连 /app 是
// WebSocket，不预检），所以用一个带合法 Origin 的 GET 把头带出来。
func exportAppAllowHeaders(t *testing.T) []string {
	t.Helper()

	origins := BrandKaitu.Config().WebOrigins
	require.NotEmpty(t, origins, "BrandKaitu.WebOrigins 为空,取不到合法 /app Origin")

	router := createAppCORSRouter()
	req, err := http.NewRequest("GET", "/app/tunnels", nil)
	require.NoError(t, err)
	req.Header.Set("Origin", origins[0])

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	return parseAllowHeaders(t, w.Header().Get("Access-Control-Allow-Headers"), "/app GET("+origins[0]+")")
}

// ===================== errorCodes：go/ast 解析 response.go =====================

// exportErrorCodes 提取 response.go 里全部 `ErrorXxx ErrorCode = N` 常量。
// 覆盖两种写法：显式 `ErrorFoo ErrorCode = 400` 与省略类型的 `ErrorFoo = 400001`。
// 解析不到任何常量必须硬失败——静默返回空数组会让这个门永远绿。
func exportErrorCodes(t *testing.T) []contractErrorCode {
	t.Helper()

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "response.go", nil, 0)
	require.NoError(t, err, "解析 response.go 失败")

	var out []contractErrorCode
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			// 接受：显式 ErrorCode 类型，或省略类型（untyped int 常量）。
			// 其它显式类型一律跳过。
			errorCodeTyped := false
			if vs.Type != nil {
				id, ok := vs.Type.(*ast.Ident)
				if !ok || id.Name != "ErrorCode" {
					continue
				}
				errorCodeTyped = true
			}
			for i, name := range vs.Names {
				if !strings.HasPrefix(name.Name, "Error") {
					continue
				}
				// 隐式常量重复 / iota（`const ( A ErrorCode = 1 \n B \n C )` 里的 B、C）
				// 没有对应的 Values。静默跳过 = 该码不进契约 = webapp 侧"没有要求镜像它"
				// = 门对着这个码空绿——正是 400011 漏了两年没人发现的那个病。
				// 提取器只认显式字面量，看不懂就硬停，绝不静默漏。
				if i >= len(vs.Values) {
					t.Fatalf("常量 %s 没有显式字面量值（隐式常量重复或 iota）。\n"+
						"本提取器只认 `ErrorFoo ErrorCode = N` 的显式写法——静默跳过会让这个码"+
						"不进契约、前端无人守。请写成显式值，或升级提取器。", name.Name)
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.INT {
					// 显式 ErrorCode 类型却不是整数字面量（如 `= ErrorBar + 1`）：
					// 提取器看不见它，同样是静默漏码，硬停。
					// 无类型的 Error* 常量（如字符串消息）跳过才是对的——它们不是错误码。
					if errorCodeTyped {
						t.Fatalf("ErrorCode 常量 %s 的值不是整数字面量——提取器看不见它，"+
							"该码不会进契约、前端无人守。", name.Name)
					}
					continue
				}
				code, err := strconv.Atoi(lit.Value)
				require.NoError(t, err, "常量 %s 的值 %q 不是十进制整数", name.Name, lit.Value)
				out = append(out, contractErrorCode{Name: name.Name, Code: code})
			}
		}
	}

	require.NotEmpty(t, out, "从 response.go 解析出 0 个 ErrorCode 常量——解析器坏了,门会变成永远绿")

	sort.Slice(out, func(i, j int) bool {
		if out[i].Code != out[j].Code {
			return out[i].Code < out[j].Code
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// ===================== golden 门 =====================

// marshalContract 序列化契约。
// 用 json.Encoder + SetEscapeHTML(false) 而不是 json.MarshalIndent：默认的 HTML
// 转义会把 _generated 里重新生成命令的 "&&" 写成 "&&"，golden 文件
// 里那串东西人眼无法辨认。Encode 自带结尾换行。
func marshalContract(t *testing.T, c apiContract) []byte {
	t.Helper()

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	require.NoError(t, enc.Encode(c))
	return buf.Bytes()
}

// lineDiff 按行位置对比，只打印不同的行。插入/删除会让其后的行整体错位，
// 但足以定位漂移点。
func lineDiff(committed, live string) string {
	c := strings.Split(committed, "\n")
	l := strings.Split(live, "\n")

	var b strings.Builder
	for i := 0; i < max(len(c), len(l)); i++ {
		inC, inL := i < len(c), i < len(l)
		if inC && inL && c[i] == l[i] {
			continue
		}
		if inC {
			fmt.Fprintf(&b, "  line %d  -committed: %s\n", i+1, c[i])
		}
		if inL {
			fmt.Fprintf(&b, "  line %d  +live(Go):  %s\n", i+1, l[i])
		}
	}
	return b.String()
}

// TestExportContract 默认是**只读的比对门**：把 Go 活值构建的契约与已提交的
// contracts/api-contract.json 比对，不一致就失败。
//
// 只有 UPDATE_CONTRACT=1 才写文件。这条是整个门的命门:
// 若 go test 自动重写 golden 文件，CI 全新检出也会自动重写并通过 → 门永远绿，
// 等于没有。这是 golden-file 的经典陷阱。
//
// 本测试不依赖 MySQL / config.yml —— 必须能在裸 CI 上跑通,所以绝不 skipIfNoConfig:
// 一旦 skip，CI 上这个门就消失了。
//
// ⚠️ go test 缓存盲区（实测可复现,非理论）: 模块根是 api/（api/go.mod），而 golden
// 文件在 ../contracts/ —— **模块外**。cmd/go 的 test cache 明确不 recheck 模块根以外
// 打开的文件("Do not recheck files outside the module, GOPATH, or GOROOT root"),
// 所以只改 golden 文件、不动 api/*.go 时,go test 会返回**陈旧的 cached PASS**。
// 改了 api/*.go 的漂移不受影响（包源变了 → action ID 变 → 必然重跑）,受影响的恰好是
// "手改 golden 去迁就代码"这一种——也正是本门要拦的动作。
// 因此跑这个门必须带 -count=1（k2 的 CI 步骤已经这么做了）。
func TestExportContract(t *testing.T) {
	contract := apiContract{
		Generated: contractGeneratedNote,
		Brands:    exportBrands(t),
		CORS: contractCORS{
			API: contractCORSGroup{AllowHeaders: exportAPIAllowHeaders(t)},
			App: contractCORSGroup{AllowHeaders: exportAppAllowHeaders(t)},
		},
		ErrorCodes: exportErrorCodes(t),
	}

	live := marshalContract(t, contract)

	if os.Getenv("UPDATE_CONTRACT") == "1" {
		require.NoError(t, os.MkdirAll(filepath.Dir(contractRelPath), 0o755))
		require.NoError(t, os.WriteFile(contractRelPath, live, 0o644))
		t.Logf("UPDATE_CONTRACT=1: wrote %s (%d bytes)", contractRelPath, len(live))
		return
	}

	committed, err := os.ReadFile(contractRelPath)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v\n\n"+
			"这份契约必须提交进 git（webapp / web 的跨层测试要读它）。\n"+
			"重新生成: %s", contractRelPath, err, contractRegenCmd)
	}

	if !bytes.Equal(committed, live) {
		t.Fatalf("契约漂移: Go 活值与已提交的 %s 不一致。\n\n%s\n"+
			"若 Go 侧改动是有意的,重新生成并把改动一起提交:\n  %s\n"+
			"若不是,说明 Go 侧被改坏了 —— 别改 golden 文件去迁就它。",
			contractRelPath, lineDiff(string(committed), string(live)), contractRegenCmd)
	}
}
