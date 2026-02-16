// Package waymaker 提供 WayMaker/k2oc 协议支持
// 使用 certtool (GnuTLS) 签名证书，与 wgcenter 完全一致
package waymaker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/wordgate/qtoolkit/log"
)

var (
	caTaskLock sync.Mutex
	setupOnce  sync.Once
	workDir    string // 运行时工作目录，存放 CA 文件和脚本
)

// ensureWorkDir 确保工作目录存在且包含所需文件
func ensureWorkDir() error {
	var setupErr error
	setupOnce.Do(func() {
		// 创建临时工作目录
		dir, err := os.MkdirTemp("", "waymaker-ca-*")
		if err != nil {
			setupErr = fmt.Errorf("failed to create temp dir: %w", err)
			return
		}
		workDir = dir

		// 写入 CA 证书
		certPath := filepath.Join(dir, "ca_cert.pem")
		if err := os.WriteFile(certPath, LegacyCACert, 0600); err != nil {
			setupErr = fmt.Errorf("failed to write CA cert: %w", err)
			return
		}

		// 写入 CA 私钥
		keyPath := filepath.Join(dir, "ca_key.pem")
		if err := os.WriteFile(keyPath, LegacyCAKey, 0600); err != nil {
			setupErr = fmt.Errorf("failed to write CA key: %w", err)
			return
		}

		// 写入生成脚本
		scriptPath := filepath.Join(dir, "generate-key4domain.sh")
		if err := os.WriteFile(scriptPath, GenerateScript, 0755); err != nil {
			setupErr = fmt.Errorf("failed to write generate script: %w", err)
			return
		}
	})
	return setupErr
}

// KeyPairOfDomain 为域名生成密钥对和证书（使用 certtool）
// 与 wgcenter 的 KeyPairOfDomain 完全一致
func KeyPairOfDomain(ctx context.Context, domain string) (privateKey string, certificate string, err error) {
	caTaskLock.Lock()
	defer caTaskLock.Unlock()

	// 确保工作目录已准备好
	if err := ensureWorkDir(); err != nil {
		log.Errorf(ctx, "waymaker: failed to setup work dir: %v", err)
		return "", "", err
	}

	privateKey, certificate, err = generateKeyPair(ctx, domain)
	if err != nil {
		return "", "", err
	}

	return privateKey, certificate, nil
}

func generateKeyPair(ctx context.Context, domain string) (privateKey string, certificate string, err error) {
	scriptPath := filepath.Join(workDir, "generate-key4domain.sh")

	cmd := exec.Command("bash", scriptPath, domain)
	cmd.Dir = workDir

	out, err := cmd.CombinedOutput()
	log.Infof(ctx, "waymaker: generated key pair output: %s", out)
	if err != nil {
		log.Errorf(ctx, "waymaker: generate key pair for domain %s, err: %v", domain, err)
		return "", "", fmt.Errorf("waymaker: certtool failed: %w", err)
	}

	prvF := fmt.Sprintf("/tmp/%s-key.pem", domain)
	certF := fmt.Sprintf("/tmp/%s-cert.pem", domain)
	prvB, err := os.ReadFile(prvF)
	if err != nil {
		log.Errorf(ctx, "waymaker: read private key for domain %s, err: %v", domain, err)
		return "", "", fmt.Errorf("waymaker: read private key failed: %w", err)
	}
	certB, err := os.ReadFile(certF)
	if err != nil {
		log.Errorf(ctx, "waymaker: read certificate for domain %s, err: %v", domain, err)
		return "", "", fmt.Errorf("waymaker: read certificate failed: %w", err)
	}

	// 清理临时文件
	os.Remove(prvF)
	os.Remove(certF)
	os.Remove(fmt.Sprintf("/tmp/%s.tmpl", domain))

	log.Infof(ctx, "waymaker: generate key pair for domain ok, key len=%d cert len=%d", len(prvB), len(certB))
	return string(prvB), string(certB), nil
}

// GetCACert 返回 Legacy CA 证书 PEM
func GetCACert() []byte {
	return LegacyCACert
}
