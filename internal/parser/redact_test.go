package parser

import "testing"

func TestRedactCredentials(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "url password is redacted, host and database preserved",
			in:   "postgres://admin:SuperSecret123@10.0.0.5:5432/prod",
			want: "postgres://admin:REDACTED@10.0.0.5:5432/prod",
		},
		{
			name: "mysql url",
			in:   "mysql://root:hunter2@db.internal:3306/app",
			want: "mysql://root:REDACTED@db.internal:3306/app",
		},
		{
			name: "url without credentials is untouched",
			in:   "postgres://10.0.0.5:5432/prod",
			want: "postgres://10.0.0.5:5432/prod",
		},
		{
			name: "username without password is untouched",
			in:   "postgres://admin@10.0.0.5:5432/prod",
			want: "postgres://admin@10.0.0.5:5432/prod",
		},
		{
			name: "secret query parameter is redacted",
			in:   "postgres://10.0.0.5:5432/prod?sslmode=require&password=hunter2",
			want: "postgres://10.0.0.5:5432/prod?password=REDACTED&sslmode=require",
		},
		{
			name: "libpq keyword form",
			in:   "host=10.0.0.5 port=5432 user=admin password=hunter2 dbname=prod",
			want: "host=10.0.0.5 port=5432 user=admin password=REDACTED dbname=prod",
		},
		{
			name: "keyword form without secret is untouched",
			in:   "host=10.0.0.5 port=5432 dbname=prod",
			want: "host=10.0.0.5 port=5432 dbname=prod",
		},
		{
			name: "password with url-illegal characters still redacted",
			in:   "postgres://admin:pa ss|wo rd@10.0.0.5:5432/prod",
			want: "postgres://admin:REDACTED@10.0.0.5:5432/prod",
		},
		{
			name: "empty string",
			in:   "",
			want: "",
		},
		{
			name: "plain host:port is untouched",
			in:   "10.0.0.5:5432",
			want: "10.0.0.5:5432",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RedactCredentials(tt.in); got != tt.want {
				t.Errorf("RedactCredentials(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
