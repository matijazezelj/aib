package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func loadFrom(t *testing.T, body string) (*Config, error) {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)
	p := filepath.Join(t.TempDir(), "aib.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(p)
}

// The bug this guards against: `docker_compose:` is not a key (it is
// `compose:`), the loader dropped it silently, and AIB scanned nothing while
// reporting success.
func TestLoad_UnknownSourceKeyIsAnError(t *testing.T) {
	_, err := loadFrom(t, "sources:\n  docker_compose:\n    - path: /iac\n")
	if err == nil {
		t.Fatal("an unknown sources key must fail loudly")
	}
	if !strings.Contains(err.Error(), "docker_compose") {
		t.Errorf("error should name the offending key, got: %v", err)
	}
}

func TestLoad_UnknownTopLevelKeyIsAnError(t *testing.T) {
	if _, err := loadFrom(t, "scan:\n  schedul: \"4h\"\n"); err == nil {
		t.Fatal("a misspelled key (schedul) must fail loudly")
	}
}

func TestLoad_ValidComposeSource(t *testing.T) {
	cfg, err := loadFrom(t, "sources:\n  compose:\n    - path: /iac/docker\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Sources.Compose) != 1 || cfg.Sources.Compose[0].Path != "/iac/docker" {
		t.Errorf("compose source not loaded: %+v", cfg.Sources.Compose)
	}
}

// Shipped example configs must stay valid under strict loading.
func TestLoad_ShippedConfigsAreStrictClean(t *testing.T) {
	root := filepath.Join("..", "..", "configs")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Skip("no configs dir")
	}
	for _, e := range entries {
		isConfig := strings.HasSuffix(e.Name(), ".yaml") || strings.HasSuffix(e.Name(), ".example")
		if e.IsDir() || !isConfig {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join(root, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := loadFrom(t, string(b)); err != nil {
				t.Errorf("%s no longer loads: %v", e.Name(), err)
			}
		})
	}
}
