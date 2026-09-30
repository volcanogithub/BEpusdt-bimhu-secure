package model

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestB21BootstrapLogsContainNoPlaintextCredentials(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "bootstrap.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Conf{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })

	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	Db = db.Session(&gorm.Session{Logger: logger.New(log.New(writer, "", 0), logger.Config{LogLevel: logger.Info})})
	initErr := ConfInit()
	_ = writer.Close()
	os.Stdout = oldStdout
	output, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if initErr != nil {
		t.Fatal(initErr)
	}
	if readErr != nil {
		t.Fatal(readErr)
	}

	info := GetInstallInfo()
	values := make([]string, 0, 5)
	for _, key := range []string{"username", "password", "secure", "token"} {
		value, ok := info[key].(string)
		if !ok || value == "" {
			t.Fatalf("missing one-time install value %q", key)
		}
		values = append(values, value)
	}
	var adminSecret Conf
	if err := db.Where("k = ?", AdminSecret).First(&adminSecret).Error; err != nil {
		t.Fatal(err)
	}
	values = append(values, adminSecret.V)
	for _, value := range values {
		if strings.Contains(string(output), value) {
			t.Fatal("stdout or ORM log leaked a plaintext credential")
		}
	}
	var password Conf
	if err := db.Where("k = ?", AdminPassword).First(&password).Error; err != nil {
		t.Fatal(err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(password.V), []byte(info["password"].(string))); err != nil {
		t.Fatal("bootstrap password was not stored with compatible bcrypt hashing")
	}
}

func TestB21UpgradePreservesExistingCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Conf{}); err != nil {
		t.Fatal(err)
	}
	existing := map[ConfKey]string{
		AdminUsername: "existing-admin",
		AdminPassword: "$2a$10$abcdefghijklmnopqrstuuuuuuuuuuuuuuuuuuuuuuuuuuuuu",
		AdminSecret:   "existing-secret",
		AdminSecure:   "/existing-path",
		ApiAuthToken:  "existing-token",
	}
	for key, value := range existing {
		if err := db.Create(&Conf{K: key, V: value}).Error; err != nil {
			t.Fatal(err)
		}
	}
	sqlDB, _ := db.DB()
	_ = sqlDB.Close()

	if err := Init(path, ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(Close)
	for key, want := range existing {
		if got := GetK(key); got != want {
			t.Fatalf("upgrade changed %s: got %q want %q", key, got, want)
		}
	}
}

func TestB21SecretRotationLogsContainNoPlaintext(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "rotation.db")), &gorm.Config{
		Logger: logger.New(log.New(writer, "", 0), logger.Config{LogLevel: logger.Info}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Conf{}); err != nil {
		t.Fatal(err)
	}
	Db = db
	values := map[ConfKey]string{
		AdminUsername: "rotation-user-plaintext",
		AdminPassword: "rotation-password-hash",
		AdminSecure:   "/rotation-path-plaintext",
		ApiAuthToken:  "rotation-token-plaintext",
	}
	if err := SetSecretValues(values); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	output, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range values {
		if strings.Contains(string(output), value) {
			t.Fatal("ORM log leaked a rotated credential")
		}
	}
	sqlDB, _ := db.DB()
	_ = sqlDB.Close()
}
