package terraform

import (
	"context"
	"testing"

	"github.com/matijazezelj/aib/internal/parser"
)

// Cloud resource tags and Kubernetes labels are operator-authored and are copied
// wholesale into node metadata, which reaches aib.db, JSON reports and the API —
// all published as CI artifacts by the GitHub Action. Secret-looking keys must
// be redacted; ordinary tags must survive.
func TestParse_SecretTagsAreRedacted(t *testing.T) {
	p := NewStateParser()
	result, err := p.Parse(context.Background(), "testdata/secret_tags.tfstate")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) == 0 {
		t.Fatal("expected at least one node")
	}

	sawRedacted := false
	sawOrdinary := false
	for _, n := range result.Nodes {
		for k, v := range n.Metadata {
			if v == "ATTR_SECRET" || v == "TAG_SECRET" || v == "LABEL_SECRET" {
				t.Errorf("node %s leaked a secret via metadata[%q] = %q", n.ID, k, v)
			}
			if k == "tag:api_key" {
				if v != parser.RedactedValue {
					t.Errorf("metadata[%q] = %q, want %q", k, v, parser.RedactedValue)
				}
				sawRedacted = true
			}
			if k == "tag:owner" && v == "platform" {
				sawOrdinary = true
			}
		}
	}
	if !sawRedacted {
		t.Errorf("fixture did not produce the expected %q metadata key", "tag:api_key")
	}
	if !sawOrdinary {
		t.Errorf("ordinary tag %q was dropped; redaction must not remove non-secret metadata", "tag:owner")
	}
}
