package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/wire"
	"golang.org/x/crypto/bcrypt"
)

// cmdUser handles "k2s user add|list|del|reset".
func cmdUser(args []string) {
	if len(args) == 0 {
		printUserUsage()
		os.Exit(1)
	}

	certDir := config.DefaultServerConfig().CertDir
	usersPath := filepath.Join(certDir, "users")
	urlPath := filepath.Join(certDir, "connect-url.txt")

	switch args[0] {
	case "add":
		if len(args) < 2 {
			out.fail("MISSING_ARG", "username required", "k2s user add <username>")
			os.Exit(1)
		}
		username := args[1]
		if err := validateUsername(username); err != nil {
			out.fail("INVALID_USERNAME", err.Error(), "")
			os.Exit(1)
		}
		cmdUserAdd(usersPath, urlPath, username)

	case "list":
		cmdUserList(usersPath)

	case "del":
		if len(args) < 2 {
			out.fail("MISSING_ARG", "username required", "k2s user del <username>")
			os.Exit(1)
		}
		username := args[1]
		if err := validateUsername(username); err != nil {
			out.fail("INVALID_USERNAME", err.Error(), "")
			os.Exit(1)
		}
		cmdUserDel(usersPath, username)

	case "reset":
		if len(args) < 2 {
			out.fail("MISSING_ARG", "username required", "k2s user reset <username>")
			os.Exit(1)
		}
		username := args[1]
		if err := validateUsername(username); err != nil {
			out.fail("INVALID_USERNAME", err.Error(), "")
			os.Exit(1)
		}
		cmdUserReset(usersPath, urlPath, username)

	case "--help", "-h":
		printUserUsage()
		return

	default:
		out.fail("UNKNOWN_COMMAND", fmt.Sprintf("unknown user subcommand: %s", args[0]), "k2s user --help")
		os.Exit(1)
	}
}

func printUserUsage() {
	fmt.Fprintf(os.Stderr, `Usage:
  k2s user add <username>     Add a new user (generates token, prints connect URL)
  k2s user list               List all users
  k2s user del <username>     Delete a user
  k2s user reset <username>   Regenerate token for a user
`)
}

func validateUsername(name string) error {
	if name == "" {
		return fmt.Errorf("username cannot be empty")
	}
	if len(name) > 32 {
		return fmt.Errorf("username too long (max 32 characters)")
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return fmt.Errorf("username contains invalid character '%c' (allowed: a-z, A-Z, 0-9, -, _)", c)
		}
	}
	return nil
}

// generateToken generates a random 32-char hex token.
func generateToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// hashToken hashes a plaintext token with bcrypt.
func hashToken(token string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash token: %w", err)
	}
	return string(hash), nil
}

func cmdUserAdd(usersPath, urlPath, username string) {
	users, err := loadUsers(usersPath)
	if err != nil {
		out.fail("LOAD_FAILED", err.Error(), "")
		os.Exit(1)
	}
	if _, exists := users[username]; exists {
		out.fail("USER_EXISTS", fmt.Sprintf("user %q already exists", username), fmt.Sprintf("k2s user reset %s", username))
		os.Exit(1)
	}

	token, err := generateToken()
	if err != nil {
		out.fail("TOKEN_FAILED", err.Error(), "")
		os.Exit(1)
	}

	hash, err := hashToken(token)
	if err != nil {
		out.fail("HASH_FAILED", err.Error(), "")
		os.Exit(1)
	}

	if err := appendUser(usersPath, username, hash); err != nil {
		out.fail("WRITE_FAILED", err.Error(), "")
		os.Exit(1)
	}

	url := buildUserURL(urlPath, username, token)
	if out.json {
		data := map[string]string{"username": username, "token": token}
		if url != "" {
			data["url"] = url
		}
		out.ok(data)
		return
	}
	fmt.Printf("\nUser added: %s\n", username)
	if url != "" {
		fmt.Printf("\nConnect URL:\n  %s\n\n", url)
	} else {
		fmt.Println("Server not started yet, run 'k2s setup' first")
	}
}

func cmdUserList(usersPath string) {
	users, err := loadUsers(usersPath)
	if err != nil {
		out.fail("LOAD_FAILED", err.Error(), "")
		os.Exit(1)
	}

	if out.json {
		names := make([]string, 0, len(users))
		for name := range users {
			names = append(names, name)
		}
		out.ok(map[string]any{"users": names, "count": len(names)})
		return
	}

	if len(users) == 0 {
		fmt.Println("No users found")
		return
	}
	fmt.Println("\nUsers:")
	for name := range users {
		fmt.Printf("  - %s\n", name)
	}
	fmt.Printf("\nTotal: %d\n", len(users))
}

func cmdUserDel(usersPath, username string) {
	users, err := loadUsers(usersPath)
	if err != nil {
		out.fail("LOAD_FAILED", err.Error(), "")
		os.Exit(1)
	}
	if _, exists := users[username]; !exists {
		out.fail("NOT_FOUND", fmt.Sprintf("user %q not found", username), "")
		os.Exit(1)
	}

	if err := removeUser(usersPath, username); err != nil {
		out.fail("WRITE_FAILED", err.Error(), "")
		os.Exit(1)
	}

	out.printf("User %s deleted\n", username)
	out.ok(map[string]string{"username": username, "deleted": "true"})
}

func cmdUserReset(usersPath, urlPath, username string) {
	users, err := loadUsers(usersPath)
	if err != nil {
		out.fail("LOAD_FAILED", err.Error(), "")
		os.Exit(1)
	}
	if _, exists := users[username]; !exists {
		out.fail("NOT_FOUND", fmt.Sprintf("user %q not found", username), "")
		os.Exit(1)
	}

	token, err := generateToken()
	if err != nil {
		out.fail("TOKEN_FAILED", err.Error(), "")
		os.Exit(1)
	}

	hash, err := hashToken(token)
	if err != nil {
		out.fail("HASH_FAILED", err.Error(), "")
		os.Exit(1)
	}

	if err := updateUser(usersPath, username, hash); err != nil {
		out.fail("WRITE_FAILED", err.Error(), "")
		os.Exit(1)
	}

	url := buildUserURL(urlPath, username, token)
	if out.json {
		data := map[string]string{"username": username, "token": token}
		if url != "" {
			data["url"] = url
		}
		out.ok(data)
		return
	}
	fmt.Printf("\nToken reset for user %s\n", username)
	if url != "" {
		fmt.Printf("\nConnect URL:\n  %s\n\n", url)
	}
}

// loadUsers reads the users file via wire.LoadUsersFile (single source of truth).
// Returns an empty map if the file doesn't exist.
func loadUsers(path string) (map[string]string, error) {
	users, err := wire.LoadUsersFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]string), nil
		}
		return nil, fmt.Errorf("read users file: %w", err)
	}
	return users, nil
}

// writeUsersFile serializes a users map to the file (single write path).
func writeUsersFile(path string, users map[string]string) error {
	var buf []byte
	buf = append(buf, "# k2s users — managed by k2s user command\n"...)
	for name, hash := range users {
		buf = fmt.Appendf(buf, "%s:%s\n", name, hash)
	}
	return writeFileAtomic(path, buf, 0600)
}

// appendUser adds a user entry to the users file.
func appendUser(path, username, hash string) error {
	users, err := loadUsers(path)
	if err != nil {
		return err
	}
	users[username] = hash
	return writeUsersFile(path, users)
}

// removeUser removes a user from the users file.
func removeUser(path, username string) error {
	users, err := loadUsers(path)
	if err != nil {
		return err
	}
	delete(users, username)
	return writeUsersFile(path, users)
}

// updateUser replaces a user's hash in the users file.
func updateUser(path, username, newHash string) error {
	users, err := loadUsers(path)
	if err != nil {
		return err
	}
	users[username] = newHash
	return writeUsersFile(path, users)
}

// buildUserURL constructs a connect URL for a user by replacing credentials in the template URL.
func buildUserURL(urlPath, username, token string) string {
	templateURL := readConnectURL(urlPath)
	if templateURL == "" {
		return ""
	}
	u, err := url.Parse(templateURL)
	if err != nil {
		return ""
	}
	u.User = url.UserPassword(username, token)
	return u.String()
}

// addFirstUser creates the first user during setup wizard.
// Returns (username, plaintext_token, error).
func addFirstUser(usersPath, defaultName string) (string, string, error) {
	username := promptString("Username", defaultName)
	if err := validateUsername(username); err != nil {
		return "", "", err
	}

	token, err := generateToken()
	if err != nil {
		return "", "", err
	}

	hash, err := hashToken(token)
	if err != nil {
		return "", "", err
	}

	if err := appendUser(usersPath, username, hash); err != nil {
		return "", "", err
	}

	fmt.Printf("Username: %s, token auto-generated\n", username)
	return username, token, nil
}
