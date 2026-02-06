package kubernetes

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/matijazezelj/aib/internal/parser"
)

// FetchLive connects to a running Kubernetes cluster via kubectl and pulls
// resources. If kubeconfig is empty, kubectl uses its default config.
// If kubeCtx is empty, the current-context is used.
// If namespaces is empty, all non-system namespaces are scanned.
func FetchLive(ctx context.Context, kubeconfig, kubeCtx string, namespaces []string) (*parser.ParseResult, error) {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return nil, fmt.Errorf("kubectl not found in PATH: %w", err)
	}

	if len(namespaces) == 0 {
		var err error
		namespaces, err = listNamespaces(ctx, kubeconfig, kubeCtx)
		if err != nil {
			return nil, fmt.Errorf("listing namespaces: %w", err)
		}
	}

	result := &parser.ParseResult{}
	now := time.Now()

	resourceTypes := "deployments,statefulsets,daemonsets,services,ingresses,configmaps,secrets,serviceaccounts,roles,rolebindings,networkpolicies,jobs,cronjobs,horizontalpodautoscalers"

	for _, ns := range namespaces {
		data, err := kubectlGet(ctx, kubeconfig, kubeCtx, ns, resourceTypes)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("namespace %s: %v", ns, err))
			continue
		}
		if len(bytes.TrimSpace(data)) == 0 {
			continue
		}
		r, err := parseManifests(data, fmt.Sprintf("live:%s", ns), now)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("parsing namespace %s: %v", ns, err))
			continue
		}
		result.Nodes = append(result.Nodes, r.Nodes...)
		result.Edges = append(result.Edges, r.Edges...)
		result.Warnings = append(result.Warnings, r.Warnings...)
	}

	// Try cert-manager certificates separately (may not be installed)
	for _, ns := range namespaces {
		data, err := kubectlGet(ctx, kubeconfig, kubeCtx, ns, "certificates.cert-manager.io")
		if err != nil {
			continue // cert-manager may not be installed, skip silently
		}
		if len(bytes.TrimSpace(data)) == 0 {
			continue
		}
		r, err := parseManifests(data, fmt.Sprintf("live:%s", ns), now)
		if err != nil {
			continue
		}
		result.Nodes = append(result.Nodes, r.Nodes...)
		result.Edges = append(result.Edges, r.Edges...)
	}

	return result, nil
}

// listNamespaces runs kubectl get namespaces and returns non-system namespace names.
func listNamespaces(ctx context.Context, kubeconfig, kubeCtx string) ([]string, error) {
	args := buildKubectlArgs(kubeconfig, kubeCtx)
	args = append(args, "get", "namespaces", "-o", "jsonpath={.items[*].metadata.name}")

	cmd := exec.CommandContext(ctx, "kubectl", args...) // #nosec G204 -- args are constructed internally
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("kubectl get namespaces: %s", stderr.String())
	}

	systemNamespaces := map[string]bool{
		"kube-system":     true,
		"kube-public":     true,
		"kube-node-lease": true,
	}

	var result []string
	for _, name := range bytes.Fields(stdout.Bytes()) {
		ns := string(name)
		if !systemNamespaces[ns] {
			result = append(result, ns)
		}
	}
	return result, nil
}

// kubectlGet runs kubectl get <resources> -n <namespace> -o yaml.
func kubectlGet(ctx context.Context, kubeconfig, kubeCtx, namespace, resourceTypes string) ([]byte, error) {
	args := buildKubectlArgs(kubeconfig, kubeCtx)
	args = append(args, "get", resourceTypes, "-n", namespace, "-o", "yaml")

	cmd := exec.CommandContext(ctx, "kubectl", args...) // #nosec G204 -- args are constructed internally
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("kubectl get %s -n %s: %s", resourceTypes, namespace, stderr.String())
	}
	return stdout.Bytes(), nil
}

// buildKubectlArgs returns common kubectl flags for kubeconfig and context.
func buildKubectlArgs(kubeconfig, kubeCtx string) []string {
	var args []string
	if kubeconfig != "" {
		args = append(args, "--kubeconfig", kubeconfig)
	}
	if kubeCtx != "" {
		args = append(args, "--context", kubeCtx)
	}
	return args
}
