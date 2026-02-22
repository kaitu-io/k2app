// gen-bench-config generates configs for the k2 benchmark suite:
//   - Simplified k2s server.yaml (k2s auto-provisions certs/ECH/auth)
//   - sing-box hysteria2 server config (shared for Brutal + BBR clients)
//   - sing-box hysteria2 client configs: Brutal (declared bandwidth) + BBR (adaptive)
//   - Self-signed TLS cert for hysteria2 (separate from k2s)
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

func main() {
	outDir := flag.String("out", "/etc/bench", "output directory")
	serverHost := flag.String("host", "server", "server hostname (Docker DNS)")
	brutalMbps := flag.Int("brutal-mbps", 1000, "declared bandwidth for Hysteria2 Brutal (Mbps)")
	flag.Parse()

	if err := os.MkdirAll(*outDir, 0755); err != nil {
		slog.Error("mkdir", "path", *outDir, "err", err)
		os.Exit(1)
	}

	// 1. k2s server config — auto-provisioning handles certs/ECH/auth.
	k2sCertDir := filepath.Join(*outDir, "k2s")
	if err := os.MkdirAll(k2sCertDir, 0755); err != nil {
		slog.Error("mkdir", "path", k2sCertDir, "err", err)
		os.Exit(1)
	}
	serverCfg := fmt.Sprintf(`listen: ":443"
host: "%s"
cert_dir: "%s"
log:
  level: info
  output: stderr
`, *serverHost, k2sCertDir)
	writeFile(filepath.Join(*outDir, "server.yaml"), serverCfg)

	// 2. Self-signed cert for hysteria2 (separate from k2s auto-provisioned certs).
	hy2CertPath := filepath.Join(*outDir, "hy2.crt")
	hy2KeyPath := filepath.Join(*outDir, "hy2.key")
	if err := generateTLSCert(hy2CertPath, hy2KeyPath, *serverHost); err != nil {
		slog.Error("generate hysteria2 TLS cert", "err", err)
		os.Exit(1)
	}
	fmt.Printf("Hysteria2 TLS cert: %s, key: %s\n", hy2CertPath, hy2KeyPath)

	// 3. sing-box hysteria2 server (shared for both Brutal and BBR clients).
	//    ignore_client_bandwidth=false (default): respects client bandwidth declaration.
	//    Brutal clients send up/down_mbps → server uses Brutal CC.
	//    BBR clients don't send bandwidth → server uses BBR CC.
	hy2Server := map[string]any{
		"log": map[string]any{"level": "warn"},
		"inbounds": []map[string]any{
			{
				"type":        "hysteria2",
				"tag":         "hy2-in",
				"listen":      "::",
				"listen_port": 10013,
				"users": []map[string]any{
					{"password": "bench"},
				},
				"tls": map[string]any{
					"enabled":          true,
					"certificate_path": hy2CertPath,
					"key_path":         hy2KeyPath,
				},
			},
		},
		"outbounds": []map[string]any{
			{"type": "direct", "tag": "direct"},
		},
	}
	writeJSON(filepath.Join(*outDir, "hysteria2-server.json"), hy2Server)

	// 4. sing-box client: Hysteria2 Brutal (SOCKS5 :10003).
	//    Declares up_mbps/down_mbps → activates Brutal CC (fixed-rate, loss-compensating).
	hy2BrutalClient := map[string]any{
		"log": map[string]any{"level": "warn"},
		"inbounds": []map[string]any{
			{
				"type":        "socks",
				"tag":         "socks-in",
				"listen":      "::",
				"listen_port": 10003,
			},
		},
		"outbounds": []map[string]any{
			{
				"type":        "hysteria2",
				"tag":         "hy2-brutal-out",
				"server":      *serverHost,
				"server_port": 10013,
				"password":    "bench",
				"up_mbps":     *brutalMbps,
				"down_mbps":   *brutalMbps,
				"tls": map[string]any{
					"enabled":     true,
					"insecure":    true,
					"server_name": *serverHost,
				},
			},
		},
	}
	writeJSON(filepath.Join(*outDir, "hy2-brutal-client.json"), hy2BrutalClient)

	// 5. sing-box client: Hysteria2 BBR (SOCKS5 :10006).
	//    No up/down_mbps → uses BBR CC (adaptive, no bandwidth declaration needed).
	hy2BBRClient := map[string]any{
		"log": map[string]any{"level": "warn"},
		"inbounds": []map[string]any{
			{
				"type":        "socks",
				"tag":         "socks-in",
				"listen":      "::",
				"listen_port": 10006,
			},
		},
		"outbounds": []map[string]any{
			{
				"type":        "hysteria2",
				"tag":         "hy2-bbr-out",
				"server":      *serverHost,
				"server_port": 10013,
				"password":    "bench",
				"tls": map[string]any{
					"enabled":     true,
					"insecure":    true,
					"server_name": *serverHost,
				},
			},
		},
	}
	writeJSON(filepath.Join(*outDir, "hy2-bbr-client.json"), hy2BBRClient)

	fmt.Printf("Brutal bandwidth: %d Mbps (up/down)\n", *brutalMbps)
	fmt.Println("All bench configs generated.")
}

func writeFile(path, content string) {
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		slog.Error("write file", "path", path, "err", err)
		os.Exit(1)
	}
	fmt.Printf("Config: %s\n", path)
}

func writeJSON(path string, v any) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		slog.Error("marshal JSON", "err", err)
		os.Exit(1)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		slog.Error("write file", "path", path, "err", err)
		os.Exit(1)
	}
	fmt.Printf("Config: %s\n", path)
}

func generateTLSCert(certPath, keyPath, host string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(10 * 365 * 24 * time.Hour),
		DNSNames:     []string{host, "localhost"},
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
