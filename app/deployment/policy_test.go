package deployment

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestB26ProductionListenerNormalAndMisconfiguration(t *testing.T) {
	for _, tc := range []struct {
		production, private, listen string
		ok                          bool
	}{
		{"", "", ":8080", true}, {"1", "", "127.0.0.1:8080", true}, {"1", "", "[::1]:8080", true},
		{"1", "1", "0.0.0.0:8080", true}, {"1", "1", "10.0.0.1:8080", true},
		{"1", "", ":8080", false}, {"1", "", "0.0.0.0:8080", false}, {"1", "1", "8.8.8.8:8080", false},
		{"1", "", "localhost:8080", false}, {"1", "", "127.0.0.1:0", false}, {"1", "", "127.0.0.1:65536", false},
		{"invalid", "", "127.0.0.1:8080", false},
	} {
		if err := ValidateListener(tc.production, tc.private, tc.listen); (err == nil) != tc.ok {
			t.Fatalf("listener policy %+v: %v", tc, err)
		}
	}
}

func TestB26MissingProductionSecrets(t *testing.T) {
	values := map[string]string{}
	for _, name := range []string{"admin_username", "admin_password", "admin_secret", "api_auth_token"} {
		values[name] = "REPLACE_" + name
	}
	if ValidateSecrets(values) == nil {
		t.Fatal("placeholder credentials accepted")
	}
	for name := range values {
		values[name] = "test-only-" + name
	}
	if err := ValidateSecrets(values); err != nil {
		t.Fatal(err)
	}
	for name, value := range values {
		values[name] = ""
		if err := ValidateSecrets(values); err == nil || strings.Contains(err.Error(), value) {
			t.Fatal("missing credential accepted or value leaked")
		}
		values[name] = value
	}
}

func TestB26PrivateFilesDirectoriesAndSecretFileFailures(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("POSIX mode validation targets Linux")
	}
	dir := filepath.Join(t.TempDir(), "private")
	if err := PrivateDirectory(dir); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(dir)
	if info.Mode().Perm() != 0o700 {
		t.Fatal("directory not 0700")
	}
	path := filepath.Join(dir, "dsn")
	if err := os.WriteFile(path, []byte("REPLACE_DATABASE_DSN"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPrivateFile(path); err == nil {
		t.Fatal("world-readable config accepted")
	}
	if err := ProtectFile(path); err != nil {
		t.Fatal(err)
	}
	info, _ = os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatal("file not 0600")
	}
	if _, err := DatabaseDSN("", path); err == nil {
		t.Fatal("placeholder DSN accepted")
	}
	if err := os.WriteFile(path, []byte("test-only-dsn\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := DatabaseDSN("", path); err != nil || got != "test-only-dsn" {
		t.Fatal("private secret file cannot be loaded")
	}
	if _, err := DatabaseDSN("existing", path); err == nil {
		t.Fatal("ambiguous secret sources accepted")
	}
	if _, err := DatabaseDSN("", path+"missing"); err == nil {
		t.Fatal("missing secret file accepted")
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := DatabaseDSN("", path); err == nil {
		t.Fatal("empty secret file accepted")
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPrivateFile(link); err == nil {
		t.Fatal("symlink secret accepted")
	}
	if ProtectFile(link) == nil || PrivateDirectory(link) == nil {
		t.Fatal("nonregular path accepted")
	}
}
