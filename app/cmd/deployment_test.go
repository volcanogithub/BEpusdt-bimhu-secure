package cmd

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
	"github.com/v03413/bepusdt/app/model"
)

func TestB26ProductionStartupNormalMissingSecretAndUnsafeListener(t *testing.T) {
	for _, scenario := range []string{"normal", "missing-secret", "unsafe-listener", "missing-file"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("BEPUSDT_PRODUCTION", "1")
			t.Setenv("BEPUSDT_PRIVATE_NETWORK", "")
			t.Setenv("POSTGRESQL_DSN", "")
			t.Setenv("POSTGRESQL_DSN_FILE", "")
			dir := t.TempDir()
			db := filepath.Join(dir, "data", "sqlite.db")
			if scenario == "missing-secret" {
				if err := model.Init(db, ""); err != nil {
					t.Fatal(err)
				}
				if err := model.SetSecretValues(map[model.ConfKey]string{model.AdminSecret: ""}); err != nil {
					t.Fatal(err)
				}
				model.Close()
			}
			if scenario == "missing-file" {
				t.Setenv("POSTGRESQL_DSN_FILE", filepath.Join(dir, "missing"))
			}
			command := *Start
			command.Action = func(context.Context, *cli.Command) error { return nil }
			root := &cli.Command{Name: "bepusdt", Commands: []*cli.Command{&command}}
			listen := "127.0.0.1:8080"
			if scenario == "unsafe-listener" {
				listen = "0.0.0.0:8080"
			}
			err := root.Run(context.Background(), []string{"bepusdt", "start", "--listen", listen, "--sqlite", db, "--log", filepath.Join(dir, "logs")})
			if scenario == "normal" {
				if err != nil {
					t.Fatal(err)
				}
				info, err := os.Stat(db)
				if err != nil || info.Mode().Perm() != 0o600 {
					t.Fatal("production database not 0600")
				}
			} else {
				if err == nil {
					t.Fatal("misconfigured production startup succeeded")
				}
				if scenario == "missing-secret" && !strings.Contains(err.Error(), "missing production credential") {
					t.Fatal("missing secret not explicitly rejected")
				}
			}
		})
	}
}

func TestB26OccupiedListenerFailsBeforeStartup(t *testing.T) {
	t.Setenv("BEPUSDT_PRODUCTION", "1")
	t.Setenv("POSTGRESQL_DSN", "")
	t.Setenv("POSTGRESQL_DSN_FILE", "")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	dir := t.TempDir()
	command := *Start
	root := &cli.Command{Name: "bepusdt", Commands: []*cli.Command{&command}}
	err = root.Run(context.Background(), []string{"bepusdt", "start", "--listen", listener.Addr().String(), "--sqlite", filepath.Join(dir, "data", "sqlite.db"), "--log", filepath.Join(dir, "logs")})
	if err == nil || !strings.Contains(err.Error(), "backend listen failed") {
		t.Fatal("occupied listener did not fail startup")
	}
}
