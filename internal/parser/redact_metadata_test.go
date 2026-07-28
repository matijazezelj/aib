package parser

import "testing"

func TestRedactMetadataValue(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{"secret-looking tag key is redacted", "api_key", "AKIAIOSFODNN7EXAMPLE", RedactedValue},
		{"secret_token key", "secret_token", "abc123", RedactedValue},
		{"kubernetes label naming", "api_token", "xyz", RedactedValue},
		{"cloud tag password key", "MasterUserPassword", "hunter2", RedactedValue},
		{"ordinary tag is preserved", "owner", "platform-team", "platform-team"},
		{"ordinary tag with url value is preserved", "docs", "https://wiki/runbook", "https://wiki/runbook"},
		{"dsn value under a harmless key is still redacted by shape", "notes", "postgres://u:p@h:5432/d", "postgres://u:" + RedactedValue + "@h:5432/d"},
		{"empty value", "owner", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RedactMetadataValue(tt.key, tt.value); got != tt.want {
				t.Errorf("RedactMetadataValue(%q, %q) = %q, want %q", tt.key, tt.value, got, tt.want)
			}
		})
	}
}

func TestIsSecretKey(t *testing.T) {
	secret := []string{
		"password", "Password", "ansible_become_pass", "ansible_ssh_pass",
		"vault_password", "db_pwd", "api_key", "apikey", "secret_token",
		"AWS_SECRET_ACCESS_KEY", "credentials", "private_key", "MasterUserPassword",
	}
	for _, k := range secret {
		if !IsSecretKey(k) {
			t.Errorf("IsSecretKey(%q) = false, want true", k)
		}
	}

	notSecret := []string{"owner", "env", "region", "ansible_host", "db_host", "port", "image"}
	for _, k := range notSecret {
		if IsSecretKey(k) {
			t.Errorf("IsSecretKey(%q) = true, want false", k)
		}
	}
}
