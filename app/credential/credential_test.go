package credential

import (
	"bytes"
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestB21GenerateOneThousandUniqueCredentials(t *testing.T) {
	passwords := make(map[string]struct{}, 1000)
	tokens := make(map[string]struct{}, 1000)
	secrets := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		bundle, err := Generate()
		if err != nil {
			t.Fatal(err)
		}
		for value, seen := range map[string]map[string]struct{}{
			bundle.Password: passwords, bundle.APIToken: tokens, bundle.AdminSecret: secrets,
		} {
			if _, exists := seen[value]; exists {
				t.Fatalf("duplicate credential at iteration %d", i)
			}
			seen[value] = struct{}{}
		}
	}
}

func TestB21GenerationHasNoTimeInput(t *testing.T) {
	material := bytes.Repeat([]byte{0x5a}, 118)
	first, err := generateFrom(bytes.NewReader(material))
	if err != nil {
		t.Fatal(err)
	}
	second, err := generateFrom(bytes.NewReader(material))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("output changed despite identical CSPRNG material; a non-random input may be involved")
	}
	// Different wall-clock start times cannot affect this output: generation
	// consumes only the supplied random reader and accepts no clock or seed.
}

func TestB21OneTimeFileIsOwnerOnly(t *testing.T) {
	bundle, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	path, err := WriteOneTimeFile(bundle)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("credential file mode = %o, want 600", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{bundle.Password, bundle.APIToken, bundle.AdminSecret} {
		if !strings.Contains(string(data), value) {
			t.Fatal("one-time handoff omitted a credential")
		}
	}
}
