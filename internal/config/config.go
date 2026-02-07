package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds all AIB configuration loaded from file and environment.
type Config struct {
	Storage StorageConfig          `mapstructure:"storage"`
	Sources SourcesConfig          `mapstructure:"sources"`
	Certs   CertsConfig            `mapstructure:"certs"`
	Alerts  AlertsConfig           `mapstructure:"alerts"`
	Server  ServerConfig           `mapstructure:"server"`
	Scan    ScanConfig             `mapstructure:"scan"`
}

// StorageConfig configures the SQLite database and optional Memgraph connection.
type StorageConfig struct {
	Path     string         `mapstructure:"path"`
	Memgraph MemgraphConfig `mapstructure:"memgraph"`
}

// MemgraphConfig configures the optional Memgraph graph database.
type MemgraphConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	URI      string `mapstructure:"uri"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
}

// SourcesConfig lists all infrastructure sources to scan.
type SourcesConfig struct {
	Terraform  []TerraformSource  `mapstructure:"terraform"`
	Kubernetes []KubernetesSource `mapstructure:"kubernetes"`
	Ansible    []AnsibleSource    `mapstructure:"ansible"`
	Compose    []ComposeSource    `mapstructure:"compose"`
}

// ComposeSource configures a Docker Compose file or directory to scan.
type ComposeSource struct {
	Path string `mapstructure:"path"`
}

// TerraformSource configures a Terraform state file or directory to scan.
type TerraformSource struct {
	Path      string `mapstructure:"path"`
	StateFile string `mapstructure:"state_file"`
}

// KubernetesSource configures a Kubernetes manifest path, Helm chart, or live cluster.
type KubernetesSource struct {
	Path       string   `mapstructure:"path"`
	HelmChart  string   `mapstructure:"helm_chart"`
	ValuesFile string   `mapstructure:"values_file"`
	Kubeconfig string   `mapstructure:"kubeconfig"`
	Context    string   `mapstructure:"context"`
	Live       bool     `mapstructure:"live"`
	Namespaces []string `mapstructure:"namespaces"`
}

// AnsibleSource configures an Ansible inventory and optional playbook directory.
type AnsibleSource struct {
	Inventory string `mapstructure:"inventory"`
	Playbooks string `mapstructure:"playbooks"`
}

// CertsConfig configures TLS certificate probing and alert thresholds.
type CertsConfig struct {
	ProbeEnabled    bool   `mapstructure:"probe_enabled"`
	ProbeInterval   string `mapstructure:"probe_interval"`
	AlertThresholds []int  `mapstructure:"alert_thresholds"`
}

// AlertsConfig configures alert backends (webhook and stdout).
type AlertsConfig struct {
	Webhook WebhookConfig `mapstructure:"webhook"`
	Stdout  StdoutConfig  `mapstructure:"stdout"`
}

// WebhookConfig configures the webhook alert backend.
type WebhookConfig struct {
	Enabled bool              `mapstructure:"enabled"`
	URL     string            `mapstructure:"url"`
	Headers map[string]string `mapstructure:"headers"`
}

// StdoutConfig configures the stdout alert backend.
type StdoutConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

// ServerConfig configures the HTTP server, API auth, and CORS.
type ServerConfig struct {
	Listen     string `mapstructure:"listen"`
	ReadOnly   bool   `mapstructure:"read_only"`
	APIToken   string `mapstructure:"api_token"`
	CORSOrigin string `mapstructure:"cors_origin"`
}

// ScanConfig configures automatic scan scheduling.
type ScanConfig struct {
	Schedule  string `mapstructure:"schedule"`
	OnStartup bool   `mapstructure:"on_startup"`
}

// Load reads the configuration from file and environment variables.
func Load(cfgFile string) (*Config, error) {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		if err == nil {
			viper.AddConfigPath(filepath.Join(home, ".aib"))
		}
		viper.AddConfigPath(".")
		viper.SetConfigName("aib")
		viper.SetConfigType("yaml")
	}

	viper.SetEnvPrefix("AIB")
	viper.AutomaticEnv()

	// Defaults
	viper.SetDefault("storage.path", "./data/aib.db")
	viper.SetDefault("storage.memgraph.enabled", false)
	viper.SetDefault("storage.memgraph.uri", "bolt://localhost:7687")
	viper.SetDefault("server.listen", ":8080")
	viper.SetDefault("server.read_only", false)
	viper.SetDefault("certs.probe_enabled", true)
	viper.SetDefault("certs.probe_interval", "6h")
	viper.SetDefault("certs.alert_thresholds", []int{90, 60, 30, 14, 7, 1})
	viper.SetDefault("alerts.stdout.enabled", true)
	viper.SetDefault("scan.on_startup", true)

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config: %w", err)
		}
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}

	// Expand ${ENV_VAR} references in sensitive string fields.
	cfg.Storage.Memgraph.Password = os.ExpandEnv(cfg.Storage.Memgraph.Password)
	cfg.Storage.Memgraph.Username = os.ExpandEnv(cfg.Storage.Memgraph.Username)
	cfg.Alerts.Webhook.URL = os.ExpandEnv(cfg.Alerts.Webhook.URL)
	cfg.Server.APIToken = os.ExpandEnv(cfg.Server.APIToken)
	for k, v := range cfg.Alerts.Webhook.Headers {
		cfg.Alerts.Webhook.Headers[k] = os.ExpandEnv(v)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}

	return &cfg, nil
}

// Validate checks the configuration for common errors and returns a joined
// multi-error if any problems are found.
func (c *Config) Validate() error {
	var errs []error

	if c.Storage.Path == "" {
		errs = append(errs, fmt.Errorf("storage.path must not be empty"))
	}

	if c.Storage.Memgraph.Enabled {
		uri := c.Storage.Memgraph.URI
		if !strings.HasPrefix(uri, "bolt://") && !strings.HasPrefix(uri, "neo4j://") {
			errs = append(errs, fmt.Errorf("storage.memgraph.uri must start with bolt:// or neo4j://, got %q", uri))
		}
	}

	if c.Certs.ProbeEnabled && c.Certs.ProbeInterval != "" {
		d, err := time.ParseDuration(c.Certs.ProbeInterval)
		if err != nil {
			errs = append(errs, fmt.Errorf("certs.probe_interval %q is not a valid duration: %w", c.Certs.ProbeInterval, err))
		} else if d < time.Minute {
			errs = append(errs, fmt.Errorf("certs.probe_interval must be >= 1m, got %s", d))
		}
	}

	for i, v := range c.Certs.AlertThresholds {
		if v <= 0 {
			errs = append(errs, fmt.Errorf("certs.alert_thresholds[%d] must be positive, got %d", i, v))
			break
		}
		if i > 0 && v >= c.Certs.AlertThresholds[i-1] {
			errs = append(errs, fmt.Errorf("certs.alert_thresholds must be sorted descending, but [%d]=%d >= [%d]=%d",
				i-1, c.Certs.AlertThresholds[i-1], i, v))
			break
		}
	}

	if c.Alerts.Webhook.Enabled && c.Alerts.Webhook.URL != "" {
		u, err := url.Parse(c.Alerts.Webhook.URL)
		if err != nil {
			errs = append(errs, fmt.Errorf("alerts.webhook.url is not a valid URL: %w", err))
		} else if u.Scheme != "http" && u.Scheme != "https" {
			errs = append(errs, fmt.Errorf("alerts.webhook.url must use http or https scheme, got %q", u.Scheme))
		}
	}

	if c.Server.Listen != "" {
		_, _, err := net.SplitHostPort(c.Server.Listen)
		if err != nil {
			errs = append(errs, fmt.Errorf("server.listen %q is not a valid host:port: %w", c.Server.Listen, err))
		}
	}

	if c.Server.APIToken != "" && len(c.Server.APIToken) < 8 {
		errs = append(errs, fmt.Errorf("server.api_token is too short (%d chars), use at least 8 characters", len(c.Server.APIToken)))
	}

	if c.Scan.Schedule != "" {
		d, err := time.ParseDuration(c.Scan.Schedule)
		if err != nil {
			// Skip cron expressions (they contain spaces)
			if !strings.Contains(c.Scan.Schedule, " ") {
				errs = append(errs, fmt.Errorf("scan.schedule %q is not a valid duration: %w", c.Scan.Schedule, err))
			}
		} else if d < time.Minute {
			errs = append(errs, fmt.Errorf("scan.schedule must be >= 1m, got %s", d))
		}
	}

	return errors.Join(errs...)
}
