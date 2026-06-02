package graph

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/matijazezelj/aib/pkg/models"
)

func correlationTestNode(id, name string, typ models.AssetType, source string, metadata map[string]string) models.Node {
	now := time.Now().Truncate(time.Second)
	if metadata == nil {
		metadata = map[string]string{}
	}
	return models.Node{
		ID:        id,
		Name:      name,
		Type:      typ,
		Source:    source,
		Provider:  "test",
		Metadata:  metadata,
		LastSeen:  now,
		FirstSeen: now,
	}
}

func TestCorrelateIdentitiesLinksSameRealAssetAcrossSources(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	buildTestGraph(t, store, []models.Node{
		correlationTestNode("tf:db:bogus-db", "bogus-db", models.AssetDatabase, "terraform", map[string]string{"address": "aws_rds_cluster.bogus_db"}),
		correlationTestNode("compose:container:bogus-db", "bogus-db", models.AssetContainer, "compose", map[string]string{"image": "postgres:16"}),
		correlationTestNode("ansible:database:database@bogus-db", "database@bogus-db", models.AssetDatabase, "ansible", nil),
		correlationTestNode("k8s:pod:bogus/bogus-api", "bogus-api", models.AssetPod, "kubernetes", map[string]string{"app": "bogus-api"}),
		correlationTestNode("compose:container:bogus-api", "bogus-api", models.AssetContainer, "compose", nil),
		correlationTestNode("tf:vm:unrelated", "jumpbox", models.AssetVM, "terraform", nil),
	}, nil)

	summary, err := CorrelateIdentities(ctx, store)
	if err != nil {
		t.Fatalf("CorrelateIdentities returned error: %v", err)
	}

	if summary.Groups != 2 {
		t.Fatalf("Groups = %d, want 2", summary.Groups)
	}
	if summary.EdgesAdded != 4 {
		t.Fatalf("EdgesAdded = %d, want 4", summary.EdgesAdded)
	}

	edges, err := store.ListEdges(ctx, EdgeFilter{Type: string(models.EdgeCorrelatesWith)})
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 4 {
		t.Fatalf("correlation edges = %d, want 4", len(edges))
	}

	byID := map[string]models.Edge{}
	for _, edge := range edges {
		byID[edge.ID] = edge
		if edge.Metadata["correlation_key"] == "" {
			t.Fatalf("edge %s missing correlation_key metadata", edge.ID)
		}
		if edge.Metadata["confidence"] == "" {
			t.Fatalf("edge %s missing confidence metadata", edge.ID)
		}
	}

	wantDB := GenerateEdgeID("ansible:database:database@bogus-db", "compose:container:bogus-db", models.EdgeCorrelatesWith)
	if _, ok := byID[wantDB]; !ok {
		t.Fatalf("missing DB correlation edge %s", wantDB)
	}
	wantTFDB := GenerateEdgeID("compose:container:bogus-db", "tf:db:bogus-db", models.EdgeCorrelatesWith)
	if _, ok := byID[wantTFDB]; !ok {
		t.Fatalf("missing Terraform DB correlation edge %s", wantTFDB)
	}
	wantAPI := GenerateEdgeID("compose:container:bogus-api", "k8s:pod:bogus/bogus-api", models.EdgeCorrelatesWith)
	if _, ok := byID[wantAPI]; !ok {
		t.Fatalf("missing API correlation edge %s", wantAPI)
	}
}

func TestCorrelateIdentitiesIsIdempotent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	buildTestGraph(t, store, []models.Node{
		correlationTestNode("compose:container:api", "api", models.AssetContainer, "compose", nil),
		correlationTestNode("k8s:service:default/api", "api", models.AssetService, "kubernetes", nil),
	}, nil)

	first, err := CorrelateIdentities(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CorrelateIdentities(ctx, store)
	if err != nil {
		t.Fatal(err)
	}

	if first.EdgesAdded != 1 {
		t.Fatalf("first EdgesAdded = %d, want 1", first.EdgesAdded)
	}
	if second.EdgesAdded != 0 {
		t.Fatalf("second EdgesAdded = %d, want 0", second.EdgesAdded)
	}
	edges, err := store.ListEdges(ctx, EdgeFilter{Type: string(models.EdgeCorrelatesWith)})
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 1 {
		t.Fatalf("stored correlation edges = %d, want 1", len(edges))
	}
}

func TestListCorrelatedAssetGroupsBuildsReportComponents(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	buildTestGraph(t, store, []models.Node{
		correlationTestNode("compose:container:bogus-api", "bogus-api", models.AssetContainer, "compose", nil),
		correlationTestNode("k8s:pod:bogus/bogus-api", "bogus-api", models.AssetPod, "kubernetes", nil),
		correlationTestNode("k8s:service:bogus/bogus-api", "bogus-api", models.AssetService, "kubernetes", nil),
	}, nil)
	if _, err := CorrelateIdentities(ctx, store); err != nil {
		t.Fatal(err)
	}

	groups, err := ListCorrelatedAssetGroups(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(groups))
	}
	if groups[0].Key != "bogus-api" {
		t.Fatalf("group key = %q, want bogus-api", groups[0].Key)
	}
	if got := strings.Join(groups[0].Sources, ","); got != "compose,kubernetes" {
		t.Fatalf("sources = %q, want compose,kubernetes", got)
	}
	if len(groups[0].Nodes) != 3 {
		t.Fatalf("nodes = %d, want 3", len(groups[0].Nodes))
	}
}

func TestCorrelateIdentitiesDoesNotCorrelateFQDNTopLevelDomains(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	buildTestGraph(t, store, []models.Node{
		correlationTestNode("k8s:service:default/postgres", "postgres", models.AssetService, "kubernetes", map[string]string{"hostname": "postgres.svc.cluster.local"}),
		correlationTestNode("compose:container:redis", "redis", models.AssetContainer, "compose", map[string]string{"hostname": "redis.svc.cluster.local"}),
	}, nil)

	_, err := CorrelateIdentities(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	edges, err := store.ListEdges(ctx, EdgeFilter{Type: string(models.EdgeCorrelatesWith)})
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 0 {
		t.Fatalf("correlation edges = %d, want 0", len(edges))
	}
}
