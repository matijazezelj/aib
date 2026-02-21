package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// TeamsAlerter sends events to a Microsoft Teams incoming webhook using Adaptive Cards.
type TeamsAlerter struct {
	webhookURL string
	client     *http.Client
}

// NewTeamsAlerter creates a new Teams alerter that posts to the given webhook URL.
func NewTeamsAlerter(webhookURL string) *TeamsAlerter {
	return &TeamsAlerter{
		webhookURL: webhookURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Name returns "teams".
func (t *TeamsAlerter) Name() string {
	return "teams"
}

// Send formats the event as a Teams Adaptive Card and posts it to the webhook.
func (t *TeamsAlerter) Send(ctx context.Context, event Event) error {
	payload := t.buildPayload(event)

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling teams payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req) //#nosec G704 -- URL is from trusted config, not user input
	if err != nil {
		return fmt.Errorf("sending teams webhook: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort cleanup

	// Drain body to enable HTTP connection reuse.
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("teams webhook returned status %d", resp.StatusCode)
	}

	return nil
}

// teamsPayload is the top-level Teams message structure.
type teamsPayload struct {
	Type        string             `json:"type"`
	Attachments []teamsAttachment  `json:"attachments"`
}

// teamsAttachment wraps the Adaptive Card.
type teamsAttachment struct {
	ContentType string      `json:"contentType"`
	Content     teamsCard   `json:"content"`
}

// teamsCard represents an Adaptive Card.
type teamsCard struct {
	Type    string      `json:"type"`
	Version string      `json:"version"`
	Body    []teamsItem `json:"body"`
}

// teamsItem represents an item in the Adaptive Card body.
type teamsItem struct {
	Type   string       `json:"type"`
	Text   string       `json:"text,omitempty"`
	Weight string       `json:"weight,omitempty"`
	Size   string       `json:"size,omitempty"`
	Color  string       `json:"color,omitempty"`
	Wrap   bool         `json:"wrap,omitempty"`
	Facts  []teamsFact  `json:"facts,omitempty"`
}

// teamsFact represents a key-value pair in a FactSet.
type teamsFact struct {
	Title string `json:"title"`
	Value string `json:"value"`
}

func (t *TeamsAlerter) buildPayload(event Event) teamsPayload {
	color := severityTeamsColor(event.Severity)
	emoji := severityEmoji(event.Severity)

	var body []teamsItem

	// Header
	body = append(body, teamsItem{
		Type:   "TextBlock",
		Text:   fmt.Sprintf("%s **%s** | `%s`", emoji, event.EventType, event.Severity),
		Weight: "Bolder",
		Size:   "Medium",
		Color:  color,
	})

	// Asset details
	facts := []teamsFact{
		{Title: "Asset:", Value: event.Asset.Name},
		{Title: "Type:", Value: event.Asset.Type},
		{Title: "ID:", Value: event.Asset.ID},
	}
	if event.Asset.DaysRemaining > 0 {
		facts = append(facts, teamsFact{
			Title: "Expires in:",
			Value: fmt.Sprintf("%d days", event.Asset.DaysRemaining),
		})
	} else if event.Asset.ExpiresAt != "" {
		facts = append(facts, teamsFact{
			Title: "Expires at:",
			Value: event.Asset.ExpiresAt,
		})
	}
	body = append(body, teamsItem{
		Type:  "FactSet",
		Facts: facts,
	})

	// Message body
	body = append(body, teamsItem{
		Type: "TextBlock",
		Text: event.Message,
		Wrap: true,
	})

	// Impact (optional)
	if event.Impact != nil {
		var parts []string
		parts = append(parts, fmt.Sprintf("**Blast radius:** %d affected", event.Impact.AffectedCount))
		if len(event.Impact.AffectedServices) > 0 {
			parts = append(parts, fmt.Sprintf("**Services:** %s", strings.Join(event.Impact.AffectedServices, ", ")))
		}
		body = append(body, teamsItem{
			Type: "TextBlock",
			Text: strings.Join(parts, "\n\n"),
			Wrap: true,
		})
	}

	// Footer
	body = append(body, teamsItem{
		Type: "TextBlock",
		Text: fmt.Sprintf("Source: **%s** | %s", event.Source, event.Timestamp.Format(time.RFC3339)),
		Size: "Small",
		Wrap: true,
	})

	return teamsPayload{
		Type: "message",
		Attachments: []teamsAttachment{
			{
				ContentType: "application/vnd.microsoft.card.adaptive",
				Content: teamsCard{
					Type:    "AdaptiveCard",
					Version: "1.4",
					Body:    body,
				},
			},
		},
	}
}

// severityTeamsColor maps severity levels to Adaptive Card colors.
func severityTeamsColor(severity string) string {
	switch strings.ToLower(severity) {
	case "critical", "expired":
		return "Attention"
	case "warning":
		return "Warning"
	case "ok":
		return "Good"
	default:
		return "Default"
	}
}
