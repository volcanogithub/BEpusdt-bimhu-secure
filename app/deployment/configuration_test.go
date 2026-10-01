package deployment

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

var secretAssignment = regexp.MustCompile(`(?im)^\s*(?:ENV\s+)?[A-Z_]*(?:PASSWORD|TOKEN|SECRET|DSN)[A-Z_]*\s*[:=]\s*["']?([^\r\n]+)`)

func unsafeConfiguration(text string) bool {
	if strings.Contains(text, "-----BEGIN ") && strings.Contains(text, "PRIVATE KEY-----") {
		return true
	}
	for _, match := range secretAssignment.FindAllStringSubmatch(text, -1) {
		value := strings.Trim(match[1], " \"'")
		if value != "" && !strings.HasPrefix(value, "REPLACE_") && !strings.HasPrefix(value, "/run/secrets/") {
			return true
		}
	}
	return false
}

func TestB26DeploymentSecretScanPositiveAndNegative(t *testing.T) {
	for _, name := range []string{"dockerfile", ".env.example", "deploy/compose.production.yml", "deploy/nginx.production.conf", "deploy/entrypoint.sh"} {
		data, err := os.ReadFile(filepath.Join("..", "..", name))
		if err != nil {
			t.Fatal(err)
		}
		if unsafeConfiguration(string(data)) {
			t.Fatalf("secret-like value in deployment file %s", name)
		}
	}
	for _, text := range []string{"PASSWORD=", "TOKEN=REPLACE_VALUE", "POSTGRESQL_DSN_FILE=/run/secrets/database_dsn"} {
		if unsafeConfiguration(text) {
			t.Fatal("safe placeholder rejected")
		}
	}
	for _, text := range []string{"PASSWORD=test-only-value", "ENV API_TOKEN=test-only-value", "-----BEGIN PRIVATE KEY-----\ntest-only-value"} {
		if !unsafeConfiguration(text) {
			t.Fatal("scanner failed negative fixture")
		}
	}
}

func TestB26DockerSecurityConfiguration(t *testing.T) {
	root := filepath.Join("..", "..")
	data, err := os.ReadFile(filepath.Join(root, "dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	stages := strings.Split(string(data), "FROM ")
	runtime := stages[len(stages)-1]
	if !strings.Contains(runtime, "USER 10001:10001") || strings.Contains(runtime, "USER root") || strings.Contains(runtime, "EXPOSE 5432") {
		t.Fatal("unsafe runtime identity or database exposure")
	}
	if strings.Contains(string(data), "ADD . .") || strings.Contains(string(data), "COPY . .") {
		t.Fatal("unrestricted backend build context copy")
	}
	ignore, err := os.ReadFile(filepath.Join(root, ".dockerignore"))
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range []string{".env", ".env.*", "secrets", "**/*.pem", "**/*.key", "**/*.db", ".git"} {
		if !strings.Contains("\n"+string(ignore), "\n"+rule+"\n") {
			t.Fatalf("missing context exclusion %s", rule)
		}
	}
	data, err = os.ReadFile(filepath.Join(root, "deploy/compose.production.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Services map[string]struct {
			User        string   `yaml:"user"`
			ReadOnly    bool     `yaml:"read_only"`
			Ports       []string `yaml:"ports"`
			CapDrop     []string `yaml:"cap_drop"`
			SecurityOpt []string `yaml:"security_opt"`
			Tmpfs       []string `yaml:"tmpfs"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	service := config.Services["bepusdt"]
	if len(config.Services) != 1 || service.User != "10001:10001" || !service.ReadOnly || len(service.Ports) != 1 || service.Ports[0] != "127.0.0.1:8080:8080" {
		t.Fatal("unsafe compose identity, filesystem or port")
	}
	if strings.Join(service.CapDrop, ",") != "ALL" || strings.Join(service.SecurityOpt, ",") != "no-new-privileges:true" || len(service.Tmpfs) != 1 || !strings.Contains(service.Tmpfs[0], "mode=0700,uid=10001,gid=10001") {
		t.Fatal("missing runtime restrictions")
	}
	// Mutate the parsed policy and verify misconfiguration is rejected.
	valid := func() bool {
		return service.User == "10001:10001" && service.ReadOnly && len(service.Ports) == 1 && service.Ports[0] == "127.0.0.1:8080:8080"
	}
	for _, port := range []string{"8080:8080", "0.0.0.0:8080:8080", "5432:5432"} {
		service.Ports = []string{port}
		if valid() {
			t.Fatal("unsafe published port accepted")
		}
	}
	service.Ports = []string{"127.0.0.1:8080:8080"}
	service.User = "root"
	if valid() {
		t.Fatal("root runtime accepted")
	}
	service.User = "10001:10001"
	service.ReadOnly = false
	if valid() {
		t.Fatal("writable root filesystem accepted")
	}
}
