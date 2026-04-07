package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/kaitu-io/k2/k2p"
	"github.com/kaitu-io/k2/wire"
	"golang.org/x/term"
)

// k2pConfig is the on-disk config saved by "k2 peer setup".
type k2pConfig struct {
	UDID       string `json:"udid"`
	RegToken   string `json:"reg_token"`
	SecretHash string `json:"secret_hash"`
	CertDir    string `json:"cert_dir"`
	K2pURL     string `json:"k2p_url"`
	SignalBase string `json:"signal_base"`
}

func cmdPeerSetup(args []string) {
	fs := flag.NewFlagSet("peer setup", flag.ExitOnError)
	udidFlag := fs.String("udid", "", "custom UDID (auto-generated if empty)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: k2 peer setup <k2v5://URL> [--udid NAME]\n\n")
		fs.PrintDefaults()
	}
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Error: provide a k2v5:// URL")
		fs.Usage()
		os.Exit(1)
	}

	// 1. Parse the k2v5 URL to extract signal server info.
	rawURL := fs.Arg(0)
	wireCfg, err := wire.ParseURL(rawURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: parse URL: %v\n", err)
		os.Exit(1)
	}

	// 2. Determine UDID.
	udid := *udidFlag
	if udid == "" {
		udid, err = generateUDID()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: generate UDID: %v\n", err)
			os.Exit(1)
		}
	}
	if err := k2p.ValidateUDID(udid); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// 3. Prompt for password.
	fmt.Fprint(os.Stderr, "Set peer password (min 4 chars): ")
	pwBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr) // newline after hidden input
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: read password: %v\n", err)
		os.Exit(1)
	}
	password := strings.TrimSpace(string(pwBytes))
	if len(password) < 4 {
		fmt.Fprintln(os.Stderr, "Error: password must be at least 4 characters")
		os.Exit(1)
	}

	// 4. Create config dir ~/.k2p/
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: home dir: %v\n", err)
		os.Exit(1)
	}
	configDir := filepath.Join(home, ".k2p")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		fmt.Fprintf(os.Stderr, "Error: create config dir: %v\n", err)
		os.Exit(1)
	}

	// 5. Generate TLS cert pair.
	certPair, err := wire.GenerateCertPair()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: generate cert: %v\n", err)
		os.Exit(1)
	}
	if err := wire.SaveCertPair(configDir, certPair); err != nil {
		fmt.Fprintf(os.Stderr, "Error: save cert: %v\n", err)
		os.Exit(1)
	}

	// 6. Detect private networks.
	networks, dns := k2p.DetectPrivateNetworks()
	fmt.Fprintf(os.Stderr, "Detected networks: %v (DNS: %s)\n", networks, dns)

	// 7. Claim UDID via signal server.
	signalBase := fmt.Sprintf("https://%s:%d", wireCfg.Host, wireCfg.Port)
	signal := k2p.NewSignalClient(signalBase, wireCfg.Token)
	ctx := context.Background()
	regToken, err := signal.Claim(ctx, udid)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: claim UDID: %v\n", err)
		os.Exit(1)
	}

	// 8. Hash password.
	secretHash, err := k2p.HashSecret(password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: hash password: %v\n", err)
		os.Exit(1)
	}

	// 9. Build k2p URL for sharing with away peers.
	pin := certPair.FormatPins()
	extra := url.Values{}
	for _, cidr := range networks {
		extra.Add("net", cidr)
	}
	k2pURL := k2p.BuildURL(udid, password, wireCfg.Host, wireCfg.Port, pin, extra)

	// 10. Save config.
	cfg := k2pConfig{
		UDID:       udid,
		RegToken:   regToken,
		SecretHash: secretHash,
		CertDir:    configDir,
		K2pURL:     k2pURL,
		SignalBase:  signalBase,
	}
	cfgData, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: marshal config: %v\n", err)
		os.Exit(1)
	}
	cfgPath := filepath.Join(configDir, "config.json")
	if err := os.WriteFile(cfgPath, cfgData, 0600); err != nil {
		fmt.Fprintf(os.Stderr, "Error: write config: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "\nSetup complete!\n")
	fmt.Fprintf(os.Stderr, "  UDID:    %s\n", udid)
	fmt.Fprintf(os.Stderr, "  Config:  %s\n", cfgPath)
	fmt.Fprintf(os.Stderr, "\nShare this URL with away peers:\n")
	fmt.Println(k2pURL)
}

// generateUDID creates a random 8-byte hex UDID like "home-a1b2c3d4".
func generateUDID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("random: %w", err)
	}
	return "home-" + hex.EncodeToString(b), nil
}
