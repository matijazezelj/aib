package parser

import (
	"net/url"
	"regexp"
	"strings"
)

// RedactedValue replaces a secret in parser output.
//
// It is deliberately alphanumeric: url.String and url.Values.Encode
// percent-encode punctuation, which would turn a "***" placeholder into
// "%2A%2A%2A" inside a redacted DSN.
const RedactedValue = "REDACTED"

// userinfoPattern matches the "scheme://user:password@" prefix of a DSN. It is a
// fallback for values url.Parse rejects — a password containing characters that
// are illegal in a URL still has to be redacted.
var userinfoPattern = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9+.\-]*://)([^/@]*):([^/@]*)@`)

// secretKeyParts are matched as substrings, so "pass" covers ansible_password,
// ansible_become_pass, ansible_ssh_pass, vault_pass, MasterUserPassword and
// masterPassword in one entry. This over-matches on names like "passenger_port";
// redacting a harmless value is a far cheaper mistake than persisting a live
// credential.
var secretKeyParts = []string{"pass", "pwd", "secret", "token", "credential", "private_key", "apikey", "api_key"}

// IsSecretKey reports whether a metadata key name suggests its value is a
// credential. Used to redact operator-authored key/value pairs — inventory
// vars, cloud resource tags, Kubernetes labels — that parsers copy wholesale.
func IsSecretKey(key string) bool {
	lowered := strings.ToLower(key)
	for _, part := range secretKeyParts {
		if strings.Contains(lowered, part) {
			return true
		}
	}
	return false
}

// RedactCredentials strips passwords from an operator-supplied string.
//
// Parser output is persisted to the graph, which reaches aib.db, JSON reports
// and GET /api/v1/graph. The GitHub Action publishes the first two as CI
// artifacts, so an unredacted DSN becomes world-readable on a public
// repository. Host, port and database name are preserved; only the secret is
// replaced.
func RedactCredentials(value string) string {
	if strings.TrimSpace(value) == "" {
		return value
	}

	if u, err := url.Parse(value); err == nil && u.Scheme != "" {
		return redactURL(value, u)
	}

	if userinfoPattern.MatchString(value) {
		return userinfoPattern.ReplaceAllString(value, "${1}${2}:"+RedactedValue+"@")
	}

	return redactKeywordDSN(value)
}

// RedactMetadataValue redacts a key/value pair copied verbatim from
// operator-authored input, by key name first and then by value shape.
func RedactMetadataValue(key, value string) string {
	if IsSecretKey(key) {
		return RedactedValue
	}
	return RedactCredentials(value)
}

// redactURL replaces the userinfo password and any secret-bearing query
// parameter. It returns orig untouched when there is nothing to redact, so
// well-formed values are not reserialized into a different-looking string.
func redactURL(orig string, u *url.URL) string {
	changed := false

	if u.User != nil {
		if _, hasPassword := u.User.Password(); hasPassword {
			u.User = url.UserPassword(u.User.Username(), RedactedValue)
			changed = true
		}
	}

	query := u.Query()
	for key := range query {
		if IsSecretKey(key) {
			query.Set(key, RedactedValue)
			changed = true
		}
	}

	if !changed {
		return orig
	}
	u.RawQuery = query.Encode()
	return u.String()
}

// redactKeywordDSN handles the libpq-style "host=db password=hunter2" form,
// which Ansible inventories use as often as URL-shaped DSNs.
func redactKeywordDSN(value string) string {
	fields := strings.Fields(value)
	changed := false
	for i, field := range fields {
		key, _, ok := strings.Cut(field, "=")
		if ok && IsSecretKey(key) {
			fields[i] = key + "=" + RedactedValue
			changed = true
		}
	}
	if !changed {
		return value
	}
	return strings.Join(fields, " ")
}
