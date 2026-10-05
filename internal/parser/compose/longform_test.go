package compose

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func parse(t *testing.T, path string) (map[string]map[string]string, []string, int) {
	t.Helper()
	res, err := NewComposeParser().Parse(context.Background(), path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	nodes := map[string]map[string]string{}
	for _, n := range res.Nodes {
		nodes[n.ID] = n.Metadata
	}
	return nodes, res.Warnings, len(res.Edges)
}

// `docker compose config` emits long-form ports and volumes; these must parse.
func TestParse_LongFormPortsAndVolumes(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "compose.yml", `services:
  web:
    image: nginx
    ports:
      - mode: ingress
        host_ip: 203.0.113.5
        target: 80
        published: "8080"
        protocol: tcp
      - target: 53
        published: "5353"
        protocol: udp
    volumes:
      - type: volume
        source: data
        target: /var/lib/data
        read_only: true
      - type: bind
        source: /srv/www
        target: /usr/share/nginx/html
        bind:
          create_host_path: true
volumes:
  data: {}
`)
	nodes, _, edges := parse(t, p)
	got := nodes["compose:container:web"]["ports"]
	if got != "203.0.113.5:8080:80,5353:53/udp" {
		t.Errorf("ports = %q", got)
	}
	// The named volume must still produce a mounts_volume edge; the bind mount must not.
	if edges < 2 { // default-network edge + the named-volume edge
		t.Errorf("edges = %d, want >= 2 (network + named volume)", edges)
	}
}

// A service with no networks key joins the implicit "default" network, and the
// edge must have a node to point at.
func TestParse_ImplicitDefaultNetwork(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "compose.yml", "services:\n  a:\n    image: x\n  b:\n    image: y\n    networks: [backend]\n")
	nodes, _, _ := parse(t, p)
	for _, want := range []string{"compose:network:default", "compose:network:backend"} {
		if _, ok := nodes[want]; !ok {
			t.Errorf("missing node %s (an edge would reference a node that does not exist)", want)
		}
	}
}

func TestParse_IncludeFollowed(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "svc/a/compose.yml", "services:\n  alpha:\n    image: a\n")
	write(t, dir, "svc/b/compose.yml", "services:\n  beta:\n    image: b\n    depends_on: [alpha]\n")
	p := write(t, dir, "compose.yml", "include:\n  - svc/a/compose.yml\n  - path: svc/b/compose.yml\n")
	nodes, warns, _ := parse(t, p)
	for _, want := range []string{"compose:container:alpha", "compose:container:beta"} {
		if _, ok := nodes[want]; !ok {
			t.Errorf("include not followed: %s missing (warnings: %v)", want, warns)
		}
	}
}

func TestParse_IncludeCannotEscapeRoot(t *testing.T) {
	outer := t.TempDir()
	root := filepath.Join(outer, "root")
	write(t, outer, "secret/compose.yml", "services:\n  leaked:\n    image: x\n")
	p := write(t, root, "compose.yml", "include:\n  - ../secret/compose.yml\n")
	nodes, warns, _ := parse(t, p)
	if _, ok := nodes["compose:container:leaked"]; ok {
		t.Fatal("include escaped the scanned directory")
	}
	if len(warns) == 0 || !strings.Contains(warns[0], "outside the scanned directory") {
		t.Errorf("expected an 'outside' warning, got %v", warns)
	}
}

func TestParse_IncludeCycleTerminates(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.yml", "include:\n  - b.yml\nservices:\n  a:\n    image: x\n")
	write(t, dir, "b.yml", "include:\n  - a.yml\nservices:\n  b:\n    image: y\n")
	p := write(t, dir, "compose.yml", "include:\n  - a.yml\n")
	nodes, _, _ := parse(t, p)
	if _, ok := nodes["compose:container:a"]; !ok {
		t.Error("service a missing")
	}
	if _, ok := nodes["compose:container:b"]; !ok {
		t.Error("service b missing")
	}
}

func TestParse_BrokenIncludeWarnsAndKeepsRest(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "bad.yml", "services: [this is: not valid\n")
	write(t, dir, "good.yml", "services:\n  ok:\n    image: x\n")
	p := write(t, dir, "compose.yml", "include:\n  - bad.yml\n  - good.yml\n  - missing.yml\n")
	nodes, warns, _ := parse(t, p)
	if _, ok := nodes["compose:container:ok"]; !ok {
		t.Error("one broken include must not hide the rest")
	}
	if len(warns) < 2 {
		t.Errorf("want warnings for the bad and the missing include, got %v", warns)
	}
}
