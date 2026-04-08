package wire

import (
	"bufio"
	"log/slog"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// LoadUsersFile reads a users file and returns a map of username -> token_or_hash.
// File format: one user per line as "username:token_or_bcrypt_hash".
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

		name, tokenOrHash, ok := strings.Cut(line, ":")
		if !ok {
			slog.Warn("wire: users file: malformed line (no colon)", "path", path, "line", lineNum)
			continue
		}
		name = strings.TrimSpace(name)
		tokenOrHash = strings.TrimSpace(tokenOrHash)
		if name == "" || tokenOrHash == "" {
			slog.Warn("wire: users file: empty name or token", "path", path, "line", lineNum)
			continue
		}

		users[name] = tokenOrHash
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return users, nil
}

// isBcryptHash checks if a string looks like a bcrypt hash.
func isBcryptHash(s string) bool {
	return strings.HasPrefix(s, "$2a$") || strings.HasPrefix(s, "$2b$") || strings.HasPrefix(s, "$2y$")
}

// NewUsersFileValidator creates a JWTValidator that checks credentials
// against a users file. The file is re-read on every call to support
// hot reload without server restart.
//
// Supports both bcrypt hashed tokens (username:$2a$...) and plaintext
// tokens (username:token) for backward compatibility.
func NewUsersFileValidator(path string) JWTValidator {
	return func(jwt, udid, mode string) bool {
		users, err := LoadUsersFile(path)
		if err != nil {
			slog.Debug("wire: users file not available", "path", path, "err", err)
			return false
		}
		if len(users) == 0 {
			return false
		}
		storedValue, ok := users[udid]
		if !ok {
			return false
		}

		// bcrypt hash — compare with bcrypt
		if isBcryptHash(storedValue) {
			return bcrypt.CompareHashAndPassword([]byte(storedValue), []byte(jwt)) == nil
		}

		// Plaintext token — direct comparison (backward compat)
		return storedValue == jwt
	}
}

// ChainValidators returns a JWTValidator that tries each validator in order.
// The first validator to return true wins.
func ChainValidators(validators ...JWTValidator) JWTValidator {
	return func(jwt, udid, mode string) bool {
		for _, v := range validators {
			if v(jwt, udid, mode) {
				return true
			}
		}
		return false
	}
}
