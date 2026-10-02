package center

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/viper"
	"github.com/wordgate/qtoolkit/redis"
	"github.com/wordgate/qtoolkit/slack"
)

// 假 Slack（客服聊天各任务的测试共用）：
//   - qtoolkit/slack 的 Web API 基址是包内未导出变量，改不到；它的 http.Client 没设 Transport，
//     所以走 http.DefaultTransport。newFakeSlack 把 DefaultTransport 包一层转发（t.Cleanup 还原）：
//     发往 slack.com 的请求改写到这个假服务，其他主机原样放行。
//     假服务不在时 bot token 为空，slack 包自己就拒绝调用（ErrNoBotToken），不会打到真 Slack。
//   - newFakeSlack 同时配好 bot token 与总览频道（slack.chat_lobby_channel_id），测试结束全部还原，
//     所以不调用它的测试里 chatSlackLobby()=="" → Slack 镜像整体关闭。
//
// 用法见 logic_chat_slack_test.go。

const (
	fakeSlackLobbyID = "CLOBBY"
	fakeSlackBotID   = "UBOT"
)

// fakeSlackCall 一次收到的 Web API 调用：方法名 + 解码后的参数（JSON body 或 query）。
type fakeSlackCall struct {
	Method string
	Params map[string]any
}

// Str 取字符串参数；不存在或类型不符返回 ""。
func (c fakeSlackCall) Str(key string) string {
	s, _ := c.Params[key].(string)
	return s
}

// fakeSlackResp 一条脚本化响应。Status 为 0 视作 200。
type fakeSlackResp struct {
	Status     int
	Body       string
	RetryAfter string // 非空则写 Retry-After 响应头
}

func fakeSlackOK() fakeSlackResp { return fakeSlackResp{Body: `{"ok":true}`} }

// fakeSlackErr 返回 HTTP 200 + {"ok":false,"error":code}（Slack 业务错误，如 name_taken）。
func fakeSlackErr(code string) fakeSlackResp {
	return fakeSlackResp{Body: fmt.Sprintf(`{"ok":false,"error":%q}`, code)}
}

// fakeSlackHTTP 返回指定 HTTP 状态码（如 500）。
func fakeSlackHTTP(status int) fakeSlackResp { return fakeSlackResp{Status: status, Body: "boom"} }

// fakeSlackRateLimited 返回 429 + Retry-After。
func fakeSlackRateLimited(seconds int) fakeSlackResp {
	return fakeSlackResp{Status: http.StatusTooManyRequests, RetryAfter: fmt.Sprint(seconds)}
}

type fakeSlack struct {
	t      *testing.T
	server *httptest.Server

	mu      sync.Mutex
	calls   []fakeSlackCall
	scripts map[string][]fakeSlackResp // 每方法一条队列，先于默认响应被消费
	failAll int                        // 非 0：所有调用返回该 HTTP 状态码
	members []string                   // conversations.members 的默认返回（含 bot）
	onCall  func(c fakeSlackCall)      // 记录之后、响应之前调用（不持锁，可在里面追加消息等）
	seq     int
}

// fakeSlackTransport 把 slack.com 的请求改写到所属的假服务，其余主机原样放行。
type fakeSlackTransport struct {
	next   http.RoundTripper
	target *url.URL
}

func (tr fakeSlackTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Hostname() != "slack.com" {
		return tr.next.RoundTrip(req)
	}
	r2 := req.Clone(req.Context())
	r2.URL.Scheme, r2.URL.Host, r2.Host = tr.target.Scheme, tr.target.Host, tr.target.Host
	return tr.next.RoundTrip(r2)
}

// newFakeSlack 启动假 Slack 并把 slack 包指过去；配置 bot token 与总览频道；t.Cleanup 全部还原。
// 默认行为：conversations.create 返回新频道 id（C1、C2…）；conversations.members 返回
// [UBOT, U1, U2]；auth.test 返回 UBOT；chat.postMessage 返回递增 ts；其余返回 {"ok":true}。
func newFakeSlack(t *testing.T) *fakeSlack {
	t.Helper()
	testInitConfig()
	f := &fakeSlack{t: t, scripts: map[string][]fakeSlackResp{}, members: []string{fakeSlackBotID, "U1", "U2"}}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	u, _ := url.Parse(f.server.URL)
	prevTransport := http.DefaultTransport
	http.DefaultTransport = fakeSlackTransport{next: prevTransport, target: u}

	webhooks := viper.GetStringMapString("slack.webhooks")
	// 每个假服务一个唯一 token：slack.BotUserID 按 token 缓存，这样每个测试都会真的调一次 auth.test
	slack.SetConfig(&slack.Config{Webhooks: webhooks, BotToken: "xoxb-fake-" + generateId("t")})
	viper.Set("slack.chat_lobby_channel_id", fakeSlackLobbyID)
	redis.Client().Del(context.Background(), "chat:slack:staff")

	t.Cleanup(func() {
		viper.Set("slack.chat_lobby_channel_id", "")
		slack.SetConfig(&slack.Config{Webhooks: webhooks})
		http.DefaultTransport = prevTransport
		f.server.Close()
		redis.Client().Del(context.Background(), "chat:slack:staff")
	})
	return f
}

func (f *fakeSlack) serve(w http.ResponseWriter, r *http.Request) {
	method := strings.TrimPrefix(r.URL.Path, "/api/")
	params := map[string]any{}
	if body, _ := io.ReadAll(r.Body); len(body) > 0 {
		_ = json.Unmarshal(body, &params)
	}
	for k, v := range r.URL.Query() {
		params[k] = v[0]
	}
	call := fakeSlackCall{Method: method, Params: params}

	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.seq++
	seq := f.seq
	hook := f.onCall
	var resp fakeSlackResp
	switch {
	case f.failAll != 0:
		resp = fakeSlackHTTP(f.failAll)
	case len(f.scripts[method]) > 0:
		resp = f.scripts[method][0]
		f.scripts[method] = f.scripts[method][1:]
	default:
		resp = f.defaultResp(method, seq)
	}
	f.mu.Unlock()

	if hook != nil {
		hook(call)
	}
	if resp.RetryAfter != "" {
		w.Header().Set("Retry-After", resp.RetryAfter)
	}
	if resp.Status != 0 {
		w.WriteHeader(resp.Status)
	}
	_, _ = w.Write([]byte(resp.Body))
}

func (f *fakeSlack) defaultResp(method string, seq int) fakeSlackResp {
	switch method {
	case "conversations.create":
		return fakeSlackResp{Body: fmt.Sprintf(`{"ok":true,"channel":{"id":"C%d"}}`, seq)}
	case "conversations.members":
		b, _ := json.Marshal(map[string]any{"ok": true, "members": f.members})
		return fakeSlackResp{Body: string(b)}
	case "auth.test":
		return fakeSlackResp{Body: fmt.Sprintf(`{"ok":true,"user_id":%q}`, fakeSlackBotID)}
	case "chat.postMessage":
		return fakeSlackResp{Body: fmt.Sprintf(`{"ok":true,"ts":"1700000000.%06d"}`, seq)}
	}
	return fakeSlackOK()
}

// Script 给某方法排入若干条响应，按调用顺序消费；用完后回到默认响应。
func (f *fakeSlack) Script(method string, resps ...fakeSlackResp) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scripts[method] = append(f.scripts[method], resps...)
}

// FailAll 让所有调用返回该 HTTP 状态码；传 0 恢复正常。
func (f *fakeSlack) FailAll(status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failAll = status
}

// SetMembers 设置 conversations.members 的默认返回（要含 bot 自己就把 fakeSlackBotID 放进去）。
func (f *fakeSlack) SetMembers(ids ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.members = ids
}

// OnCall 注册回调：每次调用被记录之后、响应之前执行（在假服务的 goroutine 里，不持锁）。
func (f *fakeSlack) OnCall(fn func(c fakeSlackCall)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.onCall = fn
}

// Reset 清空已记录的调用（脚本与开关不动）。
func (f *fakeSlack) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = nil
}

// Calls 返回至今收到的全部调用（按到达顺序）。
func (f *fakeSlack) Calls() []fakeSlackCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeSlackCall(nil), f.calls...)
}

// Methods 返回调用的方法名序列。
func (f *fakeSlack) Methods() []string {
	calls := f.Calls()
	out := make([]string, len(calls))
	for i, c := range calls {
		out[i] = c.Method
	}
	return out
}

// CallsOf 返回某方法的全部调用。
func (f *fakeSlack) CallsOf(method string) []fakeSlackCall {
	var out []fakeSlackCall
	for _, c := range f.Calls() {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

// Posts 返回发往某频道的 chat.postMessage 文本（按顺序，含失败的那些请求）。
func (f *fakeSlack) Posts(channel string) []string {
	var out []string
	for _, c := range f.CallsOf("chat.postMessage") {
		if c.Str("channel") == channel {
			out = append(out, c.Str("text"))
		}
	}
	return out
}
