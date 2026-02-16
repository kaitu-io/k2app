package center

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"time"

	"github.com/wordgate/qtoolkit/log"
)

// GenerateCA 生成自签名根 CA 证书和私钥（PEM 格式）
// 证书信息仿照 DigiCert 官方 CA，字段完整，兼容 golang 解析
func GenerateCA(ctx context.Context) (caCertPEM, caKeyPEM []byte, err error) {
	log.Infof(ctx, "generating new CA certificate and key")
	org := "DigiCert Inc"
	country := "US"
	province := "Utah"
	locality := "Lehi"
	street := "2801 North Thanksgiving Way, Suite 500"
	postalCode := "84043"
	email := "ca@digicert.com"
	commonName := "DigiCert Global Root CA"

	// 生成 ECDSA P-256 私钥
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Errorf(ctx, "failed to generate private key for CA: %v", err)
		return nil, nil, err
	}

	// 证书序列号
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		log.Errorf(ctx, "failed to generate serial number for CA: %v", err)
		return nil, nil, err
	}

	now := time.Now()
	// 构造证书模板
	caTemplate := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization:  []string{org},
			Country:       []string{country},
			Province:      []string{province},
			Locality:      []string{locality},
			StreetAddress: []string{street},
			PostalCode:    []string{postalCode},
			CommonName:    commonName,
			ExtraNames: []pkix.AttributeTypeAndValue{
				{Type: []int{1, 2, 840, 113549, 1, 9, 1}, Value: email}, // emailAddress
			},
		},
		NotBefore:             now.Add(-10 * time.Minute),
		NotAfter:              now.AddDate(10, 0, 0), // 有效期10年
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            2,
		MaxPathLenZero:        false,
		SubjectKeyId:          []byte{1, 2, 3, 4, 6, 8, 9, 10}, // 可自定义
	}

	// 自签名
	certDER, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &priv.PublicKey, priv)
	if err != nil {
		log.Errorf(ctx, "failed to create CA certificate: %v", err)
		return nil, nil, err
	}

	// PEM 编码证书
	caCertPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})
	// PEM 编码私钥
	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		log.Errorf(ctx, "failed to marshal CA private key: %v", err)
		return nil, nil, err
	}
	caKeyPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyBytes,
	})
	log.Infof(ctx, "successfully generated CA certificate and key")
	return caCertPEM, caKeyPEM, nil
}

// GenerateDomainCert 生成指定域名的 ECDSA 证书对（未签名）
func GenerateDomainCert(ctx context.Context, domain string) (certPEM, keyPEM []byte, err error) {
	log.Infof(ctx, "generating domain certificate request for %s", domain)
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Errorf(ctx, "failed to generate private key for domain %s: %v", domain, err)
		return nil, nil, err
	}
	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		log.Errorf(ctx, "failed to marshal private key for domain %s: %v", domain, err)
		return nil, nil, err
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyBytes,
	})
	// 证书请求模板
	tmpl := x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName: domain,
		},
		DNSNames: []string{domain},
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &tmpl, priv)
	if err != nil {
		log.Errorf(ctx, "failed to create certificate request for domain %s: %v", domain, err)
		return nil, nil, err
	}
	certPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE REQUEST",
		Bytes: csrDER,
	})
	log.Infof(ctx, "successfully generated certificate request for domain %s", domain)
	return certPEM, keyPEM, nil
}

// SignDomainCert 用CA为域名证书签名，返回PEM证书
func SignDomainCert(ctx context.Context, domain string, pubKey interface{}) (certPEM []byte, err error) {
	log.Infof(ctx, "signing domain certificate for %s", domain)
	// 1. 获取CA证书和私钥
	caCertPEM, caKeyPEM, err := GetCa(ctx)
	if err != nil {
		log.Errorf(ctx, "failed to get CA for signing domain cert %s: %v", domain, err)
		return nil, err
	}
	caCertBlock, _ := pem.Decode(caCertPEM)
	if caCertBlock == nil {
		log.Errorf(ctx, "invalid CA cert PEM when signing for %s", domain)
		return nil, errors.New("invalid CA cert PEM")
	}
	caKeyBlock, _ := pem.Decode(caKeyPEM)
	if caKeyBlock == nil {
		log.Errorf(ctx, "invalid CA key PEM when signing for %s", domain)
		return nil, errors.New("invalid CA key PEM")
	}
	caCert, err := x509.ParseCertificate(caCertBlock.Bytes)
	if err != nil {
		log.Errorf(ctx, "failed to parse CA cert when signing for %s: %v", domain, err)
		return nil, err
	}
	caKey, err := x509.ParseECPrivateKey(caKeyBlock.Bytes)
	if err != nil {
		log.Errorf(ctx, "failed to parse CA key when signing for %s: %v", domain, err)
		return nil, err
	}
	// 2. 构造域名证书模板
	now := time.Now()
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		log.Errorf(ctx, "failed to generate serial number for domain cert %s: %v", domain, err)
		return nil, err
	}
	certTmpl := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: domain,
		},
		DNSNames:              []string{domain},
		NotBefore:             now.Add(-10 * time.Minute),
		NotAfter:              now.AddDate(1, 0, 0), // 有效期1年
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &certTmpl, caCert, pubKey, caKey)
	if err != nil {
		log.Errorf(ctx, "failed to create signed certificate for domain %s: %v", domain, err)
		return nil, err
	}
	certPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})
	log.Infof(ctx, "successfully signed certificate for domain %s", domain)
	return certPEM, nil
}

