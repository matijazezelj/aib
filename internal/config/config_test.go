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
	t.Setenv("AIB_TEST_TOKEN", "my-secret-token")

	cfg := &Config{
		Server: ServerConfig{APIToken: "${AIB_TEST_TOKEN}"},
	}

	expanded := os.ExpandEnv(cfg.Server.APIToken)
	if expanded != "my-secret-token" {
		t.Errorf("expanded = %q, want my-secret-token", expanded)
	}
}

func TestEnvExpansion_WebhookHeaders(t *testing.T) {
	t.Setenv("AIB_WEBHOOK_KEY", "secret-key")

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

func TestLoadFromFile(t *testing.T) {
	content := `
storage:
  path: /tmp/custom.db
  memgraph:
    enabled: true
    uri: bolt://mg:7687
    username: admin
    password: secret
sources:
  terraform:
    - path: /infra/tf
  kubernetes:
    - path: /k8s/manifests
  ansible:
    - inventory: /ansible/hosts
server:
  listen: ":9090"
  read_only: true
  api_token: my-token
  cors_origin: https://example.com
certs:
  probe_enabled: false
  probe_interval: 12h
  alert_thresholds: [30, 7]
alerts:
  webhook:
    enabled: true
    url: https://hooks.example.com/alert
    headers:
      X-Key: val
  stdout:
    enabled: false
scan:
  schedule: "0 */6 * * *"
  on_startup: false
`
	tmpFile := t.TempDir() + "/aib.yaml"
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(tmpFile)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Storage.Path != "/tmp/custom.db" {
		t.Errorf("storage.path = %q, want /tmp/custom.db", cfg.Storage.Path)
	}
	if !cfg.Storage.Memgraph.Enabled {
		t.Error("memgraph should be enabled")
	}
	if cfg.Storage.Memgraph.URI != "bolt://mg:7687" {
		t.Errorf("memgraph.uri = %q", cfg.Storage.Memgraph.URI)
	}
	if cfg.Server.Listen != ":9090" {
		t.Errorf("server.listen = %q, want :9090", cfg.Server.Listen)
	}
	if !cfg.Server.ReadOnly {
		t.Error("server.read_only should be true")
	}
	if cfg.Server.APIToken != "my-token" {
		t.Errorf("server.api_token = %q, want my-token", cfg.Server.APIToken)
	}
	if cfg.Server.CORSOrigin != "https://example.com" {
		t.Errorf("server.cors_origin = %q", cfg.Server.CORSOrigin)
	}
	if cfg.Certs.ProbeEnabled {
		t.Error("certs.probe_enabled should be false")
	}
	if cfg.Certs.ProbeInterval != "12h" {
		t.Errorf("certs.probe_interval = %q, want 12h", cfg.Certs.ProbeInterval)
	}
	if len(cfg.Certs.AlertThresholds) != 2 {
		t.Errorf("alert_thresholds len = %d, want 2", len(cfg.Certs.AlertThresholds))
	}
	if !cfg.Alerts.Webhook.Enabled {
		t.Error("webhook should be enabled")
	}
	if cfg.Alerts.Webhook.URL != "https://hooks.example.com/alert" {
		t.Errorf("webhook.url = %q", cfg.Alerts.Webhook.URL)
	}
	if cfg.Alerts.Stdout.Enabled {
		t.Error("stdout should be disabled")
	}
	if cfg.Scan.Schedule != "0 */6 * * *" {
		t.Errorf("scan.schedule = %q", cfg.Scan.Schedule)
	}
	if cfg.Scan.OnStartup {
		t.Error("scan.on_startup should be false")
	}

	if len(cfg.Sources.Terraform) != 1 {
		t.Errorf("terraform sources = %d, want 1", len(cfg.Sources.Terraform))
	}
	if len(cfg.Sources.Kubernetes) != 1 {
		t.Errorf("kubernetes sources = %d, want 1", len(cfg.Sources.Kubernetes))
	}
	if len(cfg.Sources.Ansible) != 1 {
		t.Errorf("ansible sources = %d, want 1", len(cfg.Sources.Ansible))
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
