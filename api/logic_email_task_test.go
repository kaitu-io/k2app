package center

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildTranslatedTemplate_PreservesBrand pins the fix for a bug where
// getTemplateForLanguage's lazy-translation path built the new
// EmailMarketingTemplate record without copying Brand from the source —
// every auto-translated template silently fell back to the GORM column
// default ('kaitu'), regardless of the source template's real brand.
func TestBuildTranslatedTemplate_PreservesBrand(t *testing.T) {
	source := EmailMarketingTemplate{
		ID:       42,
		Name:     "Welcome",
		Language: "zh-CN",
		Brand:    string(BrandOverleap),
	}

	got := buildTranslatedTemplate(source, "en-US", "Hi there", "Body copy")

	assert.Equal(t, string(BrandOverleap), got.Brand, "translated template must inherit the source template's brand")
	assert.Equal(t, "Welcome (en-US)", got.Name)
	assert.Equal(t, "en-US", got.Language)
	assert.Equal(t, "Hi there", got.Subject)
	assert.Equal(t, "Body copy", got.Content)
	require.NotNil(t, got.OriginID)
	assert.Equal(t, uint64(42), *got.OriginID)

	// Sanity: kaitu source stays kaitu (no regression on the common path).
	kaituSource := EmailMarketingTemplate{ID: 7, Name: "Reminder", Language: "en-US", Brand: string(BrandKaitu)}
	gotKaitu := buildTranslatedTemplate(kaituSource, "zh-CN", "提醒", "内容")
	assert.Equal(t, string(BrandKaitu), gotKaitu.Brand)
}
