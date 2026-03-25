// gen-config generates TLS certs, ECH keys, and config files for k2s/k2 Docker deployment.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"flag"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"github.com/kaitu-io/k2/wire"
)

func main() {
	outDir := flag.String("out", "/etc/k2", "output directory")
	serverHost := flag.String("host", "k2s", "server hostname (for client wire URL)")
	serverPort := flag.Int("port", 443, "server port")
	publicName := flag.String("public-name", "cloudflare-ech.com", "ECH public name (outer SNI)")
	listenAddr := flag.String("listen", "", "daemon HTTP listen address (e.g., 0.0.0.0:1777)")
	mode := flag.String("mode", "proxy", "client mode: proxy or gateway")
	flag.Parse()

	if err := os.MkdirAll(*outDir, 0755); err != nil {
		slog.Error("mkdir", "err", err)
		os.Exit(1)
	}

	// 1. Generate TLS certificate
	certPath := filepath.Join(*outDir, "cert.pem")
	keyPath := filepath.Join(*outDir, "key.pem")
	if err := generateTLSCert(certPath, keyPath); err != nil {
		slog.Error("generate TLS cert", "err", err)
		os.Exit(1)
	}
	fmt.Printf("TLS cert: %s, key: %s\n", certPath, keyPath)

	// 2. Generate ECH key
	echKeyPath := filepath.Join(*outDir, "ech.key")
	echKey, echConfigList, err := wire.GenerateECHKey(*publicName)
	if err != nil {
		slog.Error("generate ECH key", "err", err)
		os.Exit(1)
	}
	if err := wire.SaveECHKey(echKeyPath, echKey.PrivateKey, echConfigList); err != nil {
		slog.Error("save ECH key", "err", err)
		os.Exit(1)
	}
	echB64 := base64.URLEncoding.EncodeToString(echConfigList)
	fmt.Printf("ECH key: %s\n", echKeyPath)
	fmt.Printf("ECH config (base64): %s\n", echB64)

	// 3. Write server config
	serverCfg := fmt.Sprintf(`listen: ":%d"
tls:
  cert: "%s"
  key: "%s"
ech:
  key_path: "%s"
  public_name: "%s"
log:
  level: debug
  output: stderr
`, *serverPort, certPath, keyPath, echKeyPath, *publicName)

	serverCfgPath := filepath.Join(*outDir, "server.yaml")
	if err := os.WriteFile(serverCfgPath, []byte(serverCfg), 0644); err != nil {
		slog.Error("write server config", "err", err)
		os.Exit(1)
	}
	fmt.Printf("Server config: %s\n", serverCfgPath)

	// 4. Write client config
	wireURL := fmt.Sprintf("k2v5://docker-device:docker-token@%s:%d?ech=%s&insecure=1",
		*serverHost, *serverPort, echB64)

	clientCfg := fmt.Sprintf(`server: "%s"
mode: proxy
proxy:
  listen: "0.0.0.0:1080"
dns:
  direct:
    - "8.8.8.8:53"
  proxy:
    - "8.8.8.8:53"
rule:
  global: true
log:
  level: debug
  output: stderr
`, wireURL)

	if *listenAddr != "" {
		clientCfg += fmt.Sprintf("listen: \"%s\"\n", *listenAddr)
	}

	clientCfgPath := filepath.Join(*outDir, "client.yaml")
	if err := os.WriteFile(clientCfgPath, []byte(clientCfg), 0644); err != nil {
		slog.Error("write client config", "err", err)
		os.Exit(1)
	}
	fmt.Printf("Client config: %s\n", clientCfgPath)

	// 5. Write gateway config (k2r) if mode=gateway
	if *mode == "gateway" {
		gatewayCfg := fmt.Sprintf(`server: "%s"
dns:
  direct:
    - "8.8.8.8:53"
  proxy:
    - "8.8.8.8:53"
rule:
  global: true
listen_port: 12345
log:
  level: debug
  output: stderr
`, wireURL)

		if *listenAddr != "" {
			gatewayCfg += fmt.Sprintf("listen: \"%s\"\n", *listenAddr)
		}

		gatewayCfgPath := filepath.Join(*outDir, "k2r.yml")
		if err := os.WriteFile(gatewayCfgPath, []byte(gatewayCfg), 0644); err != nil {
			slog.Error("write gateway config", "err", err)
			os.Exit(1)
		}
		fmt.Printf("Gateway config: %s\n", gatewayCfgPath)
	}
}

func generateTLSCert(certPath, keyPath string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(10 * 365 * 24 * time.Hour),
		DNSNames:     []string{"k2s", "localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return err
	}

	certFile, err := os.Create(certPath)
	if err != nil {
		return err
	}
	defer certFile.Close()
	pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	keyFile, err := os.Create(keyPath)
	if err != nil {
		return err
	}
	defer keyFile.Close()
	pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return nil
}
