package wire

import (
	"bufio"
	"log/slog"
	"os"
	"strings"
)

// LoadUsersFile reads a users file and returns a map of UDID -> Token.
// File format: one user per line as "udid:token".
// Empty lines and lines starting with # are ignored.
func LoadUsersFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	users := make(map[string]string)
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		udid, token, ok := strings.Cut(line, ":")
		if !ok {
			slog.Warn("wire: users file: malformed line (no colon)", "path", path, "line", lineNum)
			continue
		}
		udid = strings.TrimSpace(udid)
		token = strings.TrimSpace(token)
		if udid == "" || token == "" {
			slog.Warn("wire: users file: empty udid or token", "path", path, "line", lineNum)
			continue
		}

		users[udid] = token
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return users, nil
}

// NewUsersFileValidator creates a JWTValidator that checks credentials
// against a users file. The file is re-read on every call to support
// hot reload without server restart.
func NewUsersFileValidator(path string) JWTValidator {
	return func(jwt, udid string) bool {
		users, err := LoadUsersFile(path)
		if err != nil {
			slog.Debug("wire: users file not available", "path", path, "err", err)
			return false
		}
		if len(users) == 0 {
			return false
		}
		token, ok := users[udid]
		if !ok {
			return false
		}
		return token == jwt
	}
}

// ChainValidators returns a JWTValidator that tries each validator in order.
// The first validator to return true wins.
func ChainValidators(validators ...JWTValidator) JWTValidator {
	return func(jwt, udid string) bool {
		for _, v := range validators {
			if v(jwt, udid) {
				return true
			}
		}
		return false
	}
}
