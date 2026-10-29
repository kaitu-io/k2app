package config

import (
	"os"
	"path/filepath"
	"runtime"
)

// SearchClientConfig searches for a client config file in the standard locations:
// ./k2.yaml, ~/.config/k2/k2.yaml, /etc/k2/k2.yaml (or ProgramData on Windows).
// Returns the first path that exists, or empty string if none found.
func SearchClientConfig() (string, error) {
	candidates := clientConfigCandidates()
	return searchFirstExisting(candidates)
}

// SearchServerConfig searches for a server config file in the standard locations:
// ./k2s.yaml, /etc/k2s/k2s.yaml (or ProgramData on Windows).
// Returns the first path that exists, or empty string if none found.
func SearchServerConfig() (string, error) {
	candidates := serverConfigCandidates()
	return searchFirstExisting(candidates)
}

// searchFirstExisting returns the first path in candidates that exists on disk.
// Returns empty string (not an error) if none exist.
func searchFirstExisting(candidates []string) (string, error) {
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", nil
}

// clientConfigCandidates returns the ordered list of paths to search for client config.
// Order: current directory → user config dir → system config dir.
func clientConfigCandidates() []string {
	candidates := []string{"k2.yaml"}

	// User config dir: ~/.config/k2/k2.yaml
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".config", "k2", "k2.yaml"))
	}

	// System config dir
	if runtime.GOOS == "windows" {
		if pd := os.Getenv("ProgramData"); pd != "" {
			candidates = append(candidates, filepath.Join(pd, "k2", "k2.yaml"))
		}
	} else {
		candidates = append(candidates, "/etc/k2/k2.yaml")
	}

	return candidates
}

// serverConfigCandidates returns the ordered list of paths to search for server config.
// Order: current directory → system config dir.
func serverConfigCandidates() []string {
	candidates := []string{"k2s.yaml"}

	// System config dir
	if runtime.GOOS == "windows" {
		if pd := os.Getenv("ProgramData"); pd != "" {
			candidates = append(candidates, filepath.Join(pd, "k2s", "k2s.yaml"))
		}
	} else {
		candidates = append(candidates, "/etc/k2s/k2s.yaml")
	}

	return candidates
}
