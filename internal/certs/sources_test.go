package certs

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/matijazezelj/aib/pkg/models"
	_ "modernc.org/sqlite"
)

func TestDiscoverEndpoints_Ingress(t *testing.T) {
	store := newTestStore(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()
	now := time.Now()

	_ = store.UpsertNode(ctx, models.Node{
		ID: "k8s:ingress:app", Name: "app-ingress", Type: models.AssetIngress,
		Source: "kubernetes", Metadata: map[string]string{"host": "app.example.com"},
		LastSeen: now, FirstSeen: now,
	})

	endpoints := DiscoverEndpoints(ctx, store, logger)
	if len(endpoints) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(endpoints))
	}
	if endpoints[0] != "app.example.com:443" {
		t.Errorf("endpoint = %q, want app.example.com:443", endpoints[0])
	}
}

func TestDiscoverEndpoints_IngressHostname(t *testing.T) {
	store := newTestStore(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()
	now := time.Now()

	_ = store.UpsertNode(ctx, models.Node{
		ID: "k8s:ingress:api", Name: "api-ingress", Type: models.AssetIngress,
		Source: "kubernetes", Metadata: map[string]string{"hostname": "api.example.com"},
		LastSeen: now, FirstSeen: now,
	})

	endpoints := DiscoverEndpoints(ctx, store, logger)
	if len(endpoints) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(endpoints))
	}
	if endpoints[0] != "api.example.com:443" {
		t.Errorf("endpoint = %q, want api.example.com:443", endpoints[0])
	}
}

func TestDiscoverEndpoints_LoadBalancer(t *testing.T) {
	store := newTestStore(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()
	now := time.Now()

	_ = store.UpsertNode(ctx, models.Node{
		ID: "tf:lb:main", Name: "main-lb", Type: models.AssetLoadBalancer,
		Source: "terraform", Metadata: map[string]string{"ip_address": "10.0.0.1"},
		LastSeen: now, FirstSeen: now,
	})

	endpoints := DiscoverEndpoints(ctx, store, logger)
	if len(endpoints) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(endpoints))
	}
	if endpoints[0] != "10.0.0.1:443" {
		t.Errorf("endpoint = %q, want 10.0.0.1:443", endpoints[0])
	}
}

func TestDiscoverEndpoints_DNS(t *testing.T) {
	store := newTestStore(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()
	now := time.Now()

	_ = store.UpsertNode(ctx, models.Node{
		ID: "tf:dns:www", Name: "www.example.com", Type: models.AssetDNSRecord,
		Source: "terraform", Metadata: map[string]string{},
		LastSeen: now, FirstSeen: now,
	})

	endpoints := DiscoverEndpoints(ctx, store, logger)
	if len(endpoints) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(endpoints))
	}
	if endpoints[0] != "www.example.com:443" {
		t.Errorf("endpoint = %q, want www.example.com:443", endpoints[0])
	}
}

func TestDiscoverEndpoints_Dedup(t *testing.T) {
	store := newTestStore(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()
	now := time.Now()

	// Two ingress nodes pointing to the same host
	_ = store.UpsertNode(ctx, models.Node{
		ID: "k8s:ingress:a", Name: "ingress-a", Type: models.AssetIngress,
		Source: "kubernetes", Metadata: map[string]string{"host": "dup.example.com"},
		LastSeen: now, FirstSeen: now,
	})
	_ = store.UpsertNode(ctx, models.Node{
		ID: "k8s:ingress:b", Name: "ingress-b", Type: models.AssetIngress,
		Source: "kubernetes", Metadata: map[string]string{"host": "dup.example.com"},
		LastSeen: now, FirstSeen: now,
	})

	endpoints := DiscoverEndpoints(ctx, store, logger)
	if len(endpoints) != 1 {
		t.Errorf("expected 1 deduplicated endpoint, got %d", len(endpoints))
	}
}

func TestDiscoverEndpoints_Mixed(t *testing.T) {
	store := newTestStore(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()
	now := time.Now()

	_ = store.UpsertNode(ctx, models.Node{
		ID: "k8s:ingress:app", Name: "app-ingress", Type: models.AssetIngress,
		Source: "kubernetes", Metadata: map[string]string{"host": "app.example.com"},
		LastSeen: now, FirstSeen: now,
	})
	_ = store.UpsertNode(ctx, models.Node{
		ID: "tf:lb:main", Name: "main-lb", Type: models.AssetLoadBalancer,
		Source: "terraform", Metadata: map[string]string{"ip_address": "10.0.0.1"},
		LastSeen: now, FirstSeen: now,
	})
	_ = store.UpsertNode(ctx, models.Node{
		ID: "tf:dns:www", Name: "www.example.com", Type: models.AssetDNSRecord,
		Source: "terraform", Metadata: map[string]string{},
		LastSeen: now, FirstSeen: now,
	})
	// Also add a non-relevant node that should NOT appear
	_ = store.UpsertNode(ctx, models.Node{
		ID: "tf:vm:web1", Name: "web1", Type: models.AssetVM,
		Source: "terraform", Metadata: map[string]string{},
		LastSeen: now, FirstSeen: now,
	})

	endpoints := DiscoverEndpoints(ctx, store, logger)
	if len(endpoints) != 3 {
		t.Errorf("expected 3 endpoints, got %d", len(endpoints))
	}
}

func TestDiscoverEndpoints_Empty(t *testing.T) {
	store := newTestStore(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	endpoints := DiscoverEndpoints(context.Background(), store, logger)
	if len(endpoints) != 0 {
		t.Errorf("expected 0 endpoints, got %d", len(endpoints))
	}
}
