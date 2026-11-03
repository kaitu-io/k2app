package center

import (
	"fmt"
	"time"
)

// 试用召回推送文案（spec 2026-11-03-ios-trial-recall-design）。
// 不含品牌名、官网、折扣、价格：iOS 自己显示 App 名，营销类文案还要过 App Store 审核。

// trialRecallPushLang 把用户语言归到文案表的四个键。
// 空值按中文处理（开途用户几乎都是中文，空值多为老账号未写语言），其余未知语言用 en-US。
func trialRecallPushLang(language string) string {
	switch language {
	case "", "zh", "zh-CN", "zh-Hans":
		return "zh-CN"
	case "zh-TW", "zh-HK", "zh-Hant":
		return "zh-TW"
	case "ja":
		return "ja"
	default:
		return "en-US"
	}
}

// trialRecallDateString 试用结束日 YYYY-MM-DD（Asia/Shanghai），邮件与推送共用。
func trialRecallDateString(ts int64) string {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	return time.Unix(ts, 0).In(loc).Format("2006-01-02")
}

type trialRecallPushText struct{ title, body string } // S1 的 body 里 %s 为日期

var trialRecallPushTexts = map[trialRecallStage]map[string]trialRecallPushText{
	trialRecallS1: {
		"zh-CN": {"试用即将结束", "试用将于 %s 结束，结束后将停止服务"},
		"zh-TW": {"試用即將結束", "試用將於 %s 結束，結束後將停止服務"},
		"en-US": {"Trial ending soon", "Your trial ends on %s. Service will stop after it ends."},
		"ja":    {"トライアル終了のお知らせ", "トライアルは %s に終了します。終了後はサービスが停止します。"},
	},
	trialRecallS2: {
		"zh-CN": {"试用已结束", "试用已结束，订阅后立即恢复使用"},
		"zh-TW": {"試用已結束", "試用已結束，訂閱後立即恢復使用"},
		"en-US": {"Trial ended", "Your trial has ended. Subscribe to resume right away."},
		"ja":    {"トライアルが終了しました", "トライアルは終了しました。ご登録後すぐにご利用を再開できます。"},
	},
}

// trialRecallPushCopy 返回推送标题与正文；endsAt 仅 S1 用于填日期。
func trialRecallPushCopy(stage trialRecallStage, language string, endsAt time.Time) (title, body string) {
	t := trialRecallPushTexts[stage][trialRecallPushLang(language)]
	body = t.body
	if stage == trialRecallS1 {
		body = fmt.Sprintf(t.body, trialRecallDateString(endsAt.Unix()))
	}
	return t.title, body
}
