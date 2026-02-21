package alert

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTeamsAlerter_Name(t *testing.T) {
	a := NewTeamsAlerter("https://example.webhook.office.com/webhookb2/...")
	if a.Name() != "teams" {
		t.Errorf("name = %q, want teams", a.Name())
	}
}

func TestTeamsAlerter_Success(t *testing.T) {
	var received teamsPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content-type = %q", r.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decoding payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	alerter := NewTeamsAlerter(server.URL)
	err := alerter.Send(context.Background(), testEvent())
	if err != nil {
		t.Fatal(err)
	}

	if received.Type != "message" {
		t.Errorf("type = %q, want message", received.Type)
	}

	if len(received.Attachments) != 1 {
		t.Fatalf("attachments = %d, want 1", len(received.Attachments))
	}
	att := received.Attachments[0]
	if att.ContentType != "application/vnd.microsoft.card.adaptive" {
		t.Errorf("contentType = %q, want application/vnd.microsoft.card.adaptive", att.ContentType)
	}

	if len(att.Content.Body) != 4 {
		t.Errorf("body items = %d, want 4", len(att.Content.Body))
	}
}

func TestTeamsAlerter_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	alerter := NewTeamsAlerter(server.URL)
	err := alerter.Send(context.Background(), testEvent())
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestTeamsAlerter_SeverityColors(t *testing.T) {
	tests := []struct {
		severity string
		color    string
	}{
		{"critical", "Attention"},
		{"expired", "Attention"},
		{"warning", "Warning"},
		{"ok", "Good"},
		{"unknown", "Default"},
	}

	for _, tt := range tests {
		t.Run(tt.severity, func(t *testing.T) {
			got := severityTeamsColor(tt.severity)
			if got != tt.color {
				t.Errorf("severityTeamsColor(%q) = %q, want %q", tt.severity, got, tt.color)
			}
		})
	}
}

func TestTeamsAlerter_WithImpact(t *testing.T) {
	var received teamsPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decoding payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	event := Event{
		Source:    "aib",
		EventType: "cert_expiring",
		Severity:  "critical",
		Asset: Asset{
			ID:            "probe:certificate:api.example.com",
			Name:          "api.example.com",
			Type:          "certificate",
			DaysRemaining: 3,
		},
		Impact: &Impact{
			AffectedCount:    5,
			AffectedServices: []string{"web-frontend", "api-gateway"},
		},
		Message:   "Certificate expiring in 3 days",
		Timestamp: time.Now(),
	}

	alerter := NewTeamsAlerter(server.URL)
	if err := alerter.Send(context.Background(), event); err != nil {
		t.Fatal(err)
	}

	att := received.Attachments[0]
	if len(att.Content.Body) != 5 {
		t.Errorf("body items = %d, want 5 (with impact)", len(att.Content.Body))
	}
}
