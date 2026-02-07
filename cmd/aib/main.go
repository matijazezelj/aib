package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/matijazezelj/aib/internal/alert"
	"github.com/matijazezelj/aib/internal/certs"
	"github.com/matijazezelj/aib/internal/config"
	"github.com/matijazezelj/aib/internal/graph"
	"github.com/matijazezelj/aib/internal/scanner"
	"github.com/matijazezelj/aib/internal/server"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/spf13/cobra"
)

var (
	version   = "dev"
	cfgFile   string
	dbPath    string
	logFormat string
	logLevel  string
	logger    *slog.Logger
)

func main() {
	logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	root := &cobra.Command{
		Use:   "aib",
		Short: "AIB — Assets in a Box",
		Long:  "Infrastructure asset discovery, dependency mapping, and blast radius analysis.",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			level, err := parseLogLevel(logLevel)
			if err != nil {
				return err
			}
			opts := &slog.HandlerOptions{Level: level}
			switch logFormat {
			case "json":
				logger = slog.New(slog.NewJSONHandler(os.Stderr, opts))
			case "text":
				logger = slog.New(slog.NewTextHandler(os.Stderr, opts))
			default:
				return fmt.Errorf("invalid --log-format %q (use: text, json)", logFormat)
			}
			return nil
		},
	}

	root.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ./aib.yaml)")
	root.PersistentFlags().StringVar(&dbPath, "db", "", "database path (overrides config)")
	root.PersistentFlags().StringVar(&logFormat, "log-format", "text", "log output format (text, json)")
	root.PersistentFlags().StringVar(&logLevel, "log-level", "info", "log level (debug, info, warn, error)")

	root.AddCommand(
		scanCmd(),
		graphCmd(),
		impactCmd(),
		certsCmd(),
		dbCmd(),
		serveCmd(),
		versionCmd(),
		completionCmd(),
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func openStore() (*graph.SQLiteStore, *config.Config) {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		logger.Error("loading config", "error", err)
		os.Exit(1)
	}

	path := cfg.Storage.Path
	if dbPath != "" {
		path = dbPath
	}

	store, err := graph.NewSQLiteStore(path)
	if err != nil {
		logger.Error("opening database", "error", err)
		os.Exit(1)
	}

	if err := store.Init(context.Background()); err != nil {
		logger.Error("initializing database", "error", err)
		os.Exit(1)
	}

	return store, cfg
}

// openStoreAndEngine returns the SQLite store and a GraphEngine.
// If Memgraph is configured and reachable, it returns a MemgraphEngine;
// otherwise it falls back to LocalEngine (in-memory BFS).
func openStoreAndEngine() (*graph.SQLiteStore, graph.GraphEngine, *config.Config) {
	store, cfg := openStore()
	localEngine := graph.NewLocalEngine(store)
	var engine graph.GraphEngine = localEngine

	if cfg.Storage.Memgraph.Enabled {
		mgEngine, err := graph.NewMemgraphEngine(
			cfg.Storage.Memgraph.URI,
			cfg.Storage.Memgraph.Username,
			cfg.Storage.Memgraph.Password,
			localEngine,
			logger,
		)
		if err != nil {
			logger.Warn("memgraph unavailable, using local graph engine", "error", err)
		} else {
			engine = mgEngine
			logger.Info("memgraph connected", "uri", cfg.Storage.Memgraph.URI)
		}
	}

	return store, engine, cfg
}

// --- scan ---

func scanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Discover assets from infrastructure sources",
	}

	cmd.AddCommand(scanTerraformCmd())
	cmd.AddCommand(scanAnsibleCmd())
	cmd.AddCommand(scanK8sCmd())
	cmd.AddCommand(scanComposeCmd())
	return cmd
}

func scanTerraformCmd() *cobra.Command {
	var remote bool
	var workspace string

	cmd := &cobra.Command{
		Use:   "terraform <path> [path...]",
		Short: "Scan Terraform state files, directories, or remote backends",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, cfg := openStore()
			defer store.Close() //nolint:errcheck // best-effort cleanup

			fmt.Printf("Scanning Terraform state across %d path(s)...\n", len(args))
			sc := scanner.New(store, cfg, logger)
			r := sc.RunSync(cmd.Context(), scanner.ScanRequest{
				Source:    "terraform",
				Paths:     args,
				Remote:    remote,
				Workspace: workspace,
			})
			printScanResult(r)
			if r.Error != nil {
				return r.Error
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&remote, "remote", false, "pull state from remote backend via 'terraform state pull'")
	cmd.Flags().StringVar(&workspace, "workspace", "", "terraform workspace to pull (use '*' for all workspaces)")
	return cmd
}

func scanAnsibleCmd() *cobra.Command {
	var playbooks string

	cmd := &cobra.Command{
		Use:   "ansible <inventory-path> [path...]",
		Short: "Scan Ansible inventory and playbooks for infrastructure assets",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, cfg := openStore()
			defer store.Close() //nolint:errcheck // best-effort cleanup

			fmt.Printf("Scanning Ansible inventory across %d path(s)...\n", len(args))
			sc := scanner.New(store, cfg, logger)
			r := sc.RunSync(cmd.Context(), scanner.ScanRequest{
				Source:    "ansible",
				Paths:     args,
				Playbooks: playbooks,
			})
			printScanResult(r)
			if r.Error != nil {
				return r.Error
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&playbooks, "playbooks", "", "directory containing Ansible playbooks to analyze")
	return cmd
}

func scanK8sCmd() *cobra.Command {
	var valuesFile string
	var helm bool
	var live bool
	var kubeconfig string
	var kubeCtx string
	var namespaces []string

	cmd := &cobra.Command{
		Use:   "kubernetes <path> [path...]",
		Short: "Scan Kubernetes manifests, Helm charts, or live clusters",
		Aliases: []string{"k8s"},
		Args:  cobra.MinimumNArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, cfg := openStore()
			defer store.Close() //nolint:errcheck // best-effort cleanup

			sc := scanner.New(store, cfg, logger)

			if live {
				fmt.Println("Scanning live Kubernetes cluster...")
				r := sc.RunSync(cmd.Context(), scanner.ScanRequest{
					Source:     "kubernetes-live",
					Kubeconfig: kubeconfig,
					Context:    kubeCtx,
					Namespaces: namespaces,
				})
				printScanResult(r)
				if r.Error != nil {
					return r.Error
				}
				return nil
			}

			if len(args) == 0 {
				return fmt.Errorf("at least one path is required (or use --live for cluster scanning)")
			}

			fmt.Printf("Scanning Kubernetes manifests across %d path(s)...\n", len(args))
			r := sc.RunSync(cmd.Context(), scanner.ScanRequest{
				Source:     "kubernetes",
				Paths:      args,
				Helm:       helm,
				ValuesFile: valuesFile,
			})
			printScanResult(r)
			if r.Error != nil {
				return r.Error
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&helm, "helm", false, "render Helm chart via 'helm template' before parsing")
	cmd.Flags().StringVar(&valuesFile, "values", "", "Helm values file (used with --helm)")
	cmd.Flags().BoolVar(&live, "live", false, "scan a live Kubernetes cluster via kubectl")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to kubeconfig file (used with --live)")
	cmd.Flags().StringVar(&kubeCtx, "context", "", "Kubernetes context (used with --live)")
	cmd.Flags().StringSliceVar(&namespaces, "namespace", nil, "namespace to scan (repeatable; default: all non-system)")
	return cmd
}

func scanComposeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "compose <path> [path...]",
		Short: "Scan Docker Compose files for service dependencies",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, cfg := openStore()
			defer store.Close() //nolint:errcheck // best-effort cleanup

			fmt.Printf("Scanning Docker Compose across %d path(s)...\n", len(args))
			sc := scanner.New(store, cfg, logger)
			r := sc.RunSync(cmd.Context(), scanner.ScanRequest{
				Source: "compose",
				Paths:  args,
			})
			printScanResult(r)
			if r.Error != nil {
				return r.Error
			}
			return nil
		},
	}
}

func printScanResult(r scanner.ScanResult) {
	if r.Error != nil {
		fmt.Printf("Scan failed: %v\n", r.Error)
		return
	}
	fmt.Printf("Discovered %d nodes, %d edges\n", r.NodesFound, r.EdgesFound)
	for _, w := range r.Warnings {
		fmt.Printf("  warning: %s\n", w)
	}
}

// --- graph ---

func graphCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "graph",
		Short: "Query the asset graph",
	}
	cmd.AddCommand(graphShowCmd(), graphNodesCmd(), graphEdgesCmd(), graphNeighborsCmd(), graphPathCmd(), graphDepsCmd(), graphPruneCmd(), graphExportCmd(), graphSyncCmd())
	return cmd
}

func graphShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print graph summary",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, _ := openStore()
			defer store.Close() //nolint:errcheck // best-effort cleanup
			ctx := cmd.Context()

			nodeCount, _ := store.NodeCount(ctx)
			edgeCount, _ := store.EdgeCount(ctx)
			nodesByType, _ := store.NodeCountByType(ctx)
			edgesByType, _ := store.EdgeCountByType(ctx)

			fmt.Printf("Graph Summary\n")
			fmt.Printf("  Total nodes: %d\n", nodeCount)
			fmt.Printf("  Total edges: %d\n\n", edgeCount)

			fmt.Printf("Nodes by type:\n")
			for t, c := range nodesByType {
				fmt.Printf("  %-20s %d\n", t, c)
			}

			fmt.Printf("\nEdges by type:\n")
			for t, c := range edgesByType {
				fmt.Printf("  %-20s %d\n", t, c)
			}

			return nil
		},
	}
}

func graphNodesCmd() *cobra.Command {
	var nodeType, source, provider string

	cmd := &cobra.Command{
		Use:   "nodes",
		Short: "List all nodes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, _ := openStore()
			defer store.Close() //nolint:errcheck // best-effort cleanup
			ctx := cmd.Context()

			nodes, err := store.ListNodes(ctx, graph.NodeFilter{
				Type: nodeType, Source: source, Provider: provider,
			})
			if err != nil {
				return err
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "ID\tNAME\tTYPE\tSOURCE\tPROVIDER")
			for _, n := range nodes {
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", n.ID, n.Name, n.Type, n.Source, n.Provider)
			}
			return w.Flush()
		},
	}

	cmd.Flags().StringVar(&nodeType, "type", "", "filter by asset type")
	cmd.Flags().StringVar(&source, "source", "", "filter by source")
	cmd.Flags().StringVar(&provider, "provider", "", "filter by provider")
	return cmd
}

func graphEdgesCmd() *cobra.Command {
	var edgeType, from, to string

	cmd := &cobra.Command{
		Use:   "edges",
		Short: "List all edges",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, _ := openStore()
			defer store.Close() //nolint:errcheck // best-effort cleanup
			ctx := cmd.Context()

			edges, err := store.ListEdges(ctx, graph.EdgeFilter{
				Type: edgeType, FromID: from, ToID: to,
			})
			if err != nil {
				return err
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "FROM\tTYPE\tTO")
			for _, e := range edges {
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", e.FromID, e.Type, e.ToID)
			}
			return w.Flush()
		},
	}

	cmd.Flags().StringVar(&edgeType, "type", "", "filter by edge type")
	cmd.Flags().StringVar(&from, "from", "", "filter by source node")
	cmd.Flags().StringVar(&to, "to", "", "filter by target node")
	return cmd
}

func graphNeighborsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "neighbors <node-id>",
		Short: "Show direct neighbors of a node",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _ := openStore()
			defer store.Close() //nolint:errcheck // best-effort cleanup
			ctx := cmd.Context()

			nodeID := args[0]
			node, err := store.GetNode(ctx, nodeID)
			if err != nil {
				return err
			}
			if node == nil {
				return fmt.Errorf("node %q not found", nodeID)
			}

			fmt.Printf("Neighbors of %s (%s, %s)\n\n", node.Name, node.Type, node.Source)

			neighbors, err := store.GetNeighbors(ctx, nodeID)
			if err != nil {
				return err
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "ID\tNAME\tTYPE\tSOURCE")
			for _, n := range neighbors {
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", n.ID, n.Name, n.Type, n.Source)
			}
			return w.Flush()
		},
	}
}

func graphPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path <from-id> <to-id>",
		Short: "Find shortest path between two nodes",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, engine, _ := openStoreAndEngine()
			defer store.Close()  //nolint:errcheck // best-effort cleanup
			defer engine.Close() //nolint:errcheck // best-effort cleanup
			ctx := cmd.Context()

			fromID, toID := args[0], args[1]

			// Validate both nodes exist
			fromNode, err := store.GetNode(ctx, fromID)
			if err != nil {
				return err
			}
			if fromNode == nil {
				return fmt.Errorf("node %q not found", fromID)
			}
			toNode, err := store.GetNode(ctx, toID)
			if err != nil {
				return err
			}
			if toNode == nil {
				return fmt.Errorf("node %q not found", toID)
			}

			nodes, _, err := engine.ShortestPath(ctx, fromID, toID)
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintf(os.Stdout, "Shortest path: %s → %s (%d steps)\n\n", fromID, toID, len(nodes)-1)

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "STEP\tNODE ID\tNAME\tTYPE")
			for i, n := range nodes {
				_, _ = fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", i, n.ID, n.Name, n.Type)
			}
			return w.Flush()
		},
	}
}

func graphDepsCmd() *cobra.Command {
	var depth int

	cmd := &cobra.Command{
		Use:   "deps <node-id>",
		Short: "Show downstream dependencies of a node",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, engine, _ := openStoreAndEngine()
			defer store.Close()  //nolint:errcheck // best-effort cleanup
			defer engine.Close() //nolint:errcheck // best-effort cleanup
			ctx := cmd.Context()

			nodeID := args[0]
			node, err := store.GetNode(ctx, nodeID)
			if err != nil {
				return err
			}
			if node == nil {
				return fmt.Errorf("node %q not found", nodeID)
			}

			deps, err := engine.DependencyChain(ctx, nodeID, depth)
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintf(os.Stdout, "Dependencies of %s (%s, %s) — depth %d\n\n", node.Name, node.Type, node.Source, depth)

			if len(deps) == 0 {
				_, _ = fmt.Fprintln(os.Stdout, "No dependencies found.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "ID\tNAME\tTYPE\tSOURCE")
			for _, n := range deps {
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", n.ID, n.Name, n.Type, n.Source)
			}
			return w.Flush()
		},
	}

	cmd.Flags().IntVar(&depth, "depth", 10, "maximum traversal depth (1-50)")
	return cmd
}

func graphPruneCmd() *cobra.Command {
	var staleDays int
	var source string
	var force bool

	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Remove stale nodes from the graph",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if staleDays <= 0 && source == "" {
				return fmt.Errorf("specify at least one filter: --stale-days or --source")
			}

			store, _ := openStore()
			defer store.Close() //nolint:errcheck // best-effort cleanup
			ctx := cmd.Context()

			nodes, err := store.ListNodes(ctx, graph.NodeFilter{
				StaleDays: staleDays,
				Source:    source,
			})
			if err != nil {
				return err
			}

			if len(nodes) == 0 {
				_, _ = fmt.Fprintln(os.Stdout, "No matching nodes found.")
				return nil
			}

			_, _ = fmt.Fprintf(os.Stdout, "Found %d nodes to prune:\n\n", len(nodes))
			limit := 10
			if len(nodes) < limit {
				limit = len(nodes)
			}
			for _, n := range nodes[:limit] {
				_, _ = fmt.Fprintf(os.Stdout, "  %s (%s, last seen: %s)\n", n.ID, n.Type, n.LastSeen.Format("2006-01-02"))
			}
			if len(nodes) > 10 {
				_, _ = fmt.Fprintf(os.Stdout, "  ... and %d more\n", len(nodes)-10)
			}

			if !force {
				_, _ = fmt.Fprintf(os.Stdout, "\nDelete %d nodes? [y/N]: ", len(nodes))
				reader := bufio.NewReader(os.Stdin)
				answer, _ := reader.ReadString('\n')
				answer = strings.TrimSpace(strings.ToLower(answer))
				if answer != "y" && answer != "yes" {
					_, _ = fmt.Fprintln(os.Stdout, "Aborted.")
					return nil
				}
			}

			deleted := 0
			for _, n := range nodes {
				if err := store.DeleteNode(ctx, n.ID); err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "error deleting %s: %v\n", n.ID, err)
					continue
				}
				deleted++
			}

			_, _ = fmt.Fprintf(os.Stdout, "Deleted %d nodes (and their edges).\n", deleted)
			return nil
		},
	}

	cmd.Flags().IntVar(&staleDays, "stale-days", 0, "delete nodes not seen in N days")
	cmd.Flags().StringVar(&source, "source", "", "delete nodes from this source")
	cmd.Flags().BoolVar(&force, "force", false, "skip confirmation prompt")
	return cmd
}

func graphExportCmd() *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export graph in various formats",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, _ := openStore()
			defer store.Close() //nolint:errcheck // best-effort cleanup
			ctx := cmd.Context()

			var output string
			var err error

			switch format {
			case "json":
				output, err = graph.ExportJSON(ctx, store)
			case "dot":
				output, err = graph.ExportDOT(ctx, store)
			case "mermaid":
				output, err = graph.ExportMermaid(ctx, store)
			default:
				return fmt.Errorf("unsupported format %q (use: json, dot, mermaid)", format)
			}

			if err != nil {
				return err
			}

			fmt.Print(output)
			return nil
		},
	}

	cmd.Flags().StringVar(&format, "format", "json", "export format: json, dot, mermaid")
	return cmd
}

func graphSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Synchronize graph data from SQLite to Memgraph",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, cfg := openStore()
			defer store.Close() //nolint:errcheck // best-effort cleanup

			if !cfg.Storage.Memgraph.Enabled {
				return fmt.Errorf("memgraph is not enabled in configuration (set storage.memgraph.enabled: true)")
			}

			auth := neo4j.NoAuth()
			if cfg.Storage.Memgraph.Username != "" {
				auth = neo4j.BasicAuth(cfg.Storage.Memgraph.Username, cfg.Storage.Memgraph.Password, "")
			}

			driver, err := neo4j.NewDriverWithContext(cfg.Storage.Memgraph.URI, auth)
			if err != nil {
				return fmt.Errorf("connecting to memgraph: %w", err)
			}
			defer driver.Close(context.Background()) //nolint:errcheck // best-effort cleanup

			return graph.SyncToMemgraph(cmd.Context(), store, driver, logger)
		},
	}
}

// --- impact ---

func impactCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "impact",
		Short: "Blast radius analysis",
	}
	cmd.AddCommand(impactNodeCmd())
	return cmd
}

func impactNodeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "node <node-id>",
		Short: "Analyze what breaks if a node fails",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, engine, _ := openStoreAndEngine()
			defer store.Close() //nolint:errcheck // best-effort cleanup
			defer engine.Close() //nolint:errcheck // best-effort cleanup
			ctx := cmd.Context()

			nodeID := args[0]
			node, err := store.GetNode(ctx, nodeID)
			if err != nil {
				return err
			}
			if node == nil {
				return fmt.Errorf("node %q not found", nodeID)
			}

			tree, err := engine.BlastRadiusTree(ctx, nodeID)
			if err != nil {
				return err
			}

			// Count total affected
			total := countTreeNodes(tree) - 1
			fmt.Printf("\nImpact Analysis: %s\n", nodeID)
			fmt.Printf("   Type: %s | Provider: %s | Source: %s\n", node.Type, node.Provider, node.Source)
			fmt.Printf("\n   Blast Radius: %d affected assets\n\n", total)

			printTree(ctx, tree, "   ", true)

			// Check for expiring certs in the tree
			warnings := collectWarnings(tree)
			if len(warnings) > 0 {
				fmt.Printf("\n   Warnings:\n")
				for _, w := range warnings {
					fmt.Printf("   - %s\n", w)
				}
			}
			fmt.Println()

			return nil
		},
	}
}

func countTreeNodes(n *graph.ImpactNode) int {
	count := 1
	for i := range n.Children {
		count += countTreeNodes(&n.Children[i])
	}
	return count
}

func printTree(ctx context.Context, n *graph.ImpactNode, prefix string, isRoot bool) {
	label := n.NodeID
	if n.Node != nil {
		label = fmt.Sprintf("%s (%s)", n.NodeID, n.Node.Type)
		if n.Node.ExpiresAt != nil {
			days := certs.DaysUntilExpiry(*n.Node.ExpiresAt)
			if days <= 30 {
				label += fmt.Sprintf(" [!] expires in %dd", days)
			}
		}
	}

	if isRoot {
		fmt.Printf("%s%s\n", prefix, label)
	}

	for i, child := range n.Children {
		connector := "├── "
		childPrefix := prefix + "│   "
		if i == len(n.Children)-1 {
			connector = "└── "
			childPrefix = prefix + "    "
		}
		childLabel := child.NodeID
		if child.Node != nil {
			childLabel = fmt.Sprintf("%s (%s)", child.NodeID, child.Node.Type)
			if child.Node.ExpiresAt != nil {
				days := certs.DaysUntilExpiry(*child.Node.ExpiresAt)
				if days <= 30 {
					childLabel += fmt.Sprintf(" [!] expires in %dd", days)
				}
			}
		}
		fmt.Printf("%s%s[%s] %s\n", prefix, connector, child.EdgeType, childLabel)
		printTree(ctx, &child, childPrefix, false)
	}
}

func collectWarnings(n *graph.ImpactNode) []string {
	var warnings []string
	if n.Node != nil && n.Node.ExpiresAt != nil {
		days := certs.DaysUntilExpiry(*n.Node.ExpiresAt)
		if days <= 30 {
			warnings = append(warnings, fmt.Sprintf("%s expires in %d days", n.NodeID, days))
		}
	}
	for i := range n.Children {
		warnings = append(warnings, collectWarnings(&n.Children[i])...)
	}
	return warnings
}

// --- certs ---

func certsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "certs",
		Short: "Certificate management",
	}
	cmd.AddCommand(certsListCmd(), certsExpiringCmd(), certsProbeCmd(), certsCheckCmd())
	return cmd
}

func certsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all tracked certificates",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, cfg := openStore()
			defer store.Close() //nolint:errcheck // best-effort cleanup
			ctx := cmd.Context()

			tracker := certs.NewTracker(store, cfg.Certs.AlertThresholds, logger)
			certList, err := tracker.ListCerts(ctx)
			if err != nil {
				return err
			}

			if len(certList) == 0 {
				fmt.Println("No certificates found. Run a scan or probe first.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "ID\tNAME\tEXPIRES\tDAYS\tSTATUS")
			for _, c := range certList {
				expires := "-"
				if c.Node.ExpiresAt != nil {
					expires = c.Node.ExpiresAt.Format("2006-01-02")
				}
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n",
					c.Node.ID, c.Node.Name, expires, c.DaysRemaining, strings.ToUpper(c.Status))
			}
			return w.Flush()
		},
	}
}

func certsExpiringCmd() *cobra.Command {
	var days int

	cmd := &cobra.Command{
		Use:   "expiring",
		Short: "Show certificates expiring within threshold",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, cfg := openStore()
			defer store.Close() //nolint:errcheck // best-effort cleanup
			ctx := cmd.Context()

			tracker := certs.NewTracker(store, cfg.Certs.AlertThresholds, logger)
			certList, err := tracker.ExpiringCerts(ctx, days)
			if err != nil {
				return err
			}

			if len(certList) == 0 {
				fmt.Printf("No certificates expiring within %d days.\n", days)
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "ID\tNAME\tEXPIRES\tDAYS\tSTATUS")
			for _, c := range certList {
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n",
					c.Node.ID, c.Node.Name, c.Node.ExpiresAt.Format("2006-01-02"), c.DaysRemaining, strings.ToUpper(c.Status))
			}
			return w.Flush()
		},
	}

	cmd.Flags().IntVar(&days, "days", 30, "expiry threshold in days")
	return cmd
}

func certsProbeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "probe <host:port>",
		Short: "Probe a TLS endpoint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, cfg := openStore()
			defer store.Close() //nolint:errcheck // best-effort cleanup
			ctx := cmd.Context()

			tracker := certs.NewTracker(store, cfg.Certs.AlertThresholds, logger)
			ci, err := tracker.ProbeAndStore(ctx, args[0])
			if err != nil {
				return err
			}

			fmt.Printf("Certificate: %s\n", ci.Node.Name)
			fmt.Printf("  ID:      %s\n", ci.Node.ID)
			fmt.Printf("  Issuer:  %s\n", ci.Node.Provider)
			if ci.Node.ExpiresAt != nil {
				fmt.Printf("  Expires: %s (%d days)\n", ci.Node.ExpiresAt.Format("2006-01-02"), ci.DaysRemaining)
			}
			fmt.Printf("  Status:  %s\n", strings.ToUpper(ci.Status))
			return nil
		},
	}
}

func certsCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Re-probe all known certificate endpoints",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, cfg := openStore()
			defer store.Close() //nolint:errcheck // best-effort cleanup
			ctx := cmd.Context()

			tracker := certs.NewTracker(store, cfg.Certs.AlertThresholds, logger)
			results := certs.ProbeAll(ctx, tracker, store, logger)

			// Send alerts for expiring certs
			var alerters []alert.Alerter
			if cfg.Alerts.Stdout.Enabled {
				alerters = append(alerters, alert.NewStdoutAlerter())
			}
			if cfg.Alerts.Webhook.Enabled && cfg.Alerts.Webhook.URL != "" {
				alerters = append(alerters, alert.NewWebhookAlerter(cfg.Alerts.Webhook.URL, cfg.Alerts.Webhook.Headers))
			}
			multi := alert.NewMulti(alerters...)

			for _, ci := range results {
				if ci.Status == "warning" || ci.Status == "critical" || ci.Status == "expired" {
					event := alert.Event{
						Source:    "aib",
						EventType: "cert_expiring",
						Severity:  ci.Status,
						Asset: alert.Asset{
							ID:            ci.Node.ID,
							Name:          ci.Node.Name,
							Type:          string(ci.Node.Type),
							DaysRemaining: ci.DaysRemaining,
						},
						Message:   fmt.Sprintf("Certificate %s expires in %d days", ci.Node.Name, ci.DaysRemaining),
						Timestamp: time.Now(),
					}
					if ci.Node.ExpiresAt != nil {
						event.Asset.ExpiresAt = ci.Node.ExpiresAt.Format(time.RFC3339)
					}
					_ = multi.Send(ctx, event)
				}
			}

			return nil
		},
	}
}

// --- serve ---

func serveCmd() *cobra.Command {
	var listen string
	var readOnly bool

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start web UI and API server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, engine, cfg := openStoreAndEngine()

			if listen == "" {
				listen = cfg.Server.Listen
			}

			tracker := certs.NewTracker(store, cfg.Certs.AlertThresholds, logger)
			sc := scanner.New(store, cfg, logger)
			srv := server.New(store, engine, tracker, sc, logger, listen, readOnly || cfg.Server.ReadOnly, cfg.Server.APIToken, cfg.Server.CORSOrigin, version)

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()

			// On-startup scan
			if cfg.Scan.OnStartup && len(cfg.Sources.Terraform)+len(cfg.Sources.Kubernetes)+len(cfg.Sources.Ansible)+len(cfg.Sources.Compose) > 0 {
				go func() {
					logger.Info("running startup scan")
					results := sc.RunAllConfigured(context.Background())
					for _, r := range results {
						if r.Error != nil {
							logger.Error("startup scan failed", "error", r.Error)
						} else {
							logger.Info("startup scan completed", "scanID", r.ScanID,
								"nodes", r.NodesFound, "edges", r.EdgesFound)
						}
					}
				}()
			}

			// Scheduled cert probing
			if cfg.Certs.ProbeEnabled && cfg.Certs.ProbeInterval != "" {
				var alerters []alert.Alerter
				if cfg.Alerts.Stdout.Enabled {
					alerters = append(alerters, alert.NewStdoutAlerter())
				}
				if cfg.Alerts.Webhook.Enabled && cfg.Alerts.Webhook.URL != "" {
					alerters = append(alerters, alert.NewWebhookAlerter(cfg.Alerts.Webhook.URL, cfg.Alerts.Webhook.Headers))
				}
				certSched, err := certs.NewCertScheduler(tracker, store, alert.NewMulti(alerters...), cfg.Certs.ProbeInterval, logger)
				if err != nil {
					logger.Error("invalid cert probe interval", "error", err)
				} else {
					certSched.Start(ctx)
					defer certSched.Stop()
				}
			}

			// Scheduled scans
			if cfg.Scan.Schedule != "" {
				sched, err := scanner.NewScheduler(sc, cfg.Scan.Schedule, logger)
				if err != nil {
					logger.Error("invalid scan schedule", "error", err)
				} else {
					sched.Start(ctx)
					defer sched.Stop()
				}
			}

			go func() {
				<-ctx.Done()
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = srv.Shutdown(shutdownCtx)
				_ = engine.Close()
				_ = store.Close()
			}()

			return srv.Start()
		},
	}

	cmd.Flags().StringVar(&listen, "listen", "", "listen address (default from config or :8080)")
	cmd.Flags().BoolVar(&readOnly, "read-only", false, "disable scan triggers via API")
	return cmd
}

// --- db ---

func dbCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "db",
		Short: "Database management",
	}
	cmd.AddCommand(dbStatsCmd(), dbBackupCmd())
	return cmd
}

func dbStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Show database statistics",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, cfg := openStore()
			defer store.Close() //nolint:errcheck // best-effort cleanup
			ctx := cmd.Context()

			path := cfg.Storage.Path
			if dbPath != "" {
				path = dbPath
			}

			// File size
			info, err := os.Stat(path)
			var sizeStr string
			if err == nil {
				sizeStr = formatBytes(info.Size())
			} else {
				sizeStr = "unknown"
			}

			nodeCount, _ := store.NodeCount(ctx)
			edgeCount, _ := store.EdgeCount(ctx)
			nodesByType, _ := store.NodeCountByType(ctx)
			edgesByType, _ := store.EdgeCountByType(ctx)
			scans, _ := store.ListScans(ctx, 100)

			_, _ = fmt.Fprintf(os.Stdout, "Database: %s (%s)\n\n", path, sizeStr)
			_, _ = fmt.Fprintf(os.Stdout, "Nodes: %d\n", nodeCount)
			for t, c := range nodesByType {
				_, _ = fmt.Fprintf(os.Stdout, "  %-20s %d\n", t, c)
			}
			_, _ = fmt.Fprintf(os.Stdout, "\nEdges: %d\n", edgeCount)
			for t, c := range edgesByType {
				_, _ = fmt.Fprintf(os.Stdout, "  %-20s %d\n", t, c)
			}

			// Scan summary
			statusCounts := make(map[string]int)
			for _, s := range scans {
				statusCounts[s.Status]++
			}
			_, _ = fmt.Fprintf(os.Stdout, "\nScans: %d total\n", len(scans))
			for status, count := range statusCounts {
				_, _ = fmt.Fprintf(os.Stdout, "  %-20s %d\n", status, count)
			}

			return nil
		},
	}
}

func dbBackupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "backup <output-path>",
		Short: "Copy database file to a backup location",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgFile)
			if err != nil {
				return err
			}

			srcPath := cfg.Storage.Path
			if dbPath != "" {
				srcPath = dbPath
			}
			dstPath := args[0]

			// Check if destination exists
			if _, err := os.Stat(dstPath); err == nil {
				_, _ = fmt.Fprintf(os.Stdout, "File %s already exists. Overwrite? [y/N]: ", dstPath)
				reader := bufio.NewReader(os.Stdin)
				answer, _ := reader.ReadString('\n')
				answer = strings.TrimSpace(strings.ToLower(answer))
				if answer != "y" && answer != "yes" {
					_, _ = fmt.Fprintln(os.Stdout, "Aborted.")
					return nil
				}
			}

			// Create parent directory
			if err := os.MkdirAll(filepath.Dir(dstPath), 0o750); err != nil {
				return fmt.Errorf("creating backup directory: %w", err)
			}

			src, err := os.Open(srcPath) // #nosec G304 -- path from config/flag
			if err != nil {
				return fmt.Errorf("opening source database: %w", err)
			}
			defer src.Close() //nolint:errcheck // best-effort cleanup

			dst, err := os.Create(dstPath) // #nosec G304 -- path from user CLI arg
			if err != nil {
				return fmt.Errorf("creating backup file: %w", err)
			}
			defer dst.Close() //nolint:errcheck // best-effort cleanup

			n, err := io.Copy(dst, src)
			if err != nil {
				return fmt.Errorf("copying database: %w", err)
			}

			_, _ = fmt.Fprintf(os.Stdout, "Backed up %s to %s (%s)\n", srcPath, dstPath, formatBytes(n))
			return nil
		},
	}
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// --- version ---

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Printf("aib %s\n", version)
		},
	}
}

func parseLogLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("invalid --log-level %q (use: debug, info, warn, error)", s)
	}
}

func completionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion scripts",
		Long: `Generate shell completion scripts for AIB.

To load completions:

Bash:
  $ source <(aib completion bash)
  # To load completions for each session, execute once:
  # Linux:
  $ aib completion bash > /etc/bash_completion.d/aib
  # macOS:
  $ aib completion bash > $(brew --prefix)/etc/bash_completion.d/aib

Zsh:
  # If shell completion is not already enabled in your environment,
  # you will need to enable it. Execute once:
  $ echo "autoload -U compinit; compinit" >> ~/.zshrc
  # To load completions for each session, execute once:
  $ aib completion zsh > "${fpath[1]}/_aib"
  # You will need to start a new shell for this setup to take effect.

Fish:
  $ aib completion fish | source
  # To load completions for each session, execute once:
  $ aib completion fish > ~/.config/fish/completions/aib.fish

PowerShell:
  PS> aib completion powershell | Out-String | Invoke-Expression
  # To load completions for every new session, add the output to your profile:
  PS> aib completion powershell >> $PROFILE
`,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return cmd.Root().GenBashCompletion(os.Stdout)
			case "zsh":
				return cmd.Root().GenZshCompletion(os.Stdout)
			case "fish":
				return cmd.Root().GenFishCompletion(os.Stdout, true)
			case "powershell":
				return cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
			default:
				return fmt.Errorf("unsupported shell: %s", args[0])
			}
		},
	}
}

