package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

var stdinReader = bufio.NewReader(os.Stdin)

// promptString asks the user for a string value with a default.
func promptString(prompt, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("%s [%s]: ", prompt, defaultVal)
	} else {
		fmt.Printf("%s: ", prompt)
	}
	line, _ := stdinReader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultVal
	}
	return line
}

// promptYesNo asks the user a yes/no question. defaultYes controls the default.
func promptYesNo(prompt string, defaultYes bool) bool {
	hint := "y/N"
	if defaultYes {
		hint = "Y/n"
	}
	fmt.Printf("%s (%s): ", prompt, hint)
	line, _ := stdinReader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	if line == "" {
		return defaultYes
	}
	return line == "y" || line == "yes"
}

// promptPort asks the user for a port number, validates availability, and retries on conflict.
func promptPort(prompt string, defaultPort int) int {
	for {
		input := promptString(prompt, strconv.Itoa(defaultPort))
		port, err := strconv.Atoi(input)
		if err != nil || port < 1 || port > 65535 {
			fmt.Println("Please enter a valid port (1-65535)")
			continue
		}

		available, process := checkPort(port)
		if available {
			fmt.Printf("Port available: %d\n", port)
			return port
		}

		// Port occupied
		fmt.Println()
		fmt.Printf("Port %d in use\n", port)
		if process != "" {
			fmt.Printf("   %s\n", process)
		}
		fmt.Println()
		if port == 443 {
			fmt.Println("Try another port (non-443 reduces stealth)")
		} else {
			fmt.Println("Please choose another port")
		}
		fmt.Println()
	}
}

// parseRange parses "50000-50100" into start, end.
func parseRange(s string) (int, int, error) {
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected START-END")
	}
	start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, err
	}
	end, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, err
	}
	if start < 1 || end > 65535 || start >= end {
		return 0, 0, fmt.Errorf("invalid range")
	}
	return start, end, nil
}

// checkPort checks if a TCP port is available. Returns (available, processInfo).
func checkPort(port int) (bool, string) {
	// Try TCP
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		process := getPortProcess(port)
		return false, process
	}
	ln.Close()

	// Try UDP
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", port))
	if err != nil {
		return true, "" // TCP ok, UDP resolve failed — unusual, treat as available
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		process := getPortProcess(port)
		return false, process
	}
	conn.Close()

	return true, ""
}

// getPortProcess tries to identify what process is using a port via ss or lsof.
func getPortProcess(port int) string {
	// Try ss first (Linux)
	out, err := exec.Command("ss", "-tlunp", fmt.Sprintf("sport = :%d", port)).CombinedOutput()
	if err == nil {
		lines := strings.Split(string(out), "\n")
		for _, line := range lines[1:] { // skip header
			line = strings.TrimSpace(line)
			if line != "" {
				// Extract process info from ss output
				if idx := strings.Index(line, "users:"); idx >= 0 {
					return line[idx:]
				}
				return line
			}
		}
	}

	// Try lsof as fallback
	out, err = exec.Command("lsof", "-i", fmt.Sprintf(":%d", port), "-P", "-n").CombinedOutput()
	if err == nil {
		lines := strings.Split(string(out), "\n")
		if len(lines) > 1 {
			return strings.TrimSpace(lines[1])
		}
	}

	return ""
}

// printHeader prints the wizard header.
func printHeader() {
	fmt.Println()
	fmt.Println("k2s Setup Wizard")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
}

// printComplete prints the setup completion summary.
func printComplete(configPath, url string) {
	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("Setup complete!")
	fmt.Println()
	fmt.Printf("Config: %s\n", configPath)
	fmt.Println("Service: running")
	fmt.Println()
	fmt.Println("Connect URL:")
	fmt.Printf("  %s\n", url)
	fmt.Println()
	fmt.Println("Add more users:")
	fmt.Println("  sudo k2s user add <username>")
	fmt.Println()
	fmt.Println("Reconfigure:")
	fmt.Println("  sudo k2s setup --reset")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
}
