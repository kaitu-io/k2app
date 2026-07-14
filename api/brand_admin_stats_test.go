package center

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseBrandFilter(t *testing.T) {
	b, ok := parseBrandFilter("kaitu")
	assert.True(t, ok)
	assert.Equal(t, BrandKaitu, b)

	b, ok = parseBrandFilter("overleap")
	assert.True(t, ok)
	assert.Equal(t, BrandOverleap, b)

	_, ok = parseBrandFilter("") // 空 = 不过滤
	assert.False(t, ok)
	_, ok = parseBrandFilter("bogus")
	assert.False(t, ok)
}
