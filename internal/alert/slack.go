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

// SlackAlerter sends events to a Slack incoming webhook using Block Kit formatting.
type SlackAlerter struct {
	webhookURL string
	channel    string
	client     *http.Client
}

// NewSlackAlerter creates a new Slack alerter that posts to the given webhook URL.
// An optional channel overrides the webhook's default channel.
func NewSlackAlerter(webhookURL, channel string) *SlackAlerter {
	return &SlackAlerter{
		webhookURL: webhookURL,
		channel:    channel,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Name returns "slack".
func (s *SlackAlerter) Name() string {
	return "slack"
}

// Send formats the event as a Slack Block Kit message and posts it to the webhook.
func (s *SlackAlerter) Send(ctx context.Context, event Event) error {
	payload := s.buildPayload(event)

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling slack payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req) //#nosec G704 -- URL is from trusted config, not user input
	if err != nil {
		return fmt.Errorf("sending slack webhook: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort cleanup

	// Drain body to enable HTTP connection reuse.
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("slack webhook returned status %d", resp.StatusCode)
	}

	return nil
}

// slackPayload is the top-level Slack message structure.
type slackPayload struct {
	Text        string            `json:"text"`
	Channel     string            `json:"channel,omitempty"`
	Attachments []slackAttachment `json:"attachments"`
}

// slackAttachment wraps blocks with a color sidebar.
type slackAttachment struct {
	Color  string       `json:"color"`
	Blocks []slackBlock `json:"blocks"`
}

// slackBlock represents a Slack Block Kit block.
type slackBlock struct {
	Type   string       `json:"type"`
	Text   *slackText   `json:"text,omitempty"`
	Fields []slackText  `json:"fields,omitempty"`
	Elements []slackText `json:"elements,omitempty"`
}

// slackText is a text object used in blocks.
type slackText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (s *SlackAlerter) buildPayload(event Event) slackPayload {
	emoji := severityEmoji(event.Severity)
	fallback := fmt.Sprintf("[%s] %s %s — %s", event.Severity, event.EventType, event.Asset.Name, event.Message)

	var blocks []slackBlock

	// Header
	blocks = append(blocks, slackBlock{
		Type: "section",
		Text: &slackText{
			Type: "mrkdwn",
			Text: fmt.Sprintf("%s *%s* | `%s`", emoji, event.EventType, event.Severity),
		},
	})

	// Asset details
	fields := []slackText{
		{Type: "mrkdwn", Text: fmt.Sprintf("*Asset:*\n%s", event.Asset.Name)},
		{Type: "mrkdwn", Text: fmt.Sprintf("*Type:*\n%s", event.Asset.Type)},
		{Type: "mrkdwn", Text: fmt.Sprintf("*ID:*\n%s", event.Asset.ID)},
	}
	if event.Asset.DaysRemaining > 0 {
		fields = append(fields, slackText{
			Type: "mrkdwn",
			Text: fmt.Sprintf("*Expires in:*\n%d days", event.Asset.DaysRemaining),
		})
	} else if event.Asset.ExpiresAt != "" {
		fields = append(fields, slackText{
			Type: "mrkdwn",
			Text: fmt.Sprintf("*Expires at:*\n%s", event.Asset.ExpiresAt),
		})
	}
	blocks = append(blocks, slackBlock{
		Type:   "section",
		Fields: fields,
	})

	// Message body
	blocks = append(blocks, slackBlock{
		Type: "section",
		Text: &slackText{
			Type: "mrkdwn",
			Text: event.Message,
		},
	})

	// Impact (optional)
	if event.Impact != nil {
		var parts []string
		parts = append(parts, fmt.Sprintf("*Blast radius:* %d affected", event.Impact.AffectedCount))
		if len(event.Impact.AffectedServices) > 0 {
			parts = append(parts, fmt.Sprintf("*Services:* %s", strings.Join(event.Impact.AffectedServices, ", ")))
		}
		blocks = append(blocks, slackBlock{
			Type: "section",
			Text: &slackText{
				Type: "mrkdwn",
				Text: strings.Join(parts, "\n"),
			},
		})
	}

	// Footer
	blocks = append(blocks, slackBlock{
		Type: "context",
		Elements: []slackText{
			{
				Type: "mrkdwn",
				Text: fmt.Sprintf("Source: *%s* | %s", event.Source, event.Timestamp.Format(time.RFC3339)),
			},
		},
	})

	payload := slackPayload{
		Text: fallback,
		Attachments: []slackAttachment{
			{
				Color:  severityColor(event.Severity),
				Blocks: blocks,
			},
		},
	}

	if s.channel != "" {
		payload.Channel = s.channel
	}

	return payload
}

// severityColor maps severity levels to Slack color hex codes.
func severityColor(severity string) string {
	switch strings.ToLower(severity) {
	case "critical", "expired":
		return "#E01E5A"
	case "warning":
		return "#ECB22E"
	case "ok":
		return "#2EB886"
	default:
		return "#CCCCCC"
	}
}

// severityEmoji maps severity levels to Slack emoji.
func severityEmoji(severity string) string {
	switch strings.ToLower(severity) {
	case "critical", "expired":
		return ":red_circle:"
	case "warning":
		return ":warning:"
	case "ok":
		return ":large_green_circle:"
	default:
		return ":white_circle:"
	}
}
