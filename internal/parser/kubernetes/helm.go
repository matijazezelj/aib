package kubernetes

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/matijazezelj/aib/internal/parser"
)

// RenderHelm runs `helm template` on a chart directory and parses the output.
func RenderHelm(ctx context.Context, chartPath string, valuesFile string) (*parser.ParseResult, error) {
	if _, err := exec.LookPath("helm"); err != nil {
		return nil, fmt.Errorf("helm CLI not found in PATH: %w", err)
	}

	ctx, cancel := parser.WithDefaultCommandTimeout(ctx)
	defer cancel()

	args := []string{"template", "release", chartPath}
	if valuesFile != "" {
		resolvedValues, err := parser.SafeResolvePath(valuesFile)
		if err != nil {
			return nil, fmt.Errorf("resolving values file path: %w", err)
		}
		args = append(args, "-f", resolvedValues)
	}

	cmd := exec.CommandContext(ctx, "helm", args...) // #nosec G204 -- args are constructed internally
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("helm template failed: %s", stderr.String())
	}

	if stdout.Len() == 0 {
		return nil, fmt.Errorf("helm template returned empty output")
	}

	return parseManifests(stdout.Bytes(), chartPath, time.Now())
}
