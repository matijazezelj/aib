package certs

import (
	"context"
	"log/slog"
	"net"

	"github.com/matijazezelj/aib/internal/graph"
	"github.com/matijazezelj/aib/pkg/models"
)

// DiscoverEndpoints finds TLS endpoints from the asset graph by looking at
// ingress, load balancer, and service nodes with associated IP addresses or hostnames.
func DiscoverEndpoints(ctx context.Context, store *graph.SQLiteStore, logger *slog.Logger) []string {
	var endpoints []string

	// portOrDefault returns the port from metadata, or "443" if not set.
	portOrDefault := func(meta map[string]string) string {
		if p, ok := meta["port"]; ok && p != "" {
			return p
		}
		if p, ok := meta["tls_port"]; ok && p != "" {
			return p
		}
		return "443"
	}

	// Look at ingress nodes for hostnames
	ingresses, _ := store.ListNodes(ctx, graph.NodeFilter{Type: string(models.AssetIngress)})
	for _, n := range ingresses {
		port := portOrDefault(n.Metadata)
		if host, ok := n.Metadata["host"]; ok && host != "" {
			endpoints = append(endpoints, net.JoinHostPort(host, port))
		}
		if host, ok := n.Metadata["hostname"]; ok && host != "" {
			endpoints = append(endpoints, net.JoinHostPort(host, port))
		}
	}

	// Look at load balancers
	lbs, _ := store.ListNodes(ctx, graph.NodeFilter{Type: string(models.AssetLoadBalancer)})
	for _, n := range lbs {
		port := portOrDefault(n.Metadata)
		if ip, ok := n.Metadata["ip_address"]; ok && ip != "" {
			endpoints = append(endpoints, net.JoinHostPort(ip, port))
		}
	}

	// Look at DNS records
	dnsRecords, _ := store.ListNodes(ctx, graph.NodeFilter{Type: string(models.AssetDNSRecord)})
	for _, n := range dnsRecords {
		port := portOrDefault(n.Metadata)
		if n.Name != "" {
			endpoints = append(endpoints, net.JoinHostPort(n.Name, port))
		}
	}

	logger.Info("discovered TLS endpoints from graph", "count", len(endpoints))

	// Deduplicate
	seen := make(map[string]bool)
	var unique []string
	for _, ep := range endpoints {
		if !seen[ep] {
			seen[ep] = true
			unique = append(unique, ep)
		}
	}

	return unique
}

// ProbeAll probes all discovered TLS endpoints and returns results.
func ProbeAll(ctx context.Context, tracker *Tracker, store *graph.SQLiteStore, logger *slog.Logger) []CertInfo {
	endpoints := DiscoverEndpoints(ctx, store, logger)
	var results []CertInfo

	for _, ep := range endpoints {
		ci, err := tracker.ProbeAndStore(ctx, ep)
		if err != nil {
			logger.Warn("failed to probe endpoint", "endpoint", ep, "error", err)
			continue
		}
		results = append(results, *ci)
	}

	logger.Info("TLS endpoint probing complete", "probed", len(endpoints), "found", len(results))
	return results
}
