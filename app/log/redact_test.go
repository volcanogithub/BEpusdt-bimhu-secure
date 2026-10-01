package log

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestB26RedactionNormalErrorsAndSecretValues(t *testing.T) {
	RegisterSecrets("b26-bare-credential")
	for _, input := range []string{
		"password=b26-sensitive", `{"token":"b26-sensitive"}`, "Authorization: Bearer b26-sensitive",
		"Cookie: session=b26-sensitive", "api_key=b26-sensitive", "private key: b26-sensitive",
		"postgres://user:b26-sensitive@host/db", "https://user:b26-sensitive@host/path",
		"https://host/path?key=b26-sensitive", "b26-bare-credential",
		"-----BEGIN PRIVATE KEY-----\nb26-sensitive\n-----END PRIVATE KEY-----",
	} {
		output := Redact(input)
		if strings.Contains(output, "b26-sensitive") || strings.Contains(output, "b26-bare-credential") {
			t.Fatal("sensitive content leaked")
		}
	}
	if got := Redact("normal transaction retry status=200 https://example.invalid/path"); got != "normal transaction retry status=200 https://example.invalid/path" {
		t.Fatal("safe diagnostics destroyed")
	}
}

func TestB26WriterSplitLinesPEMAndOversizedInput(t *testing.T) {
	var out bytes.Buffer
	w := SafeWriter(&out)
	for _, part := range []string{"pass", "word=b26-sensitive\n", "-----BEGIN PRIVATE KEY-----\n", "b26-sensitive\n", "-----END PRIVATE KEY-----\n", strings.Repeat("x", 65537), "\nnormal\n"} {
		if _, err := w.Write([]byte(part)); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Contains(out.String(), "b26-sensitive") || strings.Contains(out.String(), strings.Repeat("x", 100)) {
		t.Fatal("writer leaked split/oversized content")
	}
	if !strings.Contains(out.String(), "normal\n") {
		t.Fatal("writer did not recover")
	}
}

func TestB26ActualLogFilesAndRotationPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	if err := Init(dir); err != nil {
		t.Fatal(err)
	}
	Info("password=b26-log-sensitive")
	Task.WithFields(logrus.Fields{"api_key": "b26-log-sensitive"}).Error("retry")
	Task.Error("https://example.invalid/path?token=b26-log-sensitive")
	Info("safe diagnostic")
	// Exercise lumberjack rotation; the new logfile must inherit 0600.
	if err := loggers[0].Rotate(); err != nil {
		t.Fatal(err)
	}
	Info("safe after rotation")
	Close()
	info, _ := os.Stat(dir)
	if info.Mode().Perm() != 0o700 {
		t.Fatal("log directory not 0700")
	}
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("log %s is not 0600", entry.Name())
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(content), "b26-log-sensitive") {
			t.Fatal("actual log file leaked credential")
		}
	}
}
