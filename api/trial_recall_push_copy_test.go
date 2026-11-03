package center

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTrialRecallPushCopy_NoForbiddenWords(t *testing.T) {
	forbidden := []string{"开途", "Kaitu", "kaitu", "官网", "折", "%", "$", "kaitu.io", "优惠", "价格", "¥"}
	ends := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	for _, stage := range []trialRecallStage{trialRecallS1, trialRecallS2} {
		for _, lang := range []string{"zh-CN", "zh-TW", "en-US", "ja", "", "fr"} {
			title, body := trialRecallPushCopy(stage, lang, ends)
			assert.NotEmpty(t, title, "%s/%s", stage, lang)
			assert.NotEmpty(t, body, "%s/%s", stage, lang)
			for _, w := range forbidden {
				assert.False(t, strings.Contains(title+body, w), "%s/%s contains %q: %s %s", stage, lang, w, title, body)
			}
		}
	}
}

func TestTrialRecallPushCopy_FallbackEnUS(t *testing.T) {
	ends := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	for _, stage := range []trialRecallStage{trialRecallS1, trialRecallS2} {
		wt, wb := trialRecallPushCopy(stage, "en-US", ends)
		gt, gb := trialRecallPushCopy(stage, "fr-FR", ends)
		assert.Equal(t, wt, gt)
		assert.Equal(t, wb, gb)
	}
}

func TestTrialRecallPushCopy_EmptyLanguageIsChinese(t *testing.T) {
	ends := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	for _, stage := range []trialRecallStage{trialRecallS1, trialRecallS2} {
		wt, wb := trialRecallPushCopy(stage, "zh-CN", ends)
		gt, gb := trialRecallPushCopy(stage, "", ends)
		assert.Equal(t, wt, gt)
		assert.Equal(t, wb, gb)
	}
	for in, want := range map[string]string{"zh-Hans": "zh-CN", "zh": "zh-CN", "zh-HK": "zh-TW", "zh-Hant": "zh-TW", "ja": "ja"} {
		assert.Equal(t, want, trialRecallPushLang(in), in)
	}
}

func TestTrialRecallPushCopy_S1Content(t *testing.T) {
	// 2026-11-05 20:00 UTC 在 Asia/Shanghai 已是 11-06。
	ends := time.Date(2026, 11, 5, 20, 0, 0, 0, time.UTC)
	_, body := trialRecallPushCopy(trialRecallS1, "zh-CN", ends)
	assert.Equal(t, "试用将于 2026-11-06 结束，结束后将停止服务", body)
	_, body = trialRecallPushCopy(trialRecallS2, "zh-CN", ends)
	assert.Equal(t, "试用已结束，订阅后立即恢复使用", body)
}
