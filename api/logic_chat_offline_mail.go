package center

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
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

// chatMailAddrOK 只接受单个裸地址：能被 net/mail 解析、解析结果与原串一致（排除 "名字 <a@b>" 形式），
// 且不含 CR/LF（邮件头注入）与逗号（多收件人）。
func chatMailAddrOK(addr string) bool {
	if strings.ContainsAny(addr, "\r\n,") {
		return false
	}
	a, err := mail.ParseAddress(addr)
	return err == nil && a.Address == addr
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
	if to == "" {
		return false, nil
	}
	// 邮箱是访客自报的：发信前再校验一次，绝不把能夹带多收件人 / 邮件头的串交给发信函数
	if !chatMailAddrOK(to) {
		log.Warnf(ctx, "chat offline mail: conv=%d refused malformed address %q", conv.ID, to)
		return false, nil
	}
	if chatVisitorOnline(ctx, s) {
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
		// 没发出去就别白占 10 分钟。无条件删：超时这类失败其实可能已经发出，
		// 那样下一条回复会再发一封——可以接受重复一封，不能接受漏发。
		if derr := redis.Client().Del(context.WithoutCancel(ctx), key).Err(); derr != nil {
			log.Warnf(ctx, "chat offline mail: release window conv=%d: %v", conv.ID, derr)
		}
		return false, fmt.Errorf("send offline mail: %w", err)
	}
	log.Infof(ctx, "chat offline mail sent: conv=%d to=%s", conv.ID, hideEmail(to))
	return true, nil
}
