package scanner

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestLogResults(t *testing.T) {
	var buf bytes.Buffer
	lg := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	LogResults(lg, "startup", []ScanResult{
		{ScanID: 1, NodesFound: 5, EdgesFound: 2},
		{ScanID: 2, NodesFound: 0},
		{ScanID: 3, Error: errors.New("boom")},
		{ScanID: 4, NodesFound: 1, Warnings: []string{"include escaped"}},
	})
	out := buf.String()
	for _, want := range []string{
		"level=INFO msg=\"startup scan completed\" scanID=1",
		"level=WARN msg=\"startup scan found no assets",
		"level=ERROR msg=\"startup scan failed\" scanID=3",
		"level=WARN msg=\"startup scan warning\" scanID=4",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "scanID=2 nodes=0 edges=0\n") && strings.Contains(out, "completed\" scanID=2") {
		t.Error("a zero-asset scan must not be logged as completed")
	}
}
