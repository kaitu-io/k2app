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

// NewUsersFileValidator creates a Validator that checks credentials against a
// users file. The file is re-read on every call to support hot reload.
//
// It never issues an explicit rejection: a missing file, an empty file, an
// unknown udid, or a wrong token are all "no opinion" (OK=false, Code=0) so a
// chained remote validator — Center — still gets to answer. Shared nodes run
// with an EMPTY users file; treating a miss as a rejection here would shadow
// Center's verdict for every device on the fleet. SvcExp is always 0 on
// success: the users-file path has no subscription clock.
func NewUsersFileValidator(path string) Validator {
	return func(token, udid, mode string) AuthVerdict {
		users, err := LoadUsersFile(path)
		if err != nil {
			slog.Debug("wire: users file not available", "path", path, "err", err)
			return AuthVerdict{}
		}
		if len(users) == 0 {
			return AuthVerdict{}
		}
		storedValue, ok := users[udid]
		if !ok {
			return AuthVerdict{}
		}
		if isBcryptHash(storedValue) {
			if bcrypt.CompareHashAndPassword([]byte(storedValue), []byte(token)) == nil {
				return AuthVerdict{OK: true}
			}
			return AuthVerdict{}
		}
		if storedValue == token {
			return AuthVerdict{OK: true}
		}
		return AuthVerdict{}
	}
}

// ChainValidators tries each validator in order with the five-state semantics
// of AuthVerdict: OK returns immediately; an explicit rejection (Code
// 401/402/403) returns immediately; "cannot determine" (-1) and "node fault"
// (-2) are remembered and the chain continues (a later validator may still say
// yes); "no opinion" (Code 0) is skipped.
//
// Tail rule, most-specific first:
//   - any validator reported a NODE FAULT → {Code:-2}. It outranks -1 because
//     it is not transient: a node with no NodeSecret stays broken until an
//     operator fixes it, and collapsing it into -1 would disguise a permanent
//     misconfiguration as a passing Center blip.
//   - any validator was undetermined → {Code:-1}
//   - otherwise → the fail-closed {Code:401}
func ChainValidators(validators ...Validator) Validator {
	return func(token, udid, mode string) AuthVerdict {
		sawUndetermined := false
		sawNodeFault := false
		for _, v := range validators {
			verdict := v(token, udid, mode)
			if verdict.OK {
				return verdict
			}
			switch {
			case verdict.Code == AuthCodeUndetermined:
				sawUndetermined = true
			case verdict.Code == AuthCodeNodeFault:
				sawNodeFault = true
			case verdict.Code > 0:
				return verdict
			}
		}
		if sawNodeFault {
			return AuthVerdict{OK: false, Code: AuthCodeNodeFault}
		}
		if sawUndetermined {
			return AuthVerdict{OK: false, Code: AuthCodeUndetermined}
		}
		return AuthVerdict{OK: false, Code: 401}
	}
}
