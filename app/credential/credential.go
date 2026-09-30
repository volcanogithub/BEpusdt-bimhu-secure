package credential

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Bundle contains plaintext bootstrap credentials before the password is hashed.
// It must never be logged.
type Bundle struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	AdminSecret string `json:"admin_secret,omitempty"`
	AdminPath   string `json:"admin_path"`
	APIToken    string `json:"api_token,omitempty"`
}

func randomURLString(reader io.Reader, size int) (string, error) {
	material := make([]byte, size)
	if _, err := io.ReadFull(reader, material); err != nil {
		return "", fmt.Errorf("read cryptographic random material: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(material), nil
}

// Generate creates independent values using the operating system CSPRNG.
func Generate() (Bundle, error) {
	return generateFrom(rand.Reader)
}

func generateFrom(reader io.Reader) (Bundle, error) {
	username, err := randomURLString(reader, 12)
	if err != nil {
		return Bundle{}, err
	}
	password, err := randomURLString(reader, 24)
	if err != nil {
		return Bundle{}, err
	}
	adminSecret, err := randomURLString(reader, 32)
	if err != nil {
		return Bundle{}, err
	}
	adminPath, err := randomURLString(reader, 18)
	if err != nil {
		return Bundle{}, err
	}
	apiToken, err := randomURLString(reader, 32)
	if err != nil {
		return Bundle{}, err
	}
	return Bundle{
		Username: username, Password: password, AdminSecret: adminSecret,
		AdminPath: "/" + adminPath, APIToken: apiToken,
	}, nil
}

// WriteOneTimeFile persists a CLI reset handoff with owner-only permissions.
func WriteOneTimeFile(bundle Bundle) (string, error) {
	file, err := os.CreateTemp("", "bepusdt-reset-credentials-*.json")
	if err != nil {
		return "", fmt.Errorf("create credential handoff: %w", err)
	}
	path := file.Name()
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return "", fmt.Errorf("protect credential handoff: %w", err)
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(bundle); err != nil {
		return "", fmt.Errorf("write credential handoff: %w", err)
	}
	if err := file.Sync(); err != nil {
		return "", fmt.Errorf("sync credential handoff: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close credential handoff: %w", err)
	}
	ok = true
	return path, nil
}
