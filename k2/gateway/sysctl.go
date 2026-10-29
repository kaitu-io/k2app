//go:build linux

package gateway

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

type sysctlState struct {
	ipv4Old string
	ipv6Old string
}

const (
	ipv4ForwardPath = "/proc/sys/net/ipv4/ip_forward"
	ipv6ForwardPath = "/proc/sys/net/ipv6/conf/all/forwarding"
)

func (s *sysctlState) Enable() error {
	old, err := os.ReadFile(ipv4ForwardPath)
	if err != nil {
		return fmt.Errorf("read ip_forward: %w", err)
	}
	s.ipv4Old = strings.TrimSpace(string(old))
	if s.ipv4Old != "1" {
		if err := os.WriteFile(ipv4ForwardPath, []byte("1"), 0644); err != nil {
			return fmt.Errorf("enable ipv4 forwarding: %w", err)
		}
	}
	slog.Info("DIAG: gw-sysctl", "key", "ipv4.ip_forward", "oldValue", s.ipv4Old, "newValue", "1")

	if old6, err := os.ReadFile(ipv6ForwardPath); err == nil {
		s.ipv6Old = strings.TrimSpace(string(old6))
		if s.ipv6Old != "1" {
			_ = os.WriteFile(ipv6ForwardPath, []byte("1"), 0644)
		}
		slog.Info("DIAG: gw-sysctl", "key", "ipv6.forwarding", "oldValue", s.ipv6Old, "newValue", "1")
	}
	return nil
}

func (s *sysctlState) Restore() {
	if s.ipv4Old != "" {
		_ = os.WriteFile(ipv4ForwardPath, []byte(s.ipv4Old), 0644)
	}
	if s.ipv6Old != "" {
		_ = os.WriteFile(ipv6ForwardPath, []byte(s.ipv6Old), 0644)
	}
}
