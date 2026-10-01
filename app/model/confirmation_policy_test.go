package model

import (
	"bytes"
	"io"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestB23NewInstallDefaults(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "b23.db")), &gorm.Config{})
	if err != nil { t.Fatal(err) }
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	Db = db
	if err := db.AutoMigrate(&Conf{}); err != nil { t.Fatal(err) }
	if err := ConfInit(); err != nil { t.Fatal(err) }
	RefreshC()
	if GetC(BlockOffsetConfirm) != "1" { t.Fatal("new install disables confirmation protection") }
	for _, rule := range confirmationRules {
		var stored Conf
		if err := db.Where("k = ?", rule.key).First(&stored).Error; err != nil { t.Fatal(err) }
		if stored.V != defaultConf[rule.key] { t.Fatalf("missing default %s", rule.key) }
	}
	if err := ValidateConfirmationPolicy(io.Discard); err != nil { t.Fatal(err) }
}

func TestB23DepthBoundariesAndInvalidConfiguration(t *testing.T) {
	original := GetC(ConfirmationDepthTron)
	defer confCache.Store(ConfirmationDepthTron, original)
	confCache.Store(ConfirmationDepthTron, "20")
	for _, sample := range []struct{ head, inclusion int64; ready bool }{
		{119, 100, false}, {120, 100, true}, {121, 100, true},
		{0, 100, false}, {100, 0, false}, {99, 100, false},
	} {
		ready, err := ConfirmationDepthReached("tron", sample.head, sample.inclusion)
		if err != nil || ready != sample.ready { t.Fatalf("%+v got %t %v", sample, ready, err) }
	}
	for _, raw := range []string{"0", "-1", "19", "abc", "1.5", "9223372036854775808"} {
		confCache.Store(ConfirmationDepthTron, raw)
		if _, err := RequiredConfirmationDepth("tron"); err == nil { t.Fatalf("accepted %q", raw) }
		if err := ValidateConfirmationPolicy(io.Discard); err == nil { t.Fatalf("startup accepted %q", raw) }
	}
}

func TestB23LegacyZeroCannotDisableProtection(t *testing.T) {
	legacy, depth := GetC(BlockOffsetConfirm), GetC(ConfirmationDepthTron)
	defer confCache.Store(BlockOffsetConfirm, legacy)
	defer confCache.Store(ConfirmationDepthTron, depth)
	confCache.Store(BlockOffsetConfirm, "0")
	confCache.Store(ConfirmationDepthTron, "20")
	var output bytes.Buffer
	if err := ValidateConfirmationPolicy(&output); err != nil { t.Fatal(err) }
	if !bytes.Contains(output.Bytes(), []byte("WARNING")) { t.Fatal("missing legacy warning") }
	ready, err := ConfirmationDepthReached("tron", 119, 100)
	if err != nil || ready { t.Fatal("legacy zero bypassed policy") }
	confCache.Store(BlockOffsetConfirm, "invalid")
	if err := ValidateConfirmationPolicy(io.Discard); err == nil { t.Fatal("invalid legacy flag accepted") }
}

func TestB23IndependentEVMPolicies(t *testing.T) {
	values := map[ConfKey]string{
		ConfirmationDepthEthereum: "24", ConfirmationDepthBsc: "30", ConfirmationDepthPolygon: "80",
	}
	for key, value := range values {
		original := GetC(key)
		defer confCache.Store(key, original)
		confCache.Store(key, value)
	}
	for network, want := range map[string]int64{"ethereum": 24, "bsc": 30, "polygon": 80} {
		got, err := RequiredConfirmationDepth(network)
		if err != nil || got != want { t.Fatalf("%s got %d %v", network, got, err) }
	}
	if _, err := RequiredConfirmationDepth("unknown"); err == nil { t.Fatal("unknown network allowed") }
}
