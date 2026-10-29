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

	clientCfg := fmt.Sprintf(`routes:
  - via: "%s"
    match:
      all: true
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
		gatewayCfg := fmt.Sprintf(`routes:
  - via: "%s"
    match:
      all: true
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

		// 6. Enterprise multi-slot UAT fixtures: a self-signed cert for a
		// mock /api/subs HTTPS endpoint (served from inside the k2r
		// container on localhost by the mocksubs test binary), a k2r.yml
		// variant whose route is k2subs:// instead of a static k2v5://, and
		// the mock's default JSON response. Generated unconditionally
		// (cheap) but only consumed by the `multislot` UAT phase — every
		// other phase keeps using the static k2r.yml above untouched.
		mocksubsCertPath := filepath.Join(*outDir, "mocksubs.crt")
		mocksubsKeyPath := filepath.Join(*outDir, "mocksubs.key")
		if err := generateTLSCertForHosts(mocksubsCertPath, mocksubsKeyPath, []string{"localhost"}); err != nil {
			slog.Error("generate mocksubs TLS cert", "err", err)
			os.Exit(1)
		}
		fmt.Printf("Mocksubs cert: %s, key: %s\n", mocksubsCertPath, mocksubsKeyPath)

		multiSlotCfg := `routes:
  - via: "k2subs://uat:uatpass@localhost:8443/subs/team?refresh=5"
    match:
      all: true
lan_subnets:
  - "10.99.0.0/24"
  - "10.81.1.0/24"
  - "10.81.2.0/24"
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
`
		multiSlotCfgPath := filepath.Join(*outDir, "k2r-multislot.yml")
		if err := os.WriteFile(multiSlotCfgPath, []byte(multiSlotCfg), 0644); err != nil {
			slog.Error("write multi-slot gateway config", "err", err)
			os.Exit(1)
		}
		fmt.Printf("Multi-slot gateway config: %s\n", multiSlotCfgPath)

		deadURL := "k2v5://dead:dead@10.99.0.99:443"
		mocksubsResponse := fmt.Sprintf(`{"tunnels":[{"url":%q,"weight":1},{"url":%q,"weight":1}],"slot_bindings":[{"slot":1,"country":"ae","index":1,"tunnel_index":0},{"slot":2,"country":"gn","index":1,"tunnel_index":1}]}`,
			wireURL, deadURL)
		mocksubsResponsePath := filepath.Join(*outDir, "mocksubs-response.json")
		if err := os.WriteFile(mocksubsResponsePath, []byte(mocksubsResponse), 0644); err != nil {
			slog.Error("write mocksubs response", "err", err)
			os.Exit(1)
		}
		fmt.Printf("Mocksubs response: %s\n", mocksubsResponsePath)
	}
}

func generateTLSCert(certPath, keyPath string) error {
	return generateTLSCertForHosts(certPath, keyPath, []string{"k2s", "localhost"})
}

// generateTLSCertForHosts writes a self-signed EC cert+key for the given
// DNS names. Used both for k2s (wire TLS, verified only via the wire
// layer's own insecure=1 flag) and mocksubs (a stdlib net/http HTTPS
// server, whose Go http.Client DOES verify against the system trust
// store — the UAT entrypoint installs this same cert there).
func generateTLSCertForHosts(certPath, keyPath string, hosts []string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(10 * 365 * 24 * time.Hour),
		DNSNames:     hosts,
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
