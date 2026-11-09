package center

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
	"github.com/wordgate/qtoolkit/appstore"
	db "github.com/wordgate/qtoolkit/db"
)

// testAppleCA 自签的「Apple」三级证书链：根 → 中间（WWDR 扩展）→ 叶子（收据签名扩展）。
type testAppleCA struct {
	rootPEM []byte
	x5c     []string
	leafKey *ecdsa.PrivateKey
}

func newTestAppleCA(t *testing.T, leafOID, interOID asn1.ObjectIdentifier) *testAppleCA {
	t.Helper()
	mk := func(cn string, serial int64, isCA bool, oid asn1.ObjectIdentifier, parent *x509.Certificate, parentKey *ecdsa.PrivateKey) (*x509.Certificate, *ecdsa.PrivateKey, []byte) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)
		tmpl := &x509.Certificate{
			SerialNumber:          big.NewInt(serial),
			Subject:               pkix.Name{CommonName: cn},
			NotBefore:             time.Now().Add(-time.Hour),
			NotAfter:              time.Now().Add(time.Hour),
			IsCA:                  isCA,
			BasicConstraintsValid: true,
			KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		}
		if oid != nil {
			tmpl.ExtraExtensions = []pkix.Extension{{Id: oid, Value: []byte{0x05, 0x00}}}
		}
		if parent == nil {
			parent, parentKey = tmpl, key
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, parentKey)
		require.NoError(t, err)
		cert, err := x509.ParseCertificate(der)
		require.NoError(t, err)
		return cert, key, der
	}
	root, rootKey, rootDER := mk("Test Apple Root", 1, true, nil, nil, nil)
	inter, interKey, interDER := mk("Test WWDR", 2, true, interOID, root, rootKey)
	_, leafKey, leafDER := mk("Test Prod ECC Mac App Store", 3, false, leafOID, inter, interKey)
	return &testAppleCA{
		rootPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDER}),
		x5c:     []string{base64.StdEncoding.EncodeToString(leafDER), base64.StdEncoding.EncodeToString(interDER), base64.StdEncoding.EncodeToString(rootDER)},
		leafKey: leafKey,
	}
}

func (ca *testAppleCA) sign(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	tok.Header["x5c"] = ca.x5c
	s, err := tok.SignedString(ca.leafKey)
	require.NoError(t, err)
	return s
}

func useTestAppleRoot(t *testing.T, ca *testAppleCA) {
	t.Helper()
	orig := appleJWSRootPEM
	appleJWSRootPEM = ca.rootPEM
	t.Cleanup(func() { appleJWSRootPEM = orig })
}

func txnClaims(txn, otx string) jwt.MapClaims {
	return jwt.MapClaims{"transactionId": txn, "originalTransactionId": otx, "bundleId": "test.kaitu.bundle"}
}

func TestVerifyAppleSignedTransaction(t *testing.T) {
	ca := newTestAppleCA(t, appleJWSLeafOID, appleJWSIntermediateOID)
	useTestAppleRoot(t, ca)

	t.Run("合法链与签名通过", func(t *testing.T) {
		got, err := verifyAppleSignedTransaction(ca.sign(t, txnClaims("2000001", "1000001")))
		require.NoError(t, err)
		require.Equal(t, "2000001", got.TransactionID)
		require.Equal(t, "1000001", got.OriginalTransactionID)
	})

	t.Run("篡改 payload 被拒", func(t *testing.T) {
		parts := strings.Split(ca.sign(t, txnClaims("2000001", "1000001")), ".")
		forged, _ := json.Marshal(txnClaims("2000002", "1000002"))
		parts[1] = base64.RawURLEncoding.EncodeToString(forged)
		_, err := verifyAppleSignedTransaction(strings.Join(parts, "."))
		require.ErrorIs(t, err, errAppleJWSInvalid)
	})

	t.Run("链不到信任根被拒", func(t *testing.T) {
		other := newTestAppleCA(t, appleJWSLeafOID, appleJWSIntermediateOID)
		_, err := verifyAppleSignedTransaction(other.sign(t, txnClaims("2000001", "1000001")))
		require.ErrorIs(t, err, errAppleJWSInvalid)
	})

	t.Run("叶子缺收据签名扩展被拒", func(t *testing.T) {
		noOID := newTestAppleCA(t, nil, appleJWSIntermediateOID)
		useTestAppleRoot(t, noOID)
		_, err := verifyAppleSignedTransaction(noOID.sign(t, txnClaims("2000001", "1000001")))
		require.ErrorIs(t, err, errAppleJWSInvalid)
	})

	t.Run("中间证书缺 WWDR 扩展被拒", func(t *testing.T) {
		noOID := newTestAppleCA(t, appleJWSLeafOID, nil)
		useTestAppleRoot(t, noOID)
		_, err := verifyAppleSignedTransaction(noOID.sign(t, txnClaims("2000001", "1000001")))
		require.ErrorIs(t, err, errAppleJWSInvalid)
	})

	t.Run("非 ES256 被拒", func(t *testing.T) {
		tok := jwt.NewWithClaims(jwt.SigningMethodHS256, txnClaims("2000001", "1000001"))
		tok.Header["x5c"] = ca.x5c
		s, err := tok.SignedString([]byte("k"))
		require.NoError(t, err)
		_, err = verifyAppleSignedTransaction(s)
		require.ErrorIs(t, err, errAppleJWSInvalid)
	})

	t.Run("非 JWS 被拒", func(t *testing.T) {
		_, err := verifyAppleSignedTransaction("2000001")
		require.ErrorIs(t, err, errAppleJWSInvalid)
	})
}

// ---- 恢复接口（DB 真跑）----

type restoreFixture struct {
	r     *gin.Engine
	ca    *testAppleCA
	owner User
	otx   string
	apple *appstore.TransactionInfo // fetchAppleTransaction 的返回
}

// newRestoreFixture 零身份主人：一台旧设备 + 一条 Apple 订阅行。
func newRestoreFixture(t *testing.T) *restoreFixture {
	t.Helper()
	r := devAcctRouter(t)
	r.POST("/api/auth/device-account/restore", AuthOptional(), api_device_account_restore) // 同 route.go
	ca := newTestAppleCA(t, appleJWSLeafOID, appleJWSIntermediateOID)
	useTestAppleRoot(t, ca)

	origBundle := viper.Get("appstore.bundleId")
	viper.Set("appstore.bundleId", "test.kaitu.bundle")
	t.Cleanup(func() { viper.Set("appstore.bundleId", origBundle) })

	f := &restoreFixture{r: r, ca: ca, otx: fmt.Sprintf("otx-%d", time.Now().UnixNano())}
	f.owner = User{UUID: generateId("user"), Brand: string(BrandKaitu), IsActivated: BoolPtr(true), ExpiredAt: time.Now().Add(300 * 24 * time.Hour).Unix()}
	require.NoError(t, db.Get().Create(&f.owner).Error)
	sub := Subscription{UserID: f.owner.ID, Provider: SubscriptionProviderApple, ProviderSubscriptionID: f.otx, ProductID: "io.kaitu.sub.basic.1y", Status: "active"}
	require.NoError(t, db.Get().Create(&sub).Error)
	t.Cleanup(func() {
		db.Get().Unscoped().Where("user_id = ?", f.owner.ID).Delete(&Device{})
		db.Get().Unscoped().Delete(&Subscription{}, sub.ID)
		db.Get().Where("user_id = ?", f.owner.ID).Delete(&LoginIdentify{})
		db.Get().Unscoped().Delete(&User{}, f.owner.ID)
	})

	f.apple = &appstore.TransactionInfo{TransactionId: "txn-" + f.otx, OriginalTransactionId: f.otx,
		BundleId: "test.kaitu.bundle", ProductId: "io.kaitu.sub.basic.1y"}
	orig := fetchAppleTransaction
	fetchAppleTransaction = func(ctx context.Context, bundleID, txnID string) (*appstore.TransactionInfo, error) {
		if txnID != f.apple.TransactionId {
			return nil, fmt.Errorf("unknown txn %s", txnID)
		}
		cp := *f.apple
		return &cp, nil
	}
	t.Cleanup(func() { fetchAppleTransaction = orig })
	return f
}

func (f *restoreFixture) jws(t *testing.T) string {
	return f.ca.sign(t, txnClaims(f.apple.TransactionId, f.otx))
}

func (f *restoreFixture) ownerDevice(t *testing.T) (udid string, refresh string) {
	t.Helper()
	udid = devAcctUDID(t)
	auth, issueAt, err := generateTokens(context.Background(), f.owner.ID, udid, 0)
	require.NoError(t, err)
	require.NoError(t, db.Get().Create(&Device{UDID: udid, UserID: f.owner.ID, TokenIssueAt: issueAt.Unix(), TokenLastUsedAt: 1}).Error)
	return udid, auth.RefreshToken
}

func postRestore(t *testing.T, r *gin.Engine, host, udid, signed string) devAcctResp {
	t.Helper()
	return postRestoreAs(t, r, host, udid, signed, "")
}

// postRestoreAs 带 Bearer（App 当前登录账号的 access token）调恢复；bearer 空 = 匿名。
func postRestoreAs(t *testing.T, r *gin.Engine, host, udid, signed, bearer string) devAcctResp {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"udid": udid, "signedTransaction": signed, "remark": "iPhone"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/device-account/restore", strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-K2-Client", devAcctClientHeader)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	req.Host = host
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var resp devAcctResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), w.Body.String())
	return resp
}

func devAcctCount(t *testing.T, userID uint64) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Get().Model(&Device{}).Where("user_id = ?", userID).Count(&n).Error)
	return n
}

// 重置手机：新 udid 库里没有 → 给主人建设备行，旧设备下线。
func TestDeviceAccountRestore_NewDeviceAfterReset(t *testing.T) {
	f := newRestoreFixture(t)
	oldUDID, oldRefresh := f.ownerDevice(t)
	newUDID := devAcctUDID(t)

	resp := postRestore(t, f.r, "www.kaitu.io", newUDID, f.jws(t))
	require.Equal(t, 0, resp.Code, resp.Message)

	claims, dev, err := validateToken(context.Background(), resp.Data.RefreshToken, TokenTypeRefresh)
	require.NoError(t, err)
	require.Equal(t, f.owner.ID, claims.UserID)
	require.Equal(t, newUDID, dev.UDID)
	require.Equal(t, "ios", dev.AppPlatform)
	require.Equal(t, int64(1), devAcctCount(t, f.owner.ID), "搬不是共享：仍只 1 台")

	var n int64
	require.NoError(t, db.Get().Model(&Device{}).Where("udid = ?", oldUDID).Count(&n).Error)
	require.Zero(t, n, "旧设备行删除")
	_, _, err = validateToken(context.Background(), oldRefresh, TokenTypeRefresh)
	require.Error(t, err, "旧设备的 token 失效")
}

// 重置后 App 先自动建了空设备账号 C，再恢复：设备从 C 搬给主人。
func TestDeviceAccountRestore_MovesFromEmptyDeviceAccount(t *testing.T) {
	f := newRestoreFixture(t)
	f.ownerDevice(t)
	udid := devAcctUDID(t)
	created := postDeviceAccount(t, f.r, "www.kaitu.io", udid)
	require.Equal(t, 0, created.Code)
	emptyUser := devAcctDevice(t, udid).UserID
	t.Cleanup(func() { db.Get().Unscoped().Delete(&User{}, emptyUser) })

	resp := postRestore(t, f.r, "www.kaitu.io", udid, f.jws(t))
	require.Equal(t, 0, resp.Code, resp.Message)
	d := devAcctDevice(t, udid)
	require.Equal(t, f.owner.ID, d.UserID)
	require.Equal(t, int64(0), devAcctCount(t, emptyUser))
	require.Equal(t, int64(1), devAcctCount(t, f.owner.ID))
	_, _, err := validateToken(context.Background(), created.Data.RefreshToken, TokenTypeRefresh)
	require.Error(t, err, "空账号的 token 失效")
}

// 设备行已属主人（Keychain 丢了 token 的罕见情况）：只重签，不新建。
func TestDeviceAccountRestore_SameDeviceReissues(t *testing.T) {
	f := newRestoreFixture(t)
	udid, _ := f.ownerDevice(t)
	resp := postRestore(t, f.r, "www.kaitu.io", udid, f.jws(t))
	require.Equal(t, 0, resp.Code, resp.Message)
	_, dev, err := validateToken(context.Background(), resp.Data.RefreshToken, TokenTypeRefresh)
	require.NoError(t, err)
	require.Equal(t, udid, dev.UDID)
	require.Equal(t, int64(1), devAcctCount(t, f.owner.ID))
}

// 拒绝时什么都不写：主人设备、设备上现有账号都不动。
func TestDeviceAccountRestore_Refusals(t *testing.T) {
	assertUntouched := func(t *testing.T, f *restoreFixture, ownerUDID, ownerRefresh string) {
		t.Helper()
		require.Equal(t, f.owner.ID, devAcctDevice(t, ownerUDID).UserID)
		_, _, err := validateToken(context.Background(), ownerRefresh, TokenTypeRefresh)
		require.NoError(t, err, "主人原设备照常可用")
	}

	t.Run("订阅链没有订阅行", func(t *testing.T) {
		f := newRestoreFixture(t)
		ownerUDID, ownerRefresh := f.ownerDevice(t)
		f.otx = "otx-unknown-" + f.otx
		f.apple.OriginalTransactionId = f.otx
		resp := postRestore(t, f.r, "www.kaitu.io", devAcctUDID(t), f.jws(t))
		require.Equal(t, int(ErrorAppleSubscriptionOwnedElsewhere), resp.Code)
		assertUntouched(t, f, ownerUDID, ownerRefresh)
	})

	t.Run("家人共享", func(t *testing.T) {
		f := newRestoreFixture(t)
		ownerUDID, ownerRefresh := f.ownerDevice(t)
		f.apple.InAppOwnershipType = appstore.OwnershipType_FAMILY_SHARED
		resp := postRestore(t, f.r, "www.kaitu.io", devAcctUDID(t), f.jws(t))
		require.Equal(t, int(ErrorAppleSubscriptionOwnedElsewhere), resp.Code)
		assertUntouched(t, f, ownerUDID, ownerRefresh)
	})

	t.Run("设备上的账号有会员期", func(t *testing.T) {
		f := newRestoreFixture(t)
		ownerUDID, ownerRefresh := f.ownerDevice(t)
		udid := devAcctUDID(t)
		require.Equal(t, 0, postDeviceAccount(t, f.r, "www.kaitu.io", udid).Code)
		holder := devAcctDevice(t, udid).UserID
		require.NoError(t, db.Get().Model(&User{}).Where("id = ?", holder).Update("expired_at", time.Now().Add(time.Hour).Unix()).Error)
		resp := postRestore(t, f.r, "www.kaitu.io", udid, f.jws(t))
		require.Equal(t, int(ErrorAppleSubscriptionOwnedElsewhere), resp.Code)
		require.Equal(t, holder, devAcctDevice(t, udid).UserID)
		assertUntouched(t, f, ownerUDID, ownerRefresh)
	})

	t.Run("设备上的账号有订阅行", func(t *testing.T) {
		f := newRestoreFixture(t)
		ownerUDID, ownerRefresh := f.ownerDevice(t)
		udid := devAcctUDID(t)
		require.Equal(t, 0, postDeviceAccount(t, f.r, "www.kaitu.io", udid).Code)
		holder := devAcctDevice(t, udid).UserID
		other := Subscription{UserID: holder, Provider: SubscriptionProviderApple, ProviderSubscriptionID: "otx-holder-" + f.otx, ProductID: "p", Status: "expired"}
		require.NoError(t, db.Get().Create(&other).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(&Subscription{}, other.ID) })
		resp := postRestore(t, f.r, "www.kaitu.io", udid, f.jws(t))
		require.Equal(t, int(ErrorAppleSubscriptionOwnedElsewhere), resp.Code)
		require.Equal(t, holder, devAcctDevice(t, udid).UserID)
		assertUntouched(t, f, ownerUDID, ownerRefresh)
	})

	t.Run("设备属有身份账号", func(t *testing.T) {
		f := newRestoreFixture(t)
		ownerUDID, ownerRefresh := f.ownerDevice(t)
		udid := devAcctUDID(t)
		require.Equal(t, 0, postDeviceAccount(t, f.r, "www.kaitu.io", udid).Code)
		holder := devAcctDevice(t, udid).UserID
		require.NoError(t, db.Get().Create(&LoginIdentify{UserID: holder, Type: "email",
			IndexID: "restore-holder-" + f.otx, EncryptedValue: "x", Brand: string(BrandKaitu)}).Error)
		resp := postRestore(t, f.r, "www.kaitu.io", udid, f.jws(t))
		require.Equal(t, int(ErrorAppleSubscriptionOwnedElsewhere), resp.Code, "匿名请求不能把有身份用户的设备挤下线")
		require.Equal(t, holder, devAcctDevice(t, udid).UserID)
		assertUntouched(t, f, ownerUDID, ownerRefresh)
	})

	t.Run("设备属有身份账号，请求以另一个账号登录", func(t *testing.T) {
		f := newRestoreFixture(t)
		ownerUDID, ownerRefresh := f.ownerDevice(t)
		udid := devAcctUDID(t)
		require.Equal(t, 0, postDeviceAccount(t, f.r, "www.kaitu.io", udid).Code)
		holder := devAcctDevice(t, udid).UserID
		require.NoError(t, db.Get().Create(&LoginIdentify{UserID: holder, Type: "email",
			IndexID: "restore-holder2-" + f.otx, EncryptedValue: "x", Brand: string(BrandKaitu)}).Error)
		otherUDID := devAcctUDID(t)
		other := postDeviceAccount(t, f.r, "www.kaitu.io", otherUDID)
		require.Equal(t, 0, other.Code)
		t.Cleanup(func() { db.Get().Unscoped().Delete(&User{}, devAcctDevice(t, otherUDID).UserID) })
		resp := postRestoreAs(t, f.r, "www.kaitu.io", udid, f.jws(t), other.Data.AccessToken)
		require.Equal(t, int(ErrorAppleSubscriptionOwnedElsewhere), resp.Code)
		require.Equal(t, holder, devAcctDevice(t, udid).UserID)
		assertUntouched(t, f, ownerUDID, ownerRefresh)
	})

	t.Run("JWS 无效", func(t *testing.T) {
		f := newRestoreFixture(t)
		ownerUDID, ownerRefresh := f.ownerDevice(t)
		other := newTestAppleCA(t, appleJWSLeafOID, appleJWSIntermediateOID)
		resp := postRestore(t, f.r, "www.kaitu.io", devAcctUDID(t), other.sign(t, txnClaims(f.apple.TransactionId, f.otx)))
		require.Equal(t, int(ErrorInvalidArgument), resp.Code)
		resp = postRestore(t, f.r, "www.kaitu.io", devAcctUDID(t), f.apple.TransactionId)
		require.Equal(t, int(ErrorInvalidArgument), resp.Code, "只给 transactionId 不够")
		assertUntouched(t, f, ownerUDID, ownerRefresh)
	})

	t.Run("JWS 与 Apple 复核的订阅链不一致", func(t *testing.T) {
		f := newRestoreFixture(t)
		ownerUDID, ownerRefresh := f.ownerDevice(t)
		signed := f.ca.sign(t, txnClaims(f.apple.TransactionId, "otx-other"))
		resp := postRestore(t, f.r, "www.kaitu.io", devAcctUDID(t), signed)
		require.Equal(t, int(ErrorInvalidArgument), resp.Code)
		assertUntouched(t, f, ownerUDID, ownerRefresh)
	})

	t.Run("主人被封禁", func(t *testing.T) {
		f := newRestoreFixture(t)
		ownerUDID, _ := f.ownerDevice(t)
		require.NoError(t, db.Get().Model(&User{}).Where("id = ?", f.owner.ID).Update("is_blocked", true).Error)
		resp := postRestore(t, f.r, "www.kaitu.io", devAcctUDID(t), f.jws(t))
		require.Equal(t, int(ErrorForbidden), resp.Code)
		require.Equal(t, f.owner.ID, devAcctDevice(t, ownerUDID).UserID)
		require.Equal(t, int64(1), devAcctCount(t, f.owner.ID))
	})

	t.Run("主人属另一个品牌", func(t *testing.T) {
		f := newRestoreFixture(t)
		ownerUDID, _ := f.ownerDevice(t)
		require.NoError(t, db.Get().Model(&User{}).Where("id = ?", f.owner.ID).Update("brand", string(BrandOverleap)).Error)
		resp := postRestore(t, f.r, "www.kaitu.io", devAcctUDID(t), f.jws(t))
		require.Equal(t, int(ErrorBrandMismatch), resp.Code)
		require.Equal(t, f.owner.ID, devAcctDevice(t, ownerUDID).UserID)
		require.Equal(t, int64(1), devAcctCount(t, f.owner.ID))
	})

	t.Run("Apple 复核的 bundle 不符", func(t *testing.T) {
		f := newRestoreFixture(t)
		ownerUDID, ownerRefresh := f.ownerDevice(t)
		f.apple.BundleId = "test.other.bundle"
		resp := postRestore(t, f.r, "www.kaitu.io", devAcctUDID(t), f.jws(t))
		require.Equal(t, int(ErrorInvalidArgument), resp.Code)
		assertUntouched(t, f, ownerUDID, ownerRefresh)
	})

	t.Run("品牌开关关闭", func(t *testing.T) {
		f := newRestoreFixture(t)
		ownerUDID, ownerRefresh := f.ownerDevice(t)
		resp := postRestore(t, f.r, "www.overleap.io", devAcctUDID(t), f.jws(t))
		require.Equal(t, int(ErrorDeviceAccountDisabled), resp.Code)
		assertUntouched(t, f, ownerUDID, ownerRefresh)
	})
}

// 同一主人并发恢复到两台设备（连点两次 / 两台 iPhone 同时恢复）：都不报系统错误，最终只剩 1 台。
func TestDeviceAccountRestore_ConcurrentSameOwner(t *testing.T) {
	f := newRestoreFixture(t)
	f.ownerDevice(t)
	signed := f.jws(t)
	udids := []string{devAcctUDID(t), devAcctUDID(t), devAcctUDID(t)}
	resps := make([]devAcctResp, len(udids))
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range udids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			resps[i] = postRestore(t, f.r, "www.kaitu.io", udids[i], signed)
		}(i)
	}
	close(start)
	wg.Wait()
	for i, rs := range resps {
		require.Equal(t, 0, rs.Code, "请求 %d: %s", i, rs.Message)
	}
	require.Equal(t, int64(1), devAcctCount(t, f.owner.ID), "轮流挤，最后只剩 1 台")
}

// 2026-11-10 修订（Apple 5.1.1(v)）：主人有登录身份时，这台设备直接加入主人账号——不要求先登录，
// 主人其他设备不动（与零身份主人「搬」不同）。
func TestDeviceAccountRestore_IdentityOwnerJoins(t *testing.T) {
	f := newRestoreFixture(t)
	ownerUDID, ownerRefresh := f.ownerDevice(t)
	require.NoError(t, db.Get().Create(&LoginIdentify{UserID: f.owner.ID, Type: "email",
		IndexID: "restore-idx-" + f.otx, EncryptedValue: "x", Brand: string(BrandKaitu)}).Error)
	newUDID := devAcctUDID(t)

	resp := postRestore(t, f.r, "www.kaitu.io", newUDID, f.jws(t))
	require.Equal(t, 0, resp.Code, resp.Message)
	claims, dev, err := validateToken(context.Background(), resp.Data.RefreshToken, TokenTypeRefresh)
	require.NoError(t, err)
	require.Equal(t, f.owner.ID, claims.UserID)
	require.Equal(t, newUDID, dev.UDID)
	require.Equal(t, int64(2), devAcctCount(t, f.owner.ID), "加入不是搬：原设备保留")
	_, _, err = validateToken(context.Background(), ownerRefresh, TokenTypeRefresh)
	require.NoError(t, err, "主人原设备照常可用")
	require.Equal(t, f.owner.ID, devAcctDevice(t, ownerUDID).UserID)
}

// 有身份主人设备已满：加入时按登录同一规则踢最久未用的那台。
func TestDeviceAccountRestore_IdentityOwnerAtLimitKicksOldest(t *testing.T) {
	f := newRestoreFixture(t)
	require.NoError(t, db.Get().Create(&LoginIdentify{UserID: f.owner.ID, Type: "email",
		IndexID: "restore-idx-" + f.otx, EncryptedValue: "x", Brand: string(BrandKaitu)}).Error)
	max := f.owner.Quota().MaxDevice
	require.Greater(t, max, 1)
	var oldest string
	for i := 0; i < max; i++ {
		udid := devAcctUDID(t)
		require.NoError(t, db.Get().Create(&Device{UDID: udid, UserID: f.owner.ID, TokenIssueAt: 1, TokenLastUsedAt: int64(100 + i)}).Error)
		if i == 0 {
			oldest = udid
		}
	}
	newUDID := devAcctUDID(t)
	resp := postRestore(t, f.r, "www.kaitu.io", newUDID, f.jws(t))
	require.Equal(t, 0, resp.Code, resp.Message)
	require.Equal(t, int64(max), devAcctCount(t, f.owner.ID), "仍在上限内")
	var n int64
	require.NoError(t, db.Get().Model(&Device{}).Where("udid = ?", oldest).Count(&n).Error)
	require.Zero(t, n, "最久未用的设备被踢")
	require.Equal(t, f.owner.ID, devAcctDevice(t, newUDID).UserID)
}

// 审核员路径（4.4.16 被拒）：App 当前以有身份账号 H 登录，这台设备上的 Apple ID 订阅属于主人；
// 以 H 的 token 恢复 → 设备从 H 切到主人，H 在这台设备上的 token 失效，H 的其他设备不动。
func TestDeviceAccountRestore_SignedInHolderSwitchesToOwner(t *testing.T) {
	f := newRestoreFixture(t)
	require.NoError(t, db.Get().Create(&LoginIdentify{UserID: f.owner.ID, Type: "email",
		IndexID: "restore-idx-" + f.otx, EncryptedValue: "x", Brand: string(BrandKaitu)}).Error)
	ownerUDID, ownerRefresh := f.ownerDevice(t)

	udid := devAcctUDID(t)
	created := postDeviceAccount(t, f.r, "www.kaitu.io", udid)
	require.Equal(t, 0, created.Code)
	holder := devAcctDevice(t, udid).UserID
	t.Cleanup(func() {
		db.Get().Where("user_id = ?", holder).Delete(&LoginIdentify{})
		db.Get().Unscoped().Where("user_id = ?", holder).Delete(&Device{})
		db.Get().Unscoped().Delete(&User{}, holder)
	})
	require.NoError(t, db.Get().Create(&LoginIdentify{UserID: holder, Type: "email",
		IndexID: "restore-holder3-" + f.otx, EncryptedValue: "x", Brand: string(BrandKaitu)}).Error)
	holderOther := devAcctUDID(t)
	require.NoError(t, db.Get().Create(&Device{UDID: holderOther, UserID: holder, TokenIssueAt: 1, TokenLastUsedAt: 1}).Error)

	resp := postRestoreAs(t, f.r, "www.kaitu.io", udid, f.jws(t), created.Data.AccessToken)
	require.Equal(t, 0, resp.Code, resp.Message)
	claims, dev, err := validateToken(context.Background(), resp.Data.RefreshToken, TokenTypeRefresh)
	require.NoError(t, err)
	require.Equal(t, f.owner.ID, claims.UserID)
	require.Equal(t, udid, dev.UDID)
	require.Equal(t, f.owner.ID, devAcctDevice(t, udid).UserID)
	_, _, err = validateToken(context.Background(), created.Data.RefreshToken, TokenTypeRefresh)
	require.Error(t, err, "H 在这台设备上的 token 失效")
	require.Equal(t, holder, devAcctDevice(t, holderOther).UserID, "H 的其他设备不动")
	_, _, err = validateToken(context.Background(), ownerRefresh, TokenTypeRefresh)
	require.NoError(t, err, "主人原设备照常可用")
	require.Equal(t, f.owner.ID, devAcctDevice(t, ownerUDID).UserID)
}

// 安全审查（2026-11-10）：凭 JWS 进入有身份账号的两条硬边界——员工 / 管理员账号与被撤销的交易。
func TestDeviceAccountRestore_IdentityOwnerHardLimits(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, f *restoreFixture)
	}{
		{"主人带员工角色", func(t *testing.T, f *restoreFixture) {
			require.NoError(t, db.Get().Model(&User{}).Where("id = ?", f.owner.ID).Update("roles", RoleUser|RoleSupport).Error)
		}},
		{"主人是超级管理员", func(t *testing.T, f *restoreFixture) {
			require.NoError(t, db.Get().Model(&User{}).Where("id = ?", f.owner.ID).Update("is_admin", true).Error)
		}},
		{"交易已被退款撤销", func(t *testing.T, f *restoreFixture) {
			f.apple.RevocationDate = time.Now().UnixMilli()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRestoreFixture(t)
			ownerUDID, ownerRefresh := f.ownerDevice(t)
			require.NoError(t, db.Get().Create(&LoginIdentify{UserID: f.owner.ID, Type: "email",
				IndexID: "restore-hard-" + f.otx, EncryptedValue: "x", Brand: string(BrandKaitu)}).Error)
			tc.setup(t, f)
			newUDID := devAcctUDID(t)
			resp := postRestore(t, f.r, "www.kaitu.io", newUDID, f.jws(t))
			require.Equal(t, int(ErrorAppleSubscriptionOwnedElsewhere), resp.Code)
			require.Empty(t, resp.Data.AccessToken)
			require.Equal(t, int64(1), devAcctCount(t, f.owner.ID))
			require.Equal(t, f.owner.ID, devAcctDevice(t, ownerUDID).UserID)
			_, _, err := validateToken(context.Background(), ownerRefresh, TokenTypeRefresh)
			require.NoError(t, err)
		})
	}
}

// 接线守卫：恢复接口请求体含 Apple 签名交易（凭它即可进入主人账号）、响应体含 token，
// 只能挂在不记 body 的组上（log.MiddlewareRequestLog(true) 会把两者原样写进 app.log）。
func TestDeviceAccountRestoreRoute_NotBodyLogged(t *testing.T) {
	src, err := os.ReadFile("route.go")
	require.NoError(t, err)
	s := string(src)
	require.Contains(t, s, `restoreSensitive.POST("/auth/device-account/restore", AuthOptional(), api_device_account_restore)`)
	require.Equal(t, 1, strings.Count(s, "api_device_account_restore)"), "不得在别的组重复注册")
	use := regexp.MustCompile(`restoreSensitive\.Use\(([^\n]*)\)`).FindStringSubmatch(s)
	require.NotNil(t, use)
	require.Contains(t, use[1], "requestLogWithoutBodies()")
	require.NotContains(t, use[1], "MiddlewareRequestLog")
}
