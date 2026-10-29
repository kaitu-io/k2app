package main

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kaitu-io/k2/config"
	"gopkg.in/yaml.v3"
)

// cmdSetup handles "k2s setup [flags]":
//
// No flags: interactive wizard.
// All required flags (--port, --username, --no-firewall): fully automated (no prompts).
// Partial flags: prompt only for missing values.
func cmdSetup(args []string) {
	fs := flag.NewFlagSet("k2s setup", flag.ExitOnError)
	portFlag := fs.Int("port", 0, "listen port (default: prompt or 443)")
	usernameFlag := fs.String("username", "", "first user to create (default: prompt or prince)")
	noFirewall := fs.Bool("no-firewall", false, "skip firewall configuration")
	reset := fs.Bool("reset", false, "re-run setup (preserves certs and users)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Set up k2s server.

Usage: k2s setup [flags]

  No flags: interactive wizard.
  All flags: fully automated (no prompts).

Flags:
`)
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Examples:
  k2s setup                                            # interactive wizard
  k2s setup --port 443 --username alice --no-firewall  # fully automated
  k2s setup --port 8443                                # prompt only for missing
`)
	}
	fs.Parse(args)

	certDir := config.DefaultServerConfig().CertDir
	configPath := filepath.Join(certDir, "config.yml")
	usersPath := filepath.Join(certDir, "users")
	urlPath := filepath.Join(certDir, "connect-url.txt")

	if *reset {
		os.Remove(configPath)
		fmt.Println("Config reset, re-running setup")
		fmt.Println()
	}

	// If config exists and not reset → start service / print URL.
	if fileExists(configPath) && !*reset {
		if isServiceRunning() {
			url := readConnectURL(urlPath)
			if url == "" {
				url = waitForConnectURL(urlPath, 30*time.Second)
			}
			if url != "" {
				out.printResult(url)
				out.ok(map[string]string{"url": url})
				return
			}
			out.fail("TIMEOUT", "timed out waiting for connect URL", "")
			os.Exit(2)
		}
		fmt.Println("Existing config found, starting service...")
		startServiceWithConfig(configPath)
		waitAndPrintURL(urlPath)
		return
	}

	// Non-interactive if all required params given.
	if *portFlag > 0 && *usernameFlag != "" && *noFirewall {
		runNonInteractiveSetup(certDir, configPath, usersPath, urlPath,
			*portFlag, *usernameFlag)
		return
	}

	// Interactive: prompt for missing params.
	runInteractiveSetup(certDir, configPath, usersPath, urlPath,
		*portFlag, *usernameFlag, *noFirewall)
}

// runNonInteractiveSetup runs fully automated setup with all parameters provided.
func runNonInteractiveSetup(certDir, configPath, usersPath, urlPath string,
	port int, username string) {

	if err := os.MkdirAll(certDir, 0755); err != nil {
		out.fail("DIR_FAILED", err.Error(), "")
		os.Exit(2)
	}

	token, err := generateToken()
	if err != nil {
		out.fail("TOKEN_FAILED", err.Error(), "")
		os.Exit(2)
	}
	hash, err := hashToken(token)
	if err != nil {
		out.fail("HASH_FAILED", err.Error(), "")
		os.Exit(2)
	}
	if err := appendUser(usersPath, username, hash); err != nil {
		out.fail("USER_FAILED", err.Error(), "")
		os.Exit(2)
	}

	cfg := buildMinimalConfig(port, usersPath)
	if err := writeConfigYAML(configPath, cfg); err != nil {
		out.fail("CONFIG_FAILED", err.Error(), "")
		os.Exit(2)
	}

	startServiceWithConfig(configPath)

	url := waitForConnectURL(urlPath, 30*time.Second)
	if url == "" {
		out.fail("TIMEOUT", "timed out waiting for server", "check: sudo journalctl -u k2s -f")
		os.Exit(2)
	}

	url, err = replaceURLCredentials(url, username, token)
	if err != nil {
		out.fail("URL_FAILED", err.Error(), "")
		os.Exit(2)
	}

	out.printf("Config: %s\n", configPath)
	out.printf("Service: started\n")
	out.printf("URL: %s\n", url)
	out.ok(map[string]any{"url": url, "config": configPath, "username": username})
}

// runInteractiveSetup prompts for missing parameters and runs setup.
// Values already provided via flags are used directly without prompting.
func runInteractiveSetup(certDir, configPath, usersPath, urlPath string,
	portGiven int, usernameGiven string, noFirewall bool) {

	if err := os.MkdirAll(certDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating directory %s: %v\n", certDir, err)
		os.Exit(1)
	}

	printHeader()

	// Step 1: Port
	port := portGiven
	if port == 0 {
		port = promptPort("Listen port", 443)
	} else {
		fmt.Printf("Port: %d\n", port)
	}
	fmt.Println()

	// Step 2: Username
	var username, token string
	var err error
	if usernameGiven != "" {
		username = usernameGiven
		token, err = generateToken()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		hash, _ := hashToken(token)
		appendUser(usersPath, username, hash)
		fmt.Printf("User: %s (token auto-generated)\n", username)
	} else {
		username, token, err = addFirstUser(usersPath, "prince")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
	fmt.Println()

	// Step 3: Firewall
	if !noFirewall {
		runFirewallSetup(port)
	}
	fmt.Println()

	// Step 4: Config + start
	fmt.Println("Generating config...")
	cfg := buildMinimalConfig(port, usersPath)
	if err := writeConfigYAML(configPath, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing config: %v\n", err)
		os.Exit(1)
	}

	startServiceWithConfig(configPath)

	url := waitForConnectURL(urlPath, 30*time.Second)
	if url == "" {
		fmt.Fprintln(os.Stderr, "Timed out waiting for server to start.")
		os.Exit(1)
	}

	url, _ = replaceURLCredentials(url, username, token)
	printComplete(configPath, url)
}

// buildMinimalConfig creates a minimal config map with only user-chosen fields.
func buildMinimalConfig(port int, usersPath string) map[string]any {
	cfg := map[string]any{
		"listen": fmt.Sprintf(":%d", port),
		"auth": map[string]any{
			"users_file": usersPath,
		},
		"log": map[string]any{
			"level": "info",
		},
	}
	return cfg
}

// writeConfigYAML writes a config map to a YAML file.
func writeConfigYAML(path string, cfg map[string]any) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	header := "# k2s config — generated by k2s setup\n"
	return writeFileAtomic(path, []byte(header+string(data)), 0644)
}

// startServiceWithConfig installs (if needed) and starts the service
// with the given config file.
func startServiceWithConfig(configPath string) {
	// Stop existing service first if running
	if isServiceRunning() {
		stopRunningService()
	}

	// Install or update service with -c flag
	if err := installServiceWithConfig(configPath); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to install service: %v\n", err)
		fmt.Fprintf(os.Stderr, "Try running with sudo, or use: k2s run -c %s\n", configPath)
		os.Exit(1)
	}
	fmt.Println("Service installed and started")
}

// waitAndPrintURL waits for the connect URL and prints it.
func waitAndPrintURL(urlPath string) {
	fmt.Println()
	fmt.Println("Waiting for server to start...")
	url := waitForConnectURL(urlPath, 30*time.Second)
	if url == "" {
		fmt.Fprintln(os.Stderr, "Timed out waiting for server to start.")
		fmt.Fprintf(os.Stderr, "Check logs: sudo journalctl -u k2s -f\n")
		os.Exit(1)
	}
	fmt.Println()
	fmt.Println("k2s is running. Share this URL with clients:")
	fmt.Println()
	fmt.Println("  " + url)
	fmt.Println()
}

// readConnectURL reads the connect URL from the file, returning empty string if not found.
func readConnectURL(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// waitForConnectURL polls for the connect-url.txt file to appear.
func waitForConnectURL(path string, timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if url := readConnectURL(path); url != "" {
			return url
		}
		time.Sleep(500 * time.Millisecond)
	}
	return ""
}

// replaceURLCredentials replaces the UDID:TOKEN in a k2v5:// URL with username:token.
func replaceURLCredentials(rawURL, username, token string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse URL: %w", err)
	}
	u.User = url.UserPassword(username, token)
	return u.String(), nil
}

// fileExists checks if a file exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
