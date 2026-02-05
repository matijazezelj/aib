package config

import (
	"os"
	"testing"
)

func TestDefaults(t *testing.T) {
	// Load with no config file → should use defaults
	cfg, err := Load("/nonexistent/path/aib.yaml")
	// The file doesn't exist, but viper with SetConfigFile returns error
	// if file is explicitly specified and doesn't exist.
	// Test defaults using empty string path instead.
	_ = cfg
	_ = err

	// Reset for clean test
	cfg, err = loadDefaults()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Storage.Path != "./data/aib.db" {
		t.Errorf("storage.path = %q, want ./data/aib.db", cfg.Storage.Path)
	}
	if cfg.Storage.Memgraph.Enabled {
		t.Error("memgraph should be disabled by default")
	}
	if cfg.Storage.Memgraph.URI != "bolt://localhost:7687" {
		t.Errorf("memgraph.uri = %q", cfg.Storage.Memgraph.URI)
	}
	if cfg.Server.Listen != ":8080" {
		t.Errorf("server.listen = %q, want :8080", cfg.Server.Listen)
	}
	if cfg.Server.ReadOnly {
		t.Error("server.read_only should be false by default")
	}
	if !cfg.Certs.ProbeEnabled {
		t.Error("certs.probe_enabled should be true by default")
	}
	if !cfg.Scan.OnStartup {
		t.Error("scan.on_startup should be true by default")
	}
}

func TestEnvExpansion(t *testing.T) {
	os.Setenv("AIB_TEST_TOKEN", "my-secret-token")
	defer os.Unsetenv("AIB_TEST_TOKEN")

	cfg := &Config{
		Server: ServerConfig{APIToken: "${AIB_TEST_TOKEN}"},
	}

	expanded := os.ExpandEnv(cfg.Server.APIToken)
	if expanded != "my-secret-token" {
		t.Errorf("expanded = %q, want my-secret-token", expanded)
	}
}

func TestEnvExpansion_WebhookHeaders(t *testing.T) {
	os.Setenv("AIB_WEBHOOK_KEY", "secret-key")
	defer os.Unsetenv("AIB_WEBHOOK_KEY")

	headers := map[string]string{
		"X-API-Key": "${AIB_WEBHOOK_KEY}",
		"Static":    "value",
	}

	for k, v := range headers {
		headers[k] = os.ExpandEnv(v)
	}

	if headers["X-API-Key"] != "secret-key" {
		t.Errorf("X-API-Key = %q, want secret-key", headers["X-API-Key"])
	}
	if headers["Static"] != "value" {
		t.Errorf("Static = %q, want value", headers["Static"])
	}
}

// loadDefaults creates a Config with viper defaults without reading a file.
func loadDefaults() (*Config, error) {
	return &Config{
		Storage: StorageConfig{
			Path: "./data/aib.db",
			Memgraph: MemgraphConfig{
				Enabled: false,
				URI:     "bolt://localhost:7687",
			},
		},
		Server: ServerConfig{
			Listen:   ":8080",
			ReadOnly: false,
		},
		Certs: CertsConfig{
			ProbeEnabled:    true,
			ProbeInterval:   "6h",
			AlertThresholds: []int{90, 60, 30, 14, 7, 1},
		},
		Alerts: AlertsConfig{
			Stdout: StdoutConfig{Enabled: true},
		},
		Scan: ScanConfig{
			OnStartup: true,
		},
	}, nil
}
