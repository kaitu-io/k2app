package center

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/redis"
)

// 离线邮件（客服聊天）：客服回复时访客已不在线，就给他留的邮箱发一封带回链的邮件。
// 只在客服的文字回复时发；同一会话 10 分钟内最多一封，防止被用来向他人邮箱刷信。
// 主体是登录用户（user）的会话本期不发（第 3 期随工单打通处理）。
// 设计见 docs/superpowers/specs/2026-10-01-support-console-chatwoot-replacement-design.md

const (
	// chatOfflineMailWindow 同一会话两封离线邮件的最小间隔。
	chatOfflineMailWindow = 10 * time.Minute
	// chatResumeLinkTTL 邮件回链令牌的有效期。
	chatResumeLinkTTL = 7 * 24 * time.Hour
	// chatOfflineMailTimeout 钩子触发的一次发信的总上限。
	chatOfflineMailTimeout = 30 * time.Second
	// chatResumePath 回链落地页；令牌跟在 "#chat=" 后面。
	chatResumePath = "/support"
)

var (
	// chatOfflineMailHookEnabled 追加钩子的开关；测试默认关闭（见 logic_chat_offline_mail_test.go 的 init）。
	chatOfflineMailHookEnabled = true
	// chatOfflineMailSend 发信函数，测试替换成桩。
	chatOfflineMailSend = sendSystemEmailAs

	errChatResumeTokenUnavailable = errors.New("chat offline mail: resume token unavailable (jwt.secret not configured)")
)

func init() {
	chatAfterAppend = append(chatAfterAppend, func(conv *Conversation, msg *ConversationMessage) {
		if !chatOfflineMailHookEnabled || !chatOfflineMailCandidate(conv, msg) {
			return
		}
		// 只拷贝值：异步执行时调用方可能已改动 conv / msg
		c, m := *conv, *msg
		chatAsync(func() {
			ctx, cancel := context.WithTimeout(context.Background(), chatOfflineMailTimeout)
			defer cancel()
			if _, err := chatMaybeSendOfflineMail(ctx, &c, &m); err != nil {
				log.Errorf(ctx, "chat offline mail: conv=%d msg=%d err=%v", c.ID, m.ID, err)
			}
		})
	})
}

// chatOfflineMailCandidate 不查库就能判断的前置条件：客服的文字回复、主体是 guest。
func chatOfflineMailCandidate(conv *Conversation, msg *ConversationMessage) bool {
	return conv != nil && msg != nil &&
		msg.SenderType == SenderStaff && msg.Kind == MsgText && conv.SubjectKind == SubjectGuest
}

func chatOfflineMailKey(convID uint64) string { return fmt.Sprintf("chat:mail:%d", convID) }

// chatResumeLink 邮件里的"继续对话"链接。令牌放在 URL 片段（#）里：片段不会发给服务器、不进 Referer。
// 签不出令牌（jwt.secret 未配置）返回 ""。
func chatResumeLink(conv *Conversation) string {
	tok := signChatResumeToken(conv.UUID, chatResumeLinkTTL)
	if tok == "" {
		return ""
	}
	return Brand(conv.Brand).Config().BaseURL + chatResumePath + "#chat=" + tok
}

// chatMaybeSendOfflineMail 客服回复而访客不在线时，给访客留的邮箱发一封带回链的邮件。
// 仅当：客服的文字消息、主体是 guest 且留过邮箱、访客当前不在线、该会话 10 分钟内没发过。
// 不满足条件返回 false, nil；发信失败返回 error，并让出频率窗口（下一条回复还能再试）。
func chatMaybeSendOfflineMail(ctx context.Context, conv *Conversation, msg *ConversationMessage) (sent bool, err error) {
	if !chatOfflineMailCandidate(conv, msg) {
		return false, nil
	}
	// guest 可能已被并入别的簇：邮箱与在线标记都挂在簇根上
	s, err := chatSubjectOfConversation(ctx, conv)
	if err != nil {
		return false, err
	}
	to := guestEmail(ctx, s.ID)
	if to == "" || chatVisitorOnline(ctx, s) {
		return false, nil
	}
	link := chatResumeLink(conv)
	if link == "" {
		return false, errChatResumeTokenUnavailable
	}

	// 频率窗口放在所有条件之后：被跳过的消息不占窗口
	key := chatOfflineMailKey(conv.ID)
	ok, err := redis.Client().SetNX(ctx, key, msg.ID, chatOfflineMailWindow).Result()
	if err != nil {
		return false, fmt.Errorf("chat offline mail window: %w", err)
	}
	if !ok {
		return false, nil
	}

	subject, body := chatOfflineMailContent(s.Brand, msg.Content, link)
	if err := chatOfflineMailSend(ctx, s.Brand, to, subject, body); err != nil {
		// 没发出去就别白占 10 分钟
		if derr := redis.Client().Del(context.WithoutCancel(ctx), key).Err(); derr != nil {
			log.Warnf(ctx, "chat offline mail: release window conv=%d: %v", conv.ID, derr)
		}
		return false, fmt.Errorf("send offline mail: %w", err)
	}
	log.Infof(ctx, "chat offline mail sent: conv=%d to=%s", conv.ID, hideEmail(to))
	return true, nil
}

// chatOfflineMailContent 按品牌出文案（写法同 ticketReplyNotification）。品牌名取注册表的 DisplayName，
// 这里不写任何品牌字面量。
func chatOfflineMailContent(b Brand, reply, link string) (subject, body string) {
	name := b.Config().DisplayName
	if b == BrandOverleap {
		return fmt.Sprintf("[%s] New reply from support", name),
			fmt.Sprintf("Hi,\n\nOur support team replied to your conversation:\n\n---\n%s\n---\n\nContinue the conversation here (link valid for 7 days):\n%s\n\n— The %s Team\n", reply, link, name)
	}
	return fmt.Sprintf("[%s] 客服回复了您的咨询", name),
		fmt.Sprintf("您好，\n\n客服回复了您的咨询：\n\n---\n%s\n---\n\n点击下方链接继续对话（7 天内有效）：\n%s\n\n%s 团队\n", reply, link, name)
}
