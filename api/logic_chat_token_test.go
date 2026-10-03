package center

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func chatTokenSecret(t *testing.T) {
	t.Helper()
	old := viper.GetString("jwt.secret")
	viper.Set("jwt.secret", "test-secret-for-chat-tokens")
	t.Cleanup(func() { viper.Set("jwt.secret", old) })
}

func TestChatWSToken_RoundTripAndExpiry(t *testing.T) {
	chatTokenSecret(t)
	s := chatSubject{Brand: BrandKaitu, Kind: SubjectGuest, ID: 42}
	tok := signChatWSToken(s, 5*time.Minute)
	require.NotEmpty(t, tok)

	got, err := parseChatWSToken(tok, time.Now())
	require.NoError(t, err)
	assert.Equal(t, s, got)

	_, err = parseChatWSToken(tok, time.Now().Add(6*time.Minute))
	assert.Error(t, err, "过期令牌必须被拒")

	// 每个位置逐一篡改（两种替换字符），每一次都必须被拒。
	// 严格 base64 解码保证"改签名末位字符"不会解码成同样的字节而侥幸通过。
	for i := 0; i < len(tok); i++ {
		for _, repl := range []byte{'A', 'B'} {
			b := []byte(tok)
			if b[i] == repl || b[i] == '.' {
				continue
			}
			b[i] = repl
			_, err := parseChatWSToken(string(b), time.Now())
			assert.Errorf(t, err, "篡改第 %d 个字符为 %q 后仍被接受", i, repl)
		}
	}
	// 签名末位字符：遍历整个 base64url 字母表，任何与原值不同的字符都必须被拒
	// （非严格解码下，与原值只差尾部填充比特的字符会解码成同样字节而被接受）。
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	for _, ch := range []byte(alphabet) {
		if ch == tok[len(tok)-1] {
			continue
		}
		_, err := parseChatWSToken(tok[:len(tok)-1]+string(ch), time.Now())
		assert.Errorf(t, err, "签名末位改为 %q 后仍被接受", ch)
	}
	// 去掉/多加分隔符
	_, err = parseChatWSToken(strings.Replace(tok, ".", "", 1), time.Now())
	assert.Error(t, err)

	// resume 令牌不能冒充 ws 令牌
	resume := signChatResumeToken("conv-uuid-1", time.Hour)
	_, err = parseChatWSToken(resume, time.Now())
	assert.Error(t, err)
	// 反向亦然
	_, err = parseChatResumeToken(tok, time.Now())
	assert.Error(t, err)
}

func TestChatResumeToken_RoundTripAndExpiry(t *testing.T) {
	chatTokenSecret(t)
	tok := signChatResumeToken("conv-uuid-1", 7*24*time.Hour)
	got, err := parseChatResumeToken(tok, time.Now())
	require.NoError(t, err)
	assert.Equal(t, "conv-uuid-1", got)
	_, err = parseChatResumeToken(tok, time.Now().Add(8*24*time.Hour))
	assert.Error(t, err)
	_, err = parseChatResumeToken("garbage", time.Now())
	assert.Error(t, err)
	_, err = parseChatResumeToken(strings.Repeat("a.", 3), time.Now())
	assert.Error(t, err)
}

func TestChatToken_EmptySecretFailsClosed(t *testing.T) {
	old := viper.GetString("jwt.secret")
	viper.Set("jwt.secret", "")
	t.Cleanup(func() { viper.Set("jwt.secret", old) })
	assert.Empty(t, signChatWSToken(chatSubject{Brand: BrandKaitu, Kind: SubjectGuest, ID: 1}, time.Minute))
	assert.Empty(t, signChatResumeToken("x", time.Minute))
}

// forgeChatToken 按给定载荷、用 signWith 用途的密钥签一个令牌，隔离"密钥派生"与"载荷用途"两层校验。
func forgeChatToken(t *testing.T, claims chatTokenClaims, signWith string) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	body := base64.RawURLEncoding.EncodeToString(payload)
	m := hmac.New(sha256.New, chatTokenKey(signWith))
	m.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func TestChatToken_PurposeSeparationLayers(t *testing.T) {
	chatTokenSecret(t)
	exp := time.Now().Add(time.Minute).Unix()
	ws := chatTokenClaims{P: chatWSPurpose, B: "kaitu", K: SubjectGuest, I: 1, E: exp}
	// 载荷自称 ws，但用 resume 的密钥签 → 密钥层必须拒
	_, err := parseChatWSToken(forgeChatToken(t, ws, chatResumePurpose), time.Now())
	assert.Error(t, err, "密钥不同必须拒")
	// 用 ws 密钥签、载荷用途却写成 resume → 用途层必须拒
	bad := ws
	bad.P = chatResumePurpose
	_, err = parseChatWSToken(forgeChatToken(t, bad, chatWSPurpose), time.Now())
	assert.Error(t, err, "载荷用途不符必须拒")
	// 对照：用对密钥与用途则通过
	_, err = parseChatWSToken(forgeChatToken(t, ws, chatWSPurpose), time.Now())
	assert.NoError(t, err)
}
