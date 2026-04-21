package center

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrderMetaPayUrl(t *testing.T) {
	o := &Order{}
	err := o.SetOrderMeta(&Plan{PID: "pro-1y", Label: "1 年 Pro"}, nil, []string{"uuid-a", "uuid-b"}, false)
	require.NoError(t, err)
	require.Equal(t, "", o.GetPayUrl())

	err = o.SetOrderPayUrl("https://pay.example.com/c/cs_123")
	require.NoError(t, err)
	assert.Equal(t, "https://pay.example.com/c/cs_123", o.GetPayUrl())

	// Other fields survive second marshal
	p, err := o.GetPlan()
	require.NoError(t, err)
	assert.Equal(t, "pro-1y", p.PID)
	assert.False(t, o.GetForMyself())
	assert.Equal(t, []string{"uuid-a", "uuid-b"}, o.GetForUsers())
}

func TestOrderMetaSetPayUrlOnEmpty(t *testing.T) {
	o := &Order{}
	err := o.SetOrderPayUrl("https://pay.example.com/x")
	require.NoError(t, err)
	assert.Equal(t, "https://pay.example.com/x", o.GetPayUrl())
}
