package ansible

import (
	"testing"
)

func TestParseINIInventory(t *testing.T) {
	hosts, warnings, err := parseINIInventory("testdata/inventory.ini")
	if err != nil {
		t.Fatal(err)
	}

	if len(warnings) > 0 {
		t.Logf("warnings: %v", warnings)
	}

	if len(hosts) != 3 {
		t.Errorf("hosts = %d, want 3 (web1, web2, db1)", len(hosts))
	}

	hostMap := make(map[string]hostEntry)
	for _, h := range hosts {
		hostMap[h.hostname] = h
	}

	// Check web1
	web1, ok := hostMap["web1"]
	if !ok {
		t.Fatal("missing web1")
	}
	if web1.vars["ansible_host"] != "192.168.1.10" {
		t.Errorf("web1 ansible_host = %q", web1.vars["ansible_host"])
	}
	if web1.vars["ansible_port"] != "22" {
		t.Errorf("web1 ansible_port = %q", web1.vars["ansible_port"])
	}

	// Check group vars applied
	if web1.vars["http_port"] != "80" {
		t.Errorf("web1 http_port = %q, want 80 (from [webservers:vars])", web1.vars["http_port"])
	}
}

func TestParseINIInventory_Children(t *testing.T) {
	hosts, _, err := parseINIInventory("testdata/inventory.ini")
	if err != nil {
		t.Fatal(err)
	}

	// [production:children] includes webservers and dbservers
	// So web1, web2 should have "production" in their groups
	for _, h := range hosts {
		if h.hostname == "web1" || h.hostname == "web2" {
			hasProduction := false
			for _, g := range h.groups {
				if g == "production" {
					hasProduction = true
					break
				}
			}
			if !hasProduction {
				t.Errorf("%s should be in production group (via children), groups=%v", h.hostname, h.groups)
			}
		}
	}
}

func TestParseYAMLInventory(t *testing.T) {
	hosts, _, err := parseYAMLInventory("testdata/inventory.yml")
	if err != nil {
		t.Fatal(err)
	}

	if len(hosts) != 3 {
		t.Errorf("hosts = %d, want 3", len(hosts))
	}

	hostMap := make(map[string]hostEntry)
	for _, h := range hosts {
		hostMap[h.hostname] = h
	}

	// db1 should have ansible_user=admin (overrides all.vars ansible_user=deploy)
	db1, ok := hostMap["db1"]
	if !ok {
		t.Fatal("missing db1")
	}
	if db1.vars["ansible_user"] != "admin" {
		t.Errorf("db1 ansible_user = %q, want admin", db1.vars["ansible_user"])
	}

	// web1 should inherit ansible_user=deploy from all.vars
	web1, ok := hostMap["web1"]
	if !ok {
		t.Fatal("missing web1")
	}
	if web1.vars["ansible_user"] != "deploy" {
		t.Errorf("web1 ansible_user = %q, want deploy", web1.vars["ansible_user"])
	}
}

func TestDeduplicateHosts(t *testing.T) {
	hosts := []hostEntry{
		{hostname: "web1", groups: []string{"web"}, vars: map[string]string{"a": "1"}},
		{hostname: "web1", groups: []string{"prod"}, vars: map[string]string{"b": "2"}},
	}

	result := deduplicateHosts(hosts)

	if len(result) != 1 {
		t.Errorf("dedup result = %d, want 1", len(result))
	}

	h := result["web1"]
	if h.vars["a"] != "1" || h.vars["b"] != "2" {
		t.Errorf("vars not merged: %v", h.vars)
	}

	groupSet := make(map[string]bool)
	for _, g := range h.groups {
		groupSet[g] = true
	}
	if !groupSet["web"] || !groupSet["prod"] {
		t.Errorf("groups not merged: %v", h.groups)
	}
}

func TestParseInventoryFile_AutoDetect(t *testing.T) {
	// .yml extension → YAML parser
	hosts, _, err := parseInventoryFile("testdata/inventory.yml")
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 3 {
		t.Errorf("YAML inventory hosts = %d, want 3", len(hosts))
	}

	// .ini extension → INI parser
	hosts, _, err = parseInventoryFile("testdata/inventory.ini")
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 3 {
		t.Errorf("INI inventory hosts = %d, want 3", len(hosts))
	}
}
