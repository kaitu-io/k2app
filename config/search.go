package config

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
	return "", nil // RED: not implemented
}

// clientConfigCandidates returns the ordered list of paths to search for client config.
func clientConfigCandidates() []string {
	return nil // RED: not implemented
}

// serverConfigCandidates returns the ordered list of paths to search for server config.
func serverConfigCandidates() []string {
	return nil // RED: not implemented
}
