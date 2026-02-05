package alert

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testEvent() Event {
	return Event{
		Source:    "test",
		EventType: "cert_expiring",
		Severity:  "warning",
		Asset: Asset{
			ID:            "probe:certificate:example.com",
			Name:          "example.com",
			Type:          "certificate",
			DaysRemaining: 14,
		},
		Message:   "Certificate expiring in 14 days",
		Timestamp: time.Now(),
	}
}

func TestWebhookAlerter_Success(t *testing.T) {
	var received Event
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content-type = %q", r.Header.Get("Content-Type"))
		}
		_ = json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	alerter := NewWebhookAlerter(server.URL, nil)
	err := alerter.Send(context.Background(), testEvent())
	if err != nil {
		t.Fatal(err)
	}

	if received.EventType != "cert_expiring" {
		t.Errorf("event_type = %q, want cert_expiring", received.EventType)
	}
}

func TestWebhookAlerter_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	alerter := NewWebhookAlerter(server.URL, nil)
	err := alerter.Send(context.Background(), testEvent())
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestWebhookAlerter_CustomHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom") != "value" {
			t.Errorf("X-Custom = %q, want value", r.Header.Get("X-Custom"))
		}
		if r.Header.Get("Authorization") != "Bearer token123" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	headers := map[string]string{
		"X-Custom":      "value",
		"Authorization": "Bearer token123",
	}
	alerter := NewWebhookAlerter(server.URL, headers)
	if err := alerter.Send(context.Background(), testEvent()); err != nil {
		t.Fatal(err)
	}
}

func TestWebhookAlerter_Name(t *testing.T) {
	a := NewWebhookAlerter("http://example.com", nil)
	if a.Name() != "webhook" {
		t.Errorf("name = %q, want webhook", a.Name())
	}
}

func TestStdoutAlerter_Name(t *testing.T) {
	a := NewStdoutAlerter()
	if a.Name() != "stdout" {
		t.Errorf("name = %q, want stdout", a.Name())
	}
}

func TestStdoutAlerter_Send(t *testing.T) {
	a := NewStdoutAlerter()
	// Should not return error
	err := a.Send(context.Background(), testEvent())
	if err != nil {
		t.Errorf("stdout send error: %v", err)
	}
}

func TestMulti_DispatchesAll(t *testing.T) {
	var count int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	wh1 := NewWebhookAlerter(server.URL, nil)
	wh2 := NewWebhookAlerter(server.URL, nil)
	multi := NewMulti(wh1, wh2)

	err := multi.Send(context.Background(), testEvent())
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("multi dispatched to %d, want 2", count)
	}
}

func TestMulti_ReturnsLastError(t *testing.T) {
	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failServer.Close()

	okServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer okServer.Close()

	wh1 := NewWebhookAlerter(okServer.URL, nil)
	wh2 := NewWebhookAlerter(failServer.URL, nil)
	multi := NewMulti(wh1, wh2)

	err := multi.Send(context.Background(), testEvent())
	if err == nil {
		t.Error("expected error from failing alerter")
	}
}
