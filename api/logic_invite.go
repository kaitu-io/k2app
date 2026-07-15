package center

import (
	db "github.com/wordgate/qtoolkit/db"
	"context"
	"fmt"
	"time"

	"github.com/rs/xid"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/unred"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 分享链接配置
const (
	shareLinkValidHours = 24 // 分享链接有效期：24小时
)


// createInviteShareLink 为邀请码创建微信防红短链接
// 每次生成不同的随机短链接路径，一个邀请码可以有多个有效的分享链接
// expiresInDays: 链接有效期（天数，1-365）
func createInviteShareLink(ctx context.Context, b Brand, inviteCode InviteCode, expiresInDays int) (string, error) {
	code := inviteCode.GetCode()

	// 目标 URL：真实的邀请落地页 https://www.kaitu.io/s/{code}（品牌化）
	targetURL := inviteCode.Link(b)

	// 短链接路径：使用随机 xid，与邀请码完全解耦，确保每次生成唯一路径
	// /s/{xid} - 短小且唯一的随机 ID（20字符）
	randomID := xid.New().String()
	shortPath := fmt.Sprintf("/s/%s", randomID)

	// 过期时间：根据参数计算（天数转换为秒）
	expireAt := time.Now().Add(time.Duration(expiresInDays) * 24 * time.Hour).Unix()

	log.Infof(ctx, "generating share link for code %s: shortPath=%s, targetURL=%s", code, shortPath, targetURL)

	// 使用 unred 创建短链接（微信防红）
	response, err := unred.CreateLink(shortPath, targetURL, expireAt)
	if err != nil {
		log.Errorf(ctx, "failed to create unred short link for code %s: %v", code, err)
		return "", fmt.Errorf("failed to create short link: %w", err)
	}

	if !response.Success {
		log.Errorf(ctx, "unred API returned failure for code %s: %s", code, response.Message)
		return "", fmt.Errorf("unred API error: %s", response.Message)
	}

	shortLink := response.URL
	log.Infof(ctx, "created share link for code %s: %s -> %s", code, shortLink, targetURL)

	return shortLink, nil
}

// handleInviteDownloadReward 处理邀请下载奖励（已废弃但保留兼容）
func handleInviteDownloadReward(ctx context.Context, userID uint64) (*UserProHistory, error) {
	log.Infof(ctx, "handling invite download reward for user %d (deprecated)", userID)
	var history *UserProHistory
	err := db.Get().Transaction(func(tx *gorm.DB) error {
		// 获取用户信息，包括邀请码和邀请人关联
		var user User
		if err := tx.Preload("InvitedByCode.User").First(&user, userID).Error; err != nil {
			log.Warnf(ctx, "failed to get user %d for download reward: %v", userID, err)
			return err
		}

		// 如果没有邀请码，直接返回
		if user.InvitedByCode == nil {
			log.Infof(ctx, "user %d has no invite code, skipping download reward", userID)
			return nil
		}
		log.Infof(ctx, "user %d was invited by code %s from user %d", userID, user.InvitedByCode.GetCode(), user.InvitedByCode.UserID)

		// 下载奖励功能已废弃，直接返回
		log.Infof(ctx, "download reward feature is deprecated, skipping reward for user %d", userID)
		return nil
	})
	if err != nil {
		log.Errorf(ctx, "transaction failed for invite download reward for user %d: %v", userID, err)
	}
	return history, err
}

// handleInvitePurchaseRewardInTx 处理邀请购买奖励（同步执行，在事务中）
// 必须在 ApplyOrderToTargetUsers 之前调用，因为后者会设置 IsFirstOrderDone=true
//
// 并发安全设计：
// - buyer FOR UPDATE：序列化同一买家的并发首购，防止 IsFirstOrderDone 检查被 snapshot 绕过
// - inviter FOR UPDATE：序列化同一邀请人的并发奖励，防止 expired_at lost update
func handleInvitePurchaseRewardInTx(ctx context.Context, tx *gorm.DB, userID uint64, orderID uint64) error {
	log.Infof(ctx, "[InviteReward] processing for user %d, order %d", userID, orderID)

	// 1. FOR UPDATE 锁定买家行，读取最新已提交的 IsFirstOrderDone
	// 注意：Preload 走独立 SELECT 不带 FOR UPDATE，InvitedByCode 是不可变记录所以安全
	var user User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Preload("InvitedByCode").First(&user, userID).Error; err != nil {
		return err
	}

	// 2. 检查是否是首次购买（FOR UPDATE 保证读到最新值）
	if user.IsFirstOrderDone != nil && *user.IsFirstOrderDone {
		log.Infof(ctx, "[InviteReward] user %d is not first order, skipping", userID)
		return nil
	}

	// 3. 如果没有邀请码，跳过
	if user.InvitedByCode == nil {
		log.Infof(ctx, "[InviteReward] user %d has no invite code, skipping", userID)
		return nil
	}

	inviteCodeID := user.InvitedByCode.ID
	inviterUserID := user.InvitedByCode.UserID

	// 4. FOR UPDATE 锁定邀请人行，读取最新 expired_at（防止并发 lost update）
	var inviter User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&inviter, inviterUserID).Error; err != nil {
		log.Warnf(ctx, "[InviteReward] user %d has invite code but inviter %d not found: %v", userID, inviterUserID, err)
		return nil
	}

	// 5. 为被邀请人添加购买奖励
	days := configInvite(ctx).PurchaseRewardDays
	if days > 0 {
		if _, err := addProExpiredDays(ctx, tx, &user, VipInvitedReward,
			inviteCodeID, days, "被邀请首次购买奖励"); err != nil {
			return err
		}
		log.Infof(ctx, "[InviteReward] added %d days to invitee %d", days, user.ID)
	}

	// 6. 为邀请人添加奖励
	inviterDays := configInvite(ctx).InviterPurchaseRewardDays
	if inviterDays > 0 {
		if _, err := addProExpiredDays(ctx, tx, &inviter, VipInviteReward,
			inviteCodeID, inviterDays, "邀请用户首次购买奖励"); err != nil {
			return err
		}
		log.Infof(ctx, "[InviteReward] added %d days to inviter %d", inviterDays, inviter.ID)
	}

	return nil
}
