// Package deployment applies deployment policy without changing authentication or payment semantics.
package deployment

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// ValidateListener requires explicit acknowledgement of the private proxy network.
func ValidateListener(production, privateNetwork, listen string) error {
	if production == "" || production == "0" {
		return nil
	}
	if production != "1" {
		return fmt.Errorf("BEPUSDT_PRODUCTION must be 0 or 1")
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("production LISTEN must be an IP:port")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("invalid production listen port")
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		return nil
	}
	if privateNetwork == "1" && (host == "" || ip != nil && (ip.IsUnspecified() || ip.IsPrivate())) {
		return nil
	}
	return fmt.Errorf("production backend must bind loopback or explicitly acknowledge BEPUSDT_PRIVATE_NETWORK=1 on an isolated network")
}

func ValidateSecrets(values map[string]string) error {
	for _, name := range []string{"admin_username", "admin_password", "admin_secret", "api_auth_token"} {
		value := strings.TrimSpace(values[name])
		if value == "" || strings.Contains(value, "REPLACE_") {
			return fmt.Errorf("missing production credential: %s", name)
		}
	}
	return nil
}

// ProtectFile tightens a single app-owned file; it never recursively chmods user trees.
func ProtectFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("expected regular private file")
	}
	return os.Chmod(path, 0o600)
}

func PrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("expected private directory")
	}
	return os.Chmod(path, 0o700)
}

func ReadPrivateFile(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("private configuration file unavailable")
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("private configuration file must be regular and mode 0600 or stricter")
	}
	value, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("cannot read private configuration file")
	}
	return strings.TrimSpace(string(value)), nil
}

func DatabaseDSN(value, path string) (string, error) {
	if path == "" {
		return value, nil
	}
	if value != "" {
		return "", fmt.Errorf("configure only POSTGRESQL_DSN or POSTGRESQL_DSN_FILE")
	}
	value, err := ReadPrivateFile(path)
	if err != nil {
		return "", err
	}
	if value == "" || strings.Contains(value, "REPLACE_") {
		return "", fmt.Errorf("database secret file is empty or a placeholder")
	}
	return value, nil
}
