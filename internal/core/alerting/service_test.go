package alerting

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/smtp"
	"strings"
	"testing"
	"time"

	"github.com/crypticani/cloudpulse/internal/config"
	"github.com/crypticani/cloudpulse/internal/domain"
)

func TestSendReportHTTPNotifiers(t *testing.T) {
	tests := []struct {
		name       string
		targetType string
		configure  func(*config.Webhook)
		assert     func(t *testing.T, body map[string]any)
	}{
		{
			name:       "slack",
			targetType: "slack",
			assert: func(t *testing.T, body map[string]any) {
				if _, ok := body["blocks"].([]any); !ok {
					t.Fatalf("expected slack blocks, got %#v", body)
				}
			},
		},
		{
			name:       "discord",
			targetType: "discord",
			assert: func(t *testing.T, body map[string]any) {
				if _, ok := body["embeds"].([]any); !ok {
					t.Fatalf("expected discord embeds, got %#v", body)
				}
			},
		},
		{
			name:       "teams",
			targetType: "teams",
			assert: func(t *testing.T, body map[string]any) {
				if body["@type"] != "MessageCard" {
					t.Fatalf("expected teams MessageCard, got %#v", body)
				}
			},
		},
		{
			name:       "telegram",
			targetType: "telegram",
			configure: func(target *config.Webhook) {
				target.ChatID = "12345"
			},
			assert: func(t *testing.T, body map[string]any) {
				if body["chat_id"] != "12345" {
					t.Fatalf("expected telegram chat id, got %#v", body)
				}
				if !strings.Contains(body["text"].(string), "CloudPulse Daily Report") {
					t.Fatalf("expected telegram report text, got %#v", body)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got map[string]any
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodPost {
					t.Fatalf("expected POST, got %s", r.Method)
				}
				if ct := r.Header.Get("Content-Type"); ct != "application/json" {
					t.Fatalf("expected json content type, got %s", ct)
				}
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				return &http.Response{
					StatusCode: http.StatusNoContent,
					Status:     "204 No Content",
					Body:       io.NopCloser(strings.NewReader("")),
					Header:     make(http.Header),
				}, nil
			})}

			target := config.Webhook{
				Name:     tt.name,
				Type:     tt.targetType,
				URL:      "https://example.test/webhook",
				Enabled:  true,
				Currency: "INR",
			}
			if tt.configure != nil {
				tt.configure(&target)
			}
			svc := New(client, []config.Webhook{target})
			if err := svc.SendReport(context.Background(), sampleReport()); err != nil {
				t.Fatalf("send report: %v", err)
			}
			tt.assert(t, got)
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestSendReportTelegramBuildsAPIURL(t *testing.T) {
	target := config.Webhook{Name: "telegram", Type: "telegram", Enabled: true, BotToken: "token:abc", ChatID: "12345"}
	endpoint, body, err := formatTelegram(target, sampleReport())
	if err != nil {
		t.Fatalf("format telegram: %v", err)
	}
	if !strings.Contains(endpoint, "https://api.telegram.org/bot") {
		t.Fatalf("expected telegram API endpoint, got %s", endpoint)
	}
	if body.(map[string]any)["chat_id"] != "12345" {
		t.Fatalf("expected chat id in payload")
	}
}

func TestSendNotificationHTTPNotifiers(t *testing.T) {
	tests := []struct {
		name       string
		targetType string
		configure  func(*config.Webhook)
		assert     func(t *testing.T, body map[string]any)
	}{
		{
			name:       "slack",
			targetType: "slack",
			assert: func(t *testing.T, body map[string]any) {
				if _, ok := body["blocks"].([]any); !ok {
					t.Fatalf("expected slack blocks, got %#v", body)
				}
			},
		},
		{
			name:       "discord",
			targetType: "discord",
			assert: func(t *testing.T, body map[string]any) {
				if _, ok := body["embeds"].([]any); !ok {
					t.Fatalf("expected discord embeds, got %#v", body)
				}
			},
		},
		{
			name:       "teams",
			targetType: "teams",
			assert: func(t *testing.T, body map[string]any) {
				if body["@type"] != "MessageCard" {
					t.Fatalf("expected teams MessageCard, got %#v", body)
				}
			},
		},
		{
			name:       "telegram",
			targetType: "telegram",
			configure: func(target *config.Webhook) {
				target.ChatID = "12345"
			},
			assert: func(t *testing.T, body map[string]any) {
				if body["chat_id"] != "12345" {
					t.Fatalf("expected telegram chat id, got %#v", body)
				}
				if !strings.Contains(body["text"].(string), "CloudPulse Ingestion Succeeded") {
					t.Fatalf("expected ingestion notification text, got %#v", body)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got map[string]any
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodPost {
					t.Fatalf("expected POST, got %s", r.Method)
				}
				if ct := r.Header.Get("Content-Type"); ct != "application/json" {
					t.Fatalf("expected json content type, got %s", ct)
				}
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				return &http.Response{
					StatusCode: http.StatusNoContent,
					Status:     "204 No Content",
					Body:       io.NopCloser(strings.NewReader("")),
					Header:     make(http.Header),
				}, nil
			})}

			target := config.Webhook{
				Name:    tt.name,
				Type:    tt.targetType,
				URL:     "https://example.test/webhook",
				Enabled: true,
			}
			if tt.configure != nil {
				tt.configure(&target)
			}
			svc := New(client, []config.Webhook{target})
			if err := svc.SendNotification(context.Background(), sampleNotification()); err != nil {
				t.Fatalf("send notification: %v", err)
			}
			tt.assert(t, got)
		})
	}
}

func TestSendReportEmail(t *testing.T) {
	var gotAddr, gotFrom string
	var gotTo []string
	var gotMsg []byte

	svc := New(http.DefaultClient, []config.Webhook{{
		Name:          "email",
		Type:          "email",
		Enabled:       true,
		Currency:      "USD",
		SMTPHost:      "smtp.example.test",
		SMTPPort:      2525,
		From:          "finops@example.test",
		To:            []string{"ops@example.test"},
		SubjectPrefix: "[CloudPulse]",
	}})
	svc.sendMail = func(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
		gotAddr = addr
		gotFrom = from
		gotTo = append([]string(nil), to...)
		gotMsg = append([]byte(nil), msg...)
		return nil
	}

	if err := svc.SendReport(context.Background(), sampleReport()); err != nil {
		t.Fatalf("send report: %v", err)
	}
	if gotAddr != "smtp.example.test:2525" {
		t.Fatalf("expected smtp addr, got %s", gotAddr)
	}
	if gotFrom != "finops@example.test" || len(gotTo) != 1 || gotTo[0] != "ops@example.test" {
		t.Fatalf("unexpected envelope from=%s to=%v", gotFrom, gotTo)
	}
	msg := string(gotMsg)
	if !strings.Contains(msg, "Subject: [CloudPulse] CloudPulse Daily Report") {
		t.Fatalf("expected subject in email, got %s", msg)
	}
	if !strings.Contains(msg, "Total Cost: USD 123.45") {
		t.Fatalf("expected USD total in email, got %s", msg)
	}
}

func TestSendNotificationEmail(t *testing.T) {
	var gotMsg []byte

	svc := New(http.DefaultClient, []config.Webhook{{
		Name:          "email",
		Type:          "email",
		Enabled:       true,
		SMTPHost:      "smtp.example.test",
		SMTPPort:      2525,
		From:          "finops@example.test",
		To:            []string{"ops@example.test"},
		SubjectPrefix: "[CloudPulse]",
	}})
	svc.sendMail = func(_ string, _ smtp.Auth, _ string, _ []string, msg []byte) error {
		gotMsg = append([]byte(nil), msg...)
		return nil
	}

	if err := svc.SendNotification(context.Background(), sampleNotification()); err != nil {
		t.Fatalf("send notification: %v", err)
	}
	msg := string(gotMsg)
	if !strings.Contains(msg, "Subject: [CloudPulse] CloudPulse Ingestion Succeeded") {
		t.Fatalf("expected subject in email, got %s", msg)
	}
	if !strings.Contains(msg, "Records inserted: 100") {
		t.Fatalf("expected notification field in email, got %s", msg)
	}
}

func TestSendReportUnsupportedNotifier(t *testing.T) {
	svc := New(http.DefaultClient, []config.Webhook{{Name: "bad", Type: "pager", Enabled: true}})
	if err := svc.SendReport(context.Background(), sampleReport()); err == nil {
		t.Fatal("expected unsupported notifier error")
	}
}

func sampleNotification() Notification {
	return Notification{
		Title:    "CloudPulse Ingestion Succeeded",
		Severity: "success",
		Message:  "Background ingestion finished with status success.",
		Fields: []NotificationField{
			{Name: "Records inserted", Value: "100"},
		},
	}
}

func sampleReport() domain.Report {
	return domain.Report{
		Period:    "daily",
		Generated: time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC),
		Summary: []domain.AggregatedCost{
			{Provider: domain.ProviderOCI, Service: "COMPUTE", TotalCost: 123.45},
		},
		Anomalies: []domain.Anomaly{
			{Provider: domain.ProviderOCI, Service: "COMPUTE", Actual: 90.12, PercentDeviation: 42.5, Severity: "high"},
		},
	}
}
