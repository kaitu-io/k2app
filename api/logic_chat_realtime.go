package center

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/redis"
)

// 访客实时通道：消息先落库，再经 Redis pub/sub 广播推给访客的 WebSocket（两台 Center 互通）；
// WebSocket 不可用时访客走 HTTP 游标轮询（api_chat.go），所以推送丢了不影响正确性。
// 设计见 docs/superpowers/specs/2026-10-01-support-chat-design.md §6

const (
	chatBroadcastNamespace = "kaitu-center-chat"
	chatBroadcastBuffer    = 64 // 每个订阅者缓冲；写满即判慢消费者踢掉，访客重连后靠游标补齐

	chatWSMaxPerChannel = 5 // 本实例上每个频道的并发连接上限

	chatPublishTimeout = 2 * time.Second
)

// 在线标记的 TTL 与续期间隔：包级变量，测试可缩短。
var (
	chatOnlineTTL     = 90 * time.Second
	chatOnlineRefresh = 30 * time.Second
)

var (
	chatBroadcastOnce sync.Once
	chatBroadcastInst *redis.Broadcast
	chatRunOnce       sync.Once
)

// newChatBroadcast 构造广播实例（Origin 校验取自令牌品牌，见 chatWSOriginAllowed）。
// 实例在构造时绑定 redis 客户端，所以单例必须在配置加载之后才创建（懒创建）。
func newChatBroadcast() *redis.Broadcast {
	return redis.NewNamedBroadcast(chatBroadcastNamespace, 10,
		redis.WithSubscriberBuffer(chatBroadcastBuffer),
		redis.WithCheckOrigin(chatWSCheckOrigin),
	)
}

// chatBroadcast 返回进程内单例；未调用 startChatBroadcast 时也可安全使用（只是收不到订阅消息）。
func chatBroadcast() *redis.Broadcast {
	chatBroadcastOnce.Do(func() { chatBroadcastInst = newChatBroadcast() })
	return chatBroadcastInst
}

// startChatBroadcast 启动订阅循环（阻塞 goroutine 的活交给它自己）。幂等：重复调用只会启动一个循环，
// 否则同一进程内的订阅者会收到重复消息。
func startChatBroadcast(ctx context.Context) {
	chatRunOnce.Do(func() {
		bc := chatBroadcast()
		go func() {
			if err := bc.RunContext(ctx); err != nil && ctx.Err() == nil {
				log.Errorf(ctx, "chat broadcast stopped: %v", err)
			}
		}()
	})
}

// StartChatBroadcast 供 cmd/main.go 在启动 HTTP 服务前调用一次。
// 顺带校验一次客服聊天的配置组合：功能开着却缺关键配置时逐条记 Error（见 chatConfigProblems）。
func StartChatBroadcast(ctx context.Context) {
	chatLogConfigProblems(ctx)
	startChatBroadcast(ctx)
}

// chatSubjectOfConversation 返回会话当前应推送/通知的主体：brand + kind + 簇根。
// 会话的 subject_id 可能是后来被并入别的根的 guest id，而访客令牌携带的是当前根，
// 所以 guest 必须解析到根；推送、离线邮件、Slack 等都用它定位访客。
func chatSubjectOfConversation(ctx context.Context, conv *Conversation) (chatSubject, error) {
	s := chatSubject{Brand: Brand(conv.Brand), Kind: conv.SubjectKind, ID: conv.SubjectID}
	switch conv.SubjectKind {
	case SubjectUser:
		return s, nil
	case SubjectGuest:
		root, err := guestRootID(ctx, conv.SubjectID)
		if err != nil {
			return chatSubject{}, fmt.Errorf("resolve guest root for conversation %d: %w", conv.ID, err)
		}
		s.ID = root
		return s, nil
	}
	return chatSubject{}, fmt.Errorf("conversation %d: unknown subject kind %q", conv.ID, conv.SubjectKind)
}

// chatWirePayload 是 WebSocket 上的帧载荷（外层是 qtoolkit 的 {channel,timestamp,payload}）。
// type=message 带 message；type=state 带 conversation（会话处理方/状态变化）。
// 两种帧都带 conversationUuid：频道是按主体的，访客撞上关闭后会换到新会话，
// 前端据此丢弃不属于当前会话的帧。
type chatWirePayload struct {
	Type             string       `json:"type"`
	ConversationUUID string       `json:"conversationUuid"`
	Message          *ChatMsgDTO  `json:"message,omitempty"`
	Conversation     *ChatConvDTO `json:"conversation,omitempty"`
}

// chatPublish 把会话 conv 里的消息推给主体的在线连接。内部备注（note）永不下发。
// 与访客的 HTTP 接口共用同一个 DTO 构造（chatVisitorMessageDTO），访客视图只有一份。
func chatPublish(ctx context.Context, s chatSubject, conv *Conversation, msg *ConversationMessage) error {
	if msg.Kind == MsgNote {
		return nil
	}
	dto := chatVisitorMessageDTO(msg)
	return chatBroadcast().Pub(ctx, s.Channel(), chatWirePayload{Type: "message", ConversationUUID: conv.UUID, Message: &dto})
}

func init() {
	// 追加成功后第一个钩子：先推送再让其他钩子（AI 回复等）跑，访客先看到自己的消息再看到回复。
	// 同步调用，但只做一次短 Redis 发布；任何失败只记日志，绝不影响 appendMessage。
	hook := func(conv *Conversation, msg *ConversationMessage) {
		if msg.Kind == MsgNote {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), chatPublishTimeout)
		defer cancel()
		s, err := chatSubjectOfConversation(ctx, conv)
		if err != nil {
			log.Warnf(ctx, "chat publish: %v", err)
			return
		}
		if err := chatPublish(ctx, s, conv, msg); err != nil {
			log.Warnf(ctx, "chat publish %s: %v", s.Channel(), err)
		}
	}
	chatAfterAppend = append([]func(*Conversation, *ConversationMessage){hook}, chatAfterAppend...)

	// 会话状态变化（handler / status）推给访客，widget 不必从 system 事件消息里推断。
	chatAfterStateChange = append(chatAfterStateChange, func(conv *Conversation) {
		ctx, cancel := context.WithTimeout(context.Background(), chatPublishTimeout)
		defer cancel()
		s, err := chatSubjectOfConversation(ctx, conv)
		if err != nil {
			log.Warnf(ctx, "chat state publish: %v", err)
			return
		}
		if err := chatBroadcast().Pub(ctx, s.Channel(), chatWirePayload{Type: "state", ConversationUUID: conv.UUID, Conversation: chatConversationDTO(conv)}); err != nil {
			log.Warnf(ctx, "chat state publish %s: %v", s.Channel(), err)
		}
	})
}

// ---- 在线标记 ----

func chatOnlineKey(s chatSubject) string { return "chat:online:" + s.Channel() }

// chatVisitorOnline 报告访客当前是否有活跃的 WebSocket 连接（任一实例）。Redis 出错按离线处理。
func chatVisitorOnline(ctx context.Context, s chatSubject) bool {
	n, err := redis.Client().Exists(ctx, chatOnlineKey(s)).Result()
	return err == nil && n > 0
}

// ---- WebSocket 入口 ----

// chatWSCheckOrigin 是广播实例的握手 Origin 校验：品牌取自令牌（Origin 校验发生在握手里，
// 此时令牌已被处理器验证过）。
func chatWSCheckOrigin(r *http.Request) bool {
	s, err := parseChatWSToken(r.URL.Query().Get("token"), time.Now())
	if err != nil {
		return false
	}
	return chatWSOriginAllowed(s.Brand, r.Header.Get("Origin"))
}

// chatWSOriginAllowed：无 Origin 头（非浏览器客户端）放行；有则 host 必须属于令牌品牌的注册 Hosts，
// 或是本地开发的 localhost / 127.0.0.1。
func chatWSOriginAllowed(brand Brand, origin string) bool {
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Hostname() == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || host == "127.0.0.1" {
		return true
	}
	for _, h := range brand.Config().Hosts {
		if strings.EqualFold(h, host) {
			return true
		}
	}
	return false
}

// api_chat_ws: GET /api/chat/ws?token= —— 访客 WebSocket。
// 品牌与主体完全取自令牌（不读 Host / ReqBrand）。握手前拒绝：令牌无效 401、Origin 不符 403、功能关闭（chatSubjectAdmitted）403、
// 本实例该频道已有 chatWSMaxPerChannel 个连接 429。客户端发来的帧一律忽略。
// 心跳由 qtoolkit 的广播实现负责：每 30s ping（低于 CloudFront/ALB 的 60s 空闲，足以保活），
// 70s 收不到 pong 即断（约两次丢失）。其间隔不可配置，与"25s ping"的设想相差 5s，见任务报告。
func api_chat_ws(c *gin.Context) { chatWSHandler(chatBroadcast)(c) }

func chatWSHandler(bcFn func() *redis.Broadcast) gin.HandlerFunc {
	return func(c *gin.Context) {
		s, err := parseChatWSToken(c.Query("token"), time.Now())
		if err != nil || !s.Brand.Valid() {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		if !chatWSOriginAllowed(s.Brand, c.GetHeader("Origin")) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		// 过闸：硬关 / 品牌未开放 / 仅预览而主体没有预览标记，都不让握手（令牌可能是关之前签的）。
		// 已建立的连接不在这里踢。
		if !chatSubjectAdmitted(c.Request.Context(), s) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		bc := bcFn()
		channel := s.Channel()
		// 检查与订阅之间有窗口，并发突发可能略超 5；这是软上限，用来挡单访客开大量连接
		if bc.SubscriberCount(channel) >= chatWSMaxPerChannel {
			c.AbortWithStatus(http.StatusTooManyRequests)
			return
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		rdb := redis.Client()
		key := chatOnlineKey(s)
		// 标记是尽力而为：写失败（或超时）只记日志，握手照常进行
		setCtx, setCancel := context.WithTimeout(ctx, chatPublishTimeout)
		if err := rdb.Set(setCtx, key, "1", chatOnlineTTL).Err(); err != nil {
			log.Warnf(ctx, "chat online marker: %v", err)
		}
		setCancel()
		ttl, refresh := chatOnlineTTL, chatOnlineRefresh
		go func() {
			t := time.NewTicker(refresh)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					_ = rdb.Set(ctx, key, "1", ttl).Err()
				}
			}
		}()

		// 阻塞到连接结束；返回时本连接已取消订阅
		_ = bc.WsSubChannel(c, channel)
		cancel()
		// 只有本实例上最后一个连接断开才删标记。两实例部署下，一端断开可能清掉另一端仍连着的
		// 访客的标记，直到那端下一次续期（<=30s）恢复——离线邮件判断据此容忍最多 30s 的误判。
		if bc.SubscriberCount(channel) == 0 {
			_ = rdb.Del(context.Background(), key).Err()
		}
	}
}
