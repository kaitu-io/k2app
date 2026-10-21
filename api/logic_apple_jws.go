package center

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/wordgate/qtoolkit/appstore"
)

// appleJWSRootPEM 验客户端交易 JWS 的信任根。测试替换成自签根。
var appleJWSRootPEM = appstore.AppleRootCAPEM

// Apple 在 App Store 签名证书上打的扩展（与 Apple app-store-server-library 的 SignedDataVerifier 一致）：
// 叶子 = App Store 收据签名，中间 = WWDR。只验链到根不够——Apple 根还签发别的用途的证书。
var (
	appleJWSLeafOID         = asn1.ObjectIdentifier{1, 2, 840, 113635, 100, 6, 11, 1}
	appleJWSIntermediateOID = asn1.ObjectIdentifier{1, 2, 840, 113635, 100, 6, 2, 1}
)

var errAppleJWSInvalid = errors.New("invalid apple signed transaction")

// appleSignedTxn 交易 JWS 里恢复账号要用的字段；其余一律以 Apple Server API 复核结果为准。
type appleSignedTxn struct {
	TransactionID         string `json:"transactionId"`
	OriginalTransactionID string `json:"originalTransactionId"`
	BundleID              string `json:"bundleId"`
}

// verifyAppleSignedTransaction 校验 StoreKit Transaction.jwsRepresentation（spec 2026-10-20 §4.2.1）：
// x5c 证书链到 Apple 根、叶子与中间带 Apple 用途扩展、ES256 签名成立。它证明的是「持有这台
// Apple ID 设备才拿得到的交易」；transactionId 本身是 Apple 全局递增数字，不是秘密。
func verifyAppleSignedTransaction(jws string) (*appleSignedTxn, error) {
	parts := strings.Split(jws, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("%w: not a compact JWS", errAppleJWSInvalid)
	}
	rawHeader, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: header: %v", errAppleJWSInvalid, err)
	}
	var header struct {
		Alg string   `json:"alg"`
		X5c []string `json:"x5c"`
	}
	if err := json.Unmarshal(rawHeader, &header); err != nil {
		return nil, fmt.Errorf("%w: header: %v", errAppleJWSInvalid, err)
	}
	if header.Alg != "ES256" || len(header.X5c) < 2 {
		return nil, fmt.Errorf("%w: alg %q with %d certs", errAppleJWSInvalid, header.Alg, len(header.X5c))
	}
	certs := make([]*x509.Certificate, 2)
	for i := range certs {
		der, err := base64.StdEncoding.DecodeString(header.X5c[i])
		if err != nil {
			return nil, fmt.Errorf("%w: x5c[%d]: %v", errAppleJWSInvalid, i, err)
		}
		if certs[i], err = x509.ParseCertificate(der); err != nil {
			return nil, fmt.Errorf("%w: x5c[%d]: %v", errAppleJWSInvalid, i, err)
		}
	}
	leaf, intermediate := certs[0], certs[1]
	if !certHasExtension(leaf, appleJWSLeafOID) || !certHasExtension(intermediate, appleJWSIntermediateOID) {
		return nil, fmt.Errorf("%w: not an App Store signing chain", errAppleJWSInvalid)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(appleJWSRootPEM) {
		return nil, fmt.Errorf("%w: bad trust root", errAppleJWSInvalid)
	}
	inters := x509.NewCertPool()
	inters.AddCert(intermediate)
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, Intermediates: inters, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}}); err != nil {
		return nil, fmt.Errorf("%w: chain: %v", errAppleJWSInvalid, err)
	}
	pub, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("%w: leaf key is not ECDSA", errAppleJWSInvalid)
	}
	// 交易 JWS 没有 exp：只验签名，不做 claims 时效校验。
	if _, err := jwt.NewParser(jwt.WithValidMethods([]string{"ES256"}), jwt.WithoutClaimsValidation()).
		Parse(jws, func(*jwt.Token) (any, error) { return pub, nil }); err != nil {
		return nil, fmt.Errorf("%w: signature: %v", errAppleJWSInvalid, err)
	}
	rawPayload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: payload: %v", errAppleJWSInvalid, err)
	}
	var txn appleSignedTxn
	if err := json.Unmarshal(rawPayload, &txn); err != nil {
		return nil, fmt.Errorf("%w: payload: %v", errAppleJWSInvalid, err)
	}
	if txn.TransactionID == "" || txn.OriginalTransactionID == "" {
		return nil, fmt.Errorf("%w: missing transaction ids", errAppleJWSInvalid)
	}
	return &txn, nil
}

func certHasExtension(c *x509.Certificate, oid asn1.ObjectIdentifier) bool {
	for _, ext := range c.Extensions {
		if ext.Id.Equal(oid) {
			return true
		}
	}
	return false
}
