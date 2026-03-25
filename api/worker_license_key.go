package center

import (
	"context"
	"fmt"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/mail"
)

const licenseKeyBaseURL = "https://kaitu.io"

// SendLicenseKeyEmails sends gift emails to each old user with their shareable links.
// Called asynchronously after GenerateLicenseKeysForCampaign.
func SendLicenseKeyEmails(ctx context.Context, campaignID uint64) error {
	var keys []LicenseKey
	if err := db.Get().
		Where("campaign_id = ? AND is_used = false", campaignID).
		Find(&keys).Error; err != nil {
		return err
	}
	if len(keys) == 0 {
		log.Infof(ctx, "[LICENSE_KEY] campaign=%d no keys to send emails for", campaignID)
		return nil
	}

	// Group keys by owner
	userKeys := make(map[uint64][]LicenseKey)
	for _, k := range keys {
		if k.CreatedByUserID != nil {
			userKeys[*k.CreatedByUserID] = append(userKeys[*k.CreatedByUserID], k)
		}
	}

	var sent, failed int
	for userID, userKeyList := range userKeys {
		var user User
		if err := db.Get().Preload("LoginIdentifies").First(&user, userID).Error; err != nil {
			log.Warnf(ctx, "[LICENSE_KEY] failed to load user %d: %v", userID, err)
			failed++
			continue
		}
		if err := sendGiftEmail(ctx, user, userKeyList); err != nil {
			log.Warnf(ctx, "[LICENSE_KEY] failed to send email to user %d: %v", userID, err)
			failed++
		} else {
			sent++
		}
	}

	log.Infof(ctx, "[LICENSE_KEY] campaign=%d email send complete: sent=%d failed=%d",
		campaignID, sent, failed)
	return nil
}

func sendGiftEmail(ctx context.Context, user User, keys []LicenseKey) error {
	email := getUserEmailFromIdentifies(&user)
	if email == "" {
		return fmt.Errorf("no email found for user %d", user.ID)
	}

	subject := fmt.Sprintf("你有 %d 个专属礼物名额可以送给朋友", len(keys))

	planDays := licenseKeyTTLDays
	if len(keys) > 0 && keys[0].PlanDays > 0 {
		planDays = keys[0].PlanDays
	}

	body := "感谢你一直以来对 Kaitu 的支持！\n\n"
	body += fmt.Sprintf("作为老用户专属福利，我们送你以下 %d 个礼物链接，可以分享给从未使用过 Kaitu 的朋友。\n", len(keys))
	body += fmt.Sprintf("每个链接可为好友直接开通 %d 天会员：\n\n", planDays)
	for i, k := range keys {
		link := fmt.Sprintf("%s/g/%s", licenseKeyBaseURL, k.Code)
		body += fmt.Sprintf("链接 %d：%s\n", i+1, link)
	}
	body += "\n每个链接只能使用一次，仅限从未购买过的新用户，有效期 30 天。\n"
	body += "\n感谢你的支持！\nKaitu 团队"

	log.Debugf(ctx, "[LICENSE_KEY] sending gift email to %s (%d links)", hideEmail(email), len(keys))
	return MailSend(ctx, &mail.Message{
		To:      email,
		Subject: subject,
		Body:    body,
	})
}
