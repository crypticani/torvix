package alerting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/crypticani/cloudpulse/internal/config"
	"github.com/crypticani/cloudpulse/internal/domain"
)

type Service struct {
	client   *http.Client
	webhooks []config.Webhook
}

func New(client *http.Client, webhooks []config.Webhook) *Service {
	return &Service{client: client, webhooks: webhooks}
}

func (s *Service) SendReport(ctx context.Context, report domain.Report) error {
	rawBody, err := json.Marshal(report)
	if err != nil {
		return err
	}

	for _, wh := range s.webhooks {
		if !wh.Enabled {
			continue
		}

		var body []byte
		switch wh.Type {
		case "slack":
			body, _ = formatSlack(report)
		case "discord":
			body, _ = formatDiscord(report)
		default:
			body = rawBody
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, wh.URL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		
		resp, err := s.client.Do(req)
		if err != nil {
			return fmt.Errorf("%s webhook: %w", wh.Name, err)
		}
		resp.Body.Close()
		
		if resp.StatusCode >= 300 {
			return fmt.Errorf("%s webhook status: %s", wh.Name, resp.Status)
		}
	}
	return nil
}

func formatSlack(report domain.Report) ([]byte, error) {
	var total float64
	for _, s := range report.Summary {
		total += s.TotalCost
	}

	blocks := []map[string]any{
		{
			"type": "header",
			"text": map[string]string{
				"type": "plain_text",
				"text": fmt.Sprintf("CloudPulse %s Report", title(report.Period)),
			},
		},
		{
			"type": "section",
			"text": map[string]string{
				"type": "mrkdwn",
				"text": fmt.Sprintf("*Total Cost:* $%.2f\n*Anomalies Detected:* %d", total, len(report.Anomalies)),
			},
		},
	}

	if len(report.Anomalies) > 0 {
		var anomalyText string
		// Limit to top 10 anomalies
		count := len(report.Anomalies)
		if count > 10 {
			count = 10
		}
		for i := 0; i < count; i++ {
			a := report.Anomalies[i]
			anomalyText += fmt.Sprintf("• [%s] %s %s: $%.2f (%.1f%% deviation)\n", a.Severity, a.Provider, a.Service, a.Actual, a.PercentDeviation)
		}
		if len(report.Anomalies) > 10 {
			anomalyText += fmt.Sprintf("...and %d more\n", len(report.Anomalies)-10)
		}
		blocks = append(blocks, map[string]any{
			"type": "section",
			"text": map[string]string{
				"type": "mrkdwn",
				"text": "*Top Anomalies:*\n" + anomalyText,
			},
		})
	}

	return json.Marshal(map[string]any{"blocks": blocks})
}

func formatDiscord(report domain.Report) ([]byte, error) {
	var total float64
	for _, s := range report.Summary {
		total += s.TotalCost
	}

	embed := map[string]any{
		"title": fmt.Sprintf("CloudPulse %s Report", title(report.Period)),
		"color": 3447003, // blue
		"fields": []map[string]any{
			{
				"name":   "Total Cost",
				"value":  fmt.Sprintf("$%.2f", total),
				"inline": true,
			},
			{
				"name":   "Anomalies Detected",
				"value":  fmt.Sprintf("%d", len(report.Anomalies)),
				"inline": true,
			},
		},
	}

	if len(report.Anomalies) > 0 {
		var anomalyText string
		count := len(report.Anomalies)
		if count > 10 {
			count = 10
		}
		for i := 0; i < count; i++ {
			a := report.Anomalies[i]
			anomalyText += fmt.Sprintf("• [%s] %s %s: $%.2f (%.1f%%)\n", a.Severity, a.Provider, a.Service, a.Actual, a.PercentDeviation)
		}
		if len(report.Anomalies) > 10 {
			anomalyText += fmt.Sprintf("...and %d more\n", len(report.Anomalies)-10)
		}
		embed["fields"] = append(embed["fields"].([]map[string]any), map[string]any{
			"name":  "Top Anomalies",
			"value": anomalyText,
		})
		embed["color"] = 15158332 // red
	}

	return json.Marshal(map[string]any{"embeds": []map[string]any{embed}})
}

func title(s string) string {
	if s == "" {
		return ""
	}
	return string(bytes.ToUpper([]byte{s[0]})) + s[1:]
}
