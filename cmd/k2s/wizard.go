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

// biPrint prints a bilingual message (中文 / English).
func biPrint(zh, en string) {
	fmt.Printf("%s / %s\n", zh, en)
}

// biPrintf prints a formatted bilingual message.
func biPrintf(zh, en string, args ...any) {
	// Apply args to both strings
	fmt.Printf("%s / %s\n", fmt.Sprintf(zh, args...), fmt.Sprintf(en, args...))
}

// promptString asks the user for a string value with a default.
func promptString(zh, en, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("%s / %s [%s]: ", zh, en, defaultVal)
	} else {
		fmt.Printf("%s / %s: ", zh, en)
	}
	line, _ := stdinReader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultVal
	}
	return line
}

// promptYesNo asks the user a yes/no question. defaultYes controls the default.
func promptYesNo(zh, en string, defaultYes bool) bool {
	hint := "y/N"
	if defaultYes {
		hint = "Y/n"
	}
	fmt.Printf("%s / %s (%s): ", zh, en, hint)
	line, _ := stdinReader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	if line == "" {
		return defaultYes
	}
	return line == "y" || line == "yes"
}

// promptPort asks the user for a port number, validates availability, and retries on conflict.
func promptPort(zh, en string, defaultPort int) int {
	for {
		input := promptString(zh, en, strconv.Itoa(defaultPort))
		port, err := strconv.Atoi(input)
		if err != nil || port < 1 || port > 65535 {
			biPrint("⚠️  请输入有效端口号 (1-65535)", "⚠️  Please enter a valid port (1-65535)")
			continue
		}

		available, process := checkPort(port)
		if available {
			fmt.Printf("✔ %s %d\n", "端口可用 / Port available:", port)
			return port
		}

		// Port occupied
		fmt.Println()
		biPrintf("⚠️  端口 %d 已被占用", "⚠️  Port %d in use", port)
		if process != "" {
			fmt.Printf("   %s\n", process)
		}
		fmt.Println()
		if port == 443 {
			biPrint("建议使用其他端口", "Try another port")
			biPrint("（非443端口会降低隐身效果）", "(Non-443 reduces stealth)")
		} else {
			biPrint("请选择其他端口", "Please choose another port")
		}
		fmt.Println()
	}
}

// promptHopRange asks for a hop port range, returns start and end (0,0 if disabled).
func promptHopRange() (int, int) {
	enabled := promptYesNo("启用端口跳跃？", "Enable port hopping?", false)
	if !enabled {
		return 0, 0
	}

	input := promptString("跳跃范围", "Hop range", "50000-50100")
	start, end, err := parseRange(input)
	if err != nil {
		biPrint("⚠️  格式无效，使用默认 50000-50100", "⚠️  Invalid format, using default 50000-50100")
		return 50000, 50100
	}
	count := end - start + 1
	fmt.Printf("✔ %s %d-%d (%d ports)\n", "使用 / Using:", start, end, count)
	return start, end
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
	fmt.Println("🔧 k2s 配置向导 / k2s Setup Wizard")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
}

// printComplete prints the setup completion summary.
func printComplete(configPath, url string, hopEnabled bool) {
	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("✅ k2s 配置完成 / Setup complete!")
	fmt.Println()
	fmt.Printf("配置文件 / Config: %s\n", configPath)
	fmt.Println("服务状态 / Service: running ✔")
	fmt.Println()
	fmt.Println("连接 URL / Connect URL:")
	fmt.Printf("  %s\n", url)
	fmt.Println()
	biPrint("添加更多用户", "Add more users")
	fmt.Println("  sudo k2s user add <username>")
	fmt.Println()
	if hopEnabled {
		biPrint("端口跳跃配置指南", "Port hopping guide")
		fmt.Println("  https://kaitu.io/k2/hop-ports")
		fmt.Println()
	}
	biPrint("重新配置", "Reconfigure")
	fmt.Println("  sudo k2s setup --reset")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
}
