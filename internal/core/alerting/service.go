package alerting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/smtp"
	"net/url"
	"strings"
	"time"

	"github.com/crypticani/torvix/internal/config"
	"github.com/crypticani/torvix/internal/domain"
)

type smtpSender func(addr string, auth smtp.Auth, from string, to []string, msg []byte) error

const maxNotifiedAnomalies = 5

type Service struct {
	client   *http.Client
	webhooks []config.Webhook
	sendMail smtpSender
	logger   *slog.Logger
}

type Notification struct {
	Title    string
	Severity string
	Message  string
	Fields   []NotificationField
}

type NotificationField struct {
	Name  string
	Value string
}

func New(client *http.Client, webhooks []config.Webhook) *Service {
	return NewWithLogger(client, webhooks, slog.Default())
}

func NewWithLogger(client *http.Client, webhooks []config.Webhook, logger *slog.Logger) *Service {
	if client == nil {
		client = http.DefaultClient
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{client: client, webhooks: webhooks, sendMail: smtp.SendMail, logger: logger}
}

func (s *Service) SendReport(ctx context.Context, report domain.Report) error {
	for _, target := range s.webhooks {
		if !target.Enabled {
			continue
		}
		if err := s.sendReport(ctx, target, report); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ReportDestinations() []string {
	destinations := make([]string, 0, len(s.webhooks))
	seen := make(map[string]bool, len(s.webhooks))
	for i, target := range s.webhooks {
		if !target.Enabled {
			continue
		}
		destination := reportDestination(target, i)
		if seen[destination] {
			continue
		}
		destinations = append(destinations, destination)
		seen[destination] = true
	}
	return destinations
}

func (s *Service) SendReportToDestination(ctx context.Context, destination string, report domain.Report) error {
	for i, target := range s.webhooks {
		if !target.Enabled {
			continue
		}
		if reportDestination(target, i) != destination {
			continue
		}
		return s.sendReport(ctx, target, report)
	}
	return fmt.Errorf("report destination %q is not configured", destination)
}

func (s *Service) SendNotification(ctx context.Context, notification Notification) error {
	for _, target := range s.webhooks {
		if !target.Enabled {
			continue
		}
		if err := s.sendNotification(ctx, target, notification); err != nil {
			return err
		}
	}
	return nil
}

func reportDestination(target config.Webhook, index int) string {
	if target.Name != "" {
		return target.Name
	}
	if target.Type != "" {
		return fmt.Sprintf("%s-%d", strings.ToLower(target.Type), index+1)
	}
	return fmt.Sprintf("destination-%d", index+1)
}

func (s *Service) sendReport(ctx context.Context, target config.Webhook, report domain.Report) error {
	switch strings.ToLower(target.Type) {
	case "slack":
		return s.postJSON(ctx, target, target.URL, formatSlack(target, report))
	case "discord":
		return s.postJSON(ctx, target, target.URL, formatDiscord(target, report))
	case "teams", "msteams", "microsoft_teams":
		return s.postJSON(ctx, target, target.URL, formatTeams(target, report))
	case "telegram":
		endpoint, body, err := formatTelegram(target, report)
		if err != nil {
			return err
		}
		return s.postJSON(ctx, target, endpoint, body)
	case "email", "smtp":
		return s.sendEmail(target, report)
	default:
		return fmt.Errorf("%s notifier type %q is not supported", target.Name, target.Type)
	}
}

func (s *Service) sendNotification(ctx context.Context, target config.Webhook, notification Notification) error {
	switch strings.ToLower(target.Type) {
	case "slack":
		return s.postJSON(ctx, target, target.URL, formatSlackNotification(notification))
	case "discord":
		return s.postJSON(ctx, target, target.URL, formatDiscordNotification(notification))
	case "teams", "msteams", "microsoft_teams":
		return s.postJSON(ctx, target, target.URL, formatTeamsNotification(notification))
	case "telegram":
		endpoint, body, err := formatTelegramNotification(target, notification)
		if err != nil {
			return err
		}
		return s.postJSON(ctx, target, endpoint, body)
	case "email", "smtp":
		return s.sendNotificationEmail(target, notification)
	default:
		return fmt.Errorf("%s notifier type %q is not supported", target.Name, target.Type)
	}
}

func (s *Service) postJSON(ctx context.Context, target config.Webhook, endpoint string, payload any) error {
	if endpoint == "" {
		return fmt.Errorf("%s notifier url is required", target.Name)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("%s notifier payload: %w", target.Name, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%s notifier request: %w", target.Name, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s notifier post: %w", target.Name, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("%s notifier status: %s", target.Name, resp.Status)
	}
	s.logger.Info("alert webhook delivered", "target", target.Name, "type", target.Type)
	return nil
}

func (s *Service) sendNotificationEmail(target config.Webhook, notification Notification) error {
	if target.SMTPHost == "" {
		return fmt.Errorf("%s email smtp_host is required", target.Name)
	}
	if target.From == "" {
		return fmt.Errorf("%s email from is required", target.Name)
	}
	if len(target.To) == 0 {
		return fmt.Errorf("%s email to is required", target.Name)
	}
	port := target.SMTPPort
	if port == 0 {
		port = 587
	}
	addr := fmt.Sprintf("%s:%d", target.SMTPHost, port)
	var auth smtp.Auth
	if target.Username != "" || target.Password != "" {
		auth = smtp.PlainAuth("", target.Username, target.Password, target.SMTPHost)
	}

	msg := formatNotificationEmail(target, notification)
	if err := s.sendMail(addr, auth, target.From, target.To, msg); err != nil {
		return fmt.Errorf("%s email send: %w", target.Name, err)
	}
	s.logger.Info("alert notification delivered", "target", target.Name, "type", target.Type, "recipients", len(target.To))
	return nil
}

func (s *Service) sendEmail(target config.Webhook, report domain.Report) error {
	if target.SMTPHost == "" {
		return fmt.Errorf("%s email smtp_host is required", target.Name)
	}
	if target.From == "" {
		return fmt.Errorf("%s email from is required", target.Name)
	}
	if len(target.To) == 0 {
		return fmt.Errorf("%s email to is required", target.Name)
	}
	port := target.SMTPPort
	if port == 0 {
		port = 587
	}
	addr := fmt.Sprintf("%s:%d", target.SMTPHost, port)
	var auth smtp.Auth
	if target.Username != "" || target.Password != "" {
		auth = smtp.PlainAuth("", target.Username, target.Password, target.SMTPHost)
	}

	msg := formatEmail(target, report)
	if err := s.sendMail(addr, auth, target.From, target.To, msg); err != nil {
		return fmt.Errorf("%s email send: %w", target.Name, err)
	}
	s.logger.Info("alert report delivered", "target", target.Name, "type", target.Type, "recipients", len(target.To))
	return nil
}

func formatSlackNotification(notification Notification) any {
	blocks := []map[string]any{
		{
			"type": "header",
			"text": map[string]string{"type": "plain_text", "text": notification.Title},
		},
		{
			"type": "section",
			"text": map[string]string{"type": "mrkdwn", "text": notificationText(notification)},
		},
	}
	return map[string]any{"blocks": blocks}
}

func formatDiscordNotification(notification Notification) any {
	fields := make([]map[string]any, 0, len(notification.Fields))
	for _, field := range notification.Fields {
		fields = append(fields, map[string]any{"name": field.Name, "value": field.Value, "inline": true})
	}
	return map[string]any{
		"embeds": []map[string]any{{
			"title":       notification.Title,
			"description": notification.Message,
			"color":       notificationColor(notification.Severity),
			"fields":      fields,
		}},
	}
}

func formatTeamsNotification(notification Notification) any {
	facts := make([]map[string]string, 0, len(notification.Fields))
	for _, field := range notification.Fields {
		facts = append(facts, map[string]string{"name": field.Name, "value": field.Value})
	}
	return map[string]any{
		"@type":      "MessageCard",
		"@context":   "https://schema.org/extensions",
		"summary":    notification.Title,
		"themeColor": notificationColorHex(notification.Severity),
		"title":      notification.Title,
		"text":       notification.Message,
		"sections":   []map[string]any{{"facts": facts}},
	}
}

func formatTelegramNotification(target config.Webhook, notification Notification) (string, any, error) {
	endpoint := target.URL
	if endpoint == "" {
		if target.BotToken == "" {
			return "", nil, fmt.Errorf("%s telegram bot_token or url is required", target.Name)
		}
		endpoint = "https://api.telegram.org/bot" + url.PathEscape(target.BotToken) + "/sendMessage"
	}
	if target.ChatID == "" {
		return "", nil, fmt.Errorf("%s telegram chat_id is required", target.Name)
	}
	body := map[string]any{
		"chat_id": target.ChatID,
		"text":    notification.Title + "\n" + notificationText(notification),
	}
	if target.ParseMode != "" {
		body["parse_mode"] = target.ParseMode
	}
	return endpoint, body, nil
}

func formatNotificationEmail(target config.Webhook, notification Notification) []byte {
	subject := notification.Title
	if target.SubjectPrefix != "" {
		subject = strings.TrimSpace(target.SubjectPrefix) + " " + subject
	}
	headers := []string{
		"From: " + target.From,
		"To: " + strings.Join(target.To, ", "),
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
	}
	return []byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + notification.Title + "\n\n" + notificationText(notification))
}

func formatSlack(target config.Webhook, report domain.Report) any {
	summary := summarize(target, report)
	blocks := []map[string]any{
		{
			"type": "header",
			"text": map[string]string{"type": "plain_text", "text": summary.Title},
		},
		{
			"type": "section",
			"text": map[string]string{
				"type": "mrkdwn",
				"text": fmt.Sprintf("*Period:* %s\n*Total Cost:* %s\n*Anomalies Detected:* %d", summary.PeriodRange, summary.TotalCost, summary.AnomalyCount),
			},
		},
	}
	if summary.CostIncreaseText != "" {
		blocks = append(blocks, map[string]any{
			"type": "section",
			"text": map[string]string{"type": "mrkdwn", "text": "*Top Cost Increases:*\n" + summary.CostIncreaseText},
		})
	}
	if summary.CostDecreaseText != "" {
		blocks = append(blocks, map[string]any{
			"type": "section",
			"text": map[string]string{"type": "mrkdwn", "text": "*Top Cost Decreases:*\n" + summary.CostDecreaseText},
		})
	}
	if summary.AnomalyText != "" {
		blocks = append(blocks, map[string]any{
			"type": "section",
			"text": map[string]string{"type": "mrkdwn", "text": "*Top Anomalies:*\n" + summary.AnomalyText},
		})
	}
	return map[string]any{"blocks": blocks}
}

func formatDiscord(target config.Webhook, report domain.Report) any {
	summary := summarize(target, report)
	fields := []map[string]any{
		{"name": "Period", "value": summary.PeriodRange, "inline": false},
		{"name": "Total Cost", "value": summary.TotalCost, "inline": true},
		{"name": "Anomalies Detected", "value": fmt.Sprintf("%d", summary.AnomalyCount), "inline": true},
	}
	if summary.CostIncreaseText != "" {
		fields = append(fields, map[string]any{"name": "Top Cost Increases", "value": summary.CostIncreaseText})
	}
	if summary.CostDecreaseText != "" {
		fields = append(fields, map[string]any{"name": "Top Cost Decreases", "value": summary.CostDecreaseText})
	}
	if summary.AnomalyText != "" {
		fields = append(fields, map[string]any{"name": "Top Anomalies", "value": summary.AnomalyText})
	}
	color := 3447003
	if summary.AnomalyCount > 0 {
		color = 15158332
	}
	return map[string]any{
		"embeds": []map[string]any{{
			"title":  summary.Title,
			"color":  color,
			"fields": fields,
		}},
	}
}

func formatTeams(target config.Webhook, report domain.Report) any {
	summary := summarize(target, report)
	section := map[string]any{
		"facts": []map[string]string{
			{"name": "Period", "value": summary.PeriodRange},
			{"name": "Total Cost", "value": summary.TotalCost},
			{"name": "Anomalies Detected", "value": fmt.Sprintf("%d", summary.AnomalyCount)},
		},
	}
	var bodySections []string
	if summary.CostIncreaseText != "" {
		bodySections = append(bodySections, "Top cost increases:\n\n"+summary.CostIncreaseText)
	}
	if summary.CostDecreaseText != "" {
		bodySections = append(bodySections, "Top cost decreases:\n\n"+summary.CostDecreaseText)
	}
	if summary.AnomalyText != "" {
		bodySections = append(bodySections, "Top anomalies:\n\n"+summary.AnomalyText)
	}
	if len(bodySections) > 0 {
		section["text"] = strings.Join(bodySections, "\n\n")
	}
	return map[string]any{
		"@type":      "MessageCard",
		"@context":   "https://schema.org/extensions",
		"summary":    summary.Title,
		"themeColor": teamsColor(summary.AnomalyCount),
		"title":      summary.Title,
		"sections":   []map[string]any{section},
	}
}

func formatTelegram(target config.Webhook, report domain.Report) (string, any, error) {
	endpoint := target.URL
	if endpoint == "" {
		if target.BotToken == "" {
			return "", nil, fmt.Errorf("%s telegram bot_token or url is required", target.Name)
		}
		endpoint = "https://api.telegram.org/bot" + url.PathEscape(target.BotToken) + "/sendMessage"
	}
	if target.ChatID == "" {
		return "", nil, fmt.Errorf("%s telegram chat_id is required", target.Name)
	}
	summary := summarize(target, report)
	text := summary.Title + "\n" +
		"Period: " + summary.PeriodRange + "\n" +
		"Total Cost: " + summary.TotalCost + "\n" +
		fmt.Sprintf("Anomalies Detected: %d", summary.AnomalyCount)
	if summary.CostIncreaseText != "" {
		text += "\n\nTop Cost Increases:\n" + summary.CostIncreaseText
	}
	if summary.CostDecreaseText != "" {
		text += "\n\nTop Cost Decreases:\n" + summary.CostDecreaseText
	}
	if summary.AnomalyText != "" {
		text += "\n\nTop Anomalies:\n" + summary.AnomalyText
	}
	body := map[string]any{
		"chat_id": target.ChatID,
		"text":    text,
	}
	if target.ParseMode != "" {
		body["parse_mode"] = target.ParseMode
	}
	return endpoint, body, nil
}

func formatEmail(target config.Webhook, report domain.Report) []byte {
	summary := summarize(target, report)
	subject := summary.Title
	if target.SubjectPrefix != "" {
		subject = strings.TrimSpace(target.SubjectPrefix) + " " + subject
	}
	body := summary.Title + "\n\n" +
		"Period: " + summary.PeriodRange + "\n" +
		"Total Cost: " + summary.TotalCost + "\n" +
		fmt.Sprintf("Anomalies Detected: %d\n", summary.AnomalyCount)
	if summary.CostIncreaseText != "" {
		body += "\nTop Cost Increases:\n" + summary.CostIncreaseText
	}
	if summary.CostDecreaseText != "" {
		body += "\nTop Cost Decreases:\n" + summary.CostDecreaseText
	}
	if summary.AnomalyText != "" {
		body += "\nTop Anomalies:\n" + summary.AnomalyText
	}
	headers := []string{
		"From: " + target.From,
		"To: " + strings.Join(target.To, ", "),
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
	}
	return []byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + body)
}

type reportSummary struct {
	Title            string
	PeriodRange      string
	TotalCost        string
	AnomalyCount     int
	CostIncreaseText string
	CostDecreaseText string
	AnomalyText      string
}

func summarize(target config.Webhook, report domain.Report) reportSummary {
	var total float64
	for _, s := range report.Summary {
		total += s.TotalCost
	}
	return reportSummary{
		Title:            fmt.Sprintf("Torvix %s Report", title(report.Period)),
		PeriodRange:      reportPeriodRange(report),
		TotalCost:        formatCost(target.Currency, total),
		AnomalyCount:     len(report.Anomalies),
		CostIncreaseText: formatCostVariances(target.Currency, report.CostIncreases),
		CostDecreaseText: formatCostVariances(target.Currency, report.CostDecreases),
		AnomalyText:      formatAnomalies(target.Currency, report.Anomalies),
	}
}

func reportPeriodRange(report domain.Report) string {
	from := report.From.UTC()
	to := report.To.UTC()
	if from.IsZero() || to.IsZero() || !to.After(from) {
		return title(report.Period)
	}
	if isMidnightUTC(from) && isMidnightUTC(to) {
		end := to.AddDate(0, 0, -1)
		if sameUTCDate(from, end) {
			return from.Format("2006-01-02") + " UTC"
		}
		return from.Format("2006-01-02") + " to " + end.Format("2006-01-02") + " UTC"
	}
	return from.Format(time.RFC3339) + " to " + to.Format(time.RFC3339)
}

func isMidnightUTC(t time.Time) bool {
	return t.Location() == time.UTC && t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 && t.Nanosecond() == 0
}

func sameUTCDate(a, b time.Time) bool {
	return a.Year() == b.Year() && a.Month() == b.Month() && a.Day() == b.Day()
}

func formatCostVariances(currency string, variances []domain.CostVariance) string {
	if len(variances) == 0 {
		return ""
	}
	var b strings.Builder
	count := len(variances)
	if count > maxNotifiedAnomalies {
		count = maxNotifiedAnomalies
	}
	for i := 0; i < count; i++ {
		v := variances[i]
		details := varianceDetails(v)
		if details != "" {
			details = "; " + details
		}
		b.WriteString(fmt.Sprintf("- %s %s: %s (%.1f%%; current %s vs previous %s%s)\n", v.Provider, v.Service, formatSignedCost(currency, v.Delta), v.PercentChange, formatCost(currency, v.CurrentCost), formatCost(currency, v.PreviousCost), details))
	}
	if len(variances) > count {
		b.WriteString(fmt.Sprintf("...and %d more\n", len(variances)-count))
	}
	return b.String()
}

func formatSignedCost(currency string, value float64) string {
	if value >= 0 {
		return "+" + formatCost(currency, value)
	}
	return "-" + formatCost(currency, -value)
}

func varianceDetails(v domain.CostVariance) string {
	parts := make([]string, 0, 2)
	if v.CompartmentName != "" {
		parts = append(parts, "compartment "+v.CompartmentName)
	} else if v.CompartmentID != "" {
		parts = append(parts, "compartment "+v.CompartmentID)
	}
	if v.AccountID != "" {
		parts = append(parts, "account "+v.AccountID)
	}
	return strings.Join(parts, "; ")
}

func formatAnomalies(currency string, anomalies []domain.Anomaly) string {
	if len(anomalies) == 0 {
		return ""
	}
	var b strings.Builder
	count := len(anomalies)
	if count > maxNotifiedAnomalies {
		count = maxNotifiedAnomalies
	}
	for i := 0; i < count; i++ {
		a := anomalies[i]
		details := anomalyDetails(a)
		if details != "" {
			details = "; " + details
		}
		b.WriteString(fmt.Sprintf("- [%s] %s %s: %s (%.1f%% deviation%s)\n", a.Severity, a.Provider, a.Service, formatCost(currency, a.Actual), a.PercentDeviation, details))
	}
	if len(anomalies) > count {
		b.WriteString(fmt.Sprintf("...and %d more\n", len(anomalies)-count))
	}
	return b.String()
}

func anomalyDetails(a domain.Anomaly) string {
	parts := make([]string, 0, 4)
	if a.Category != "" {
		parts = append(parts, "category "+a.Category)
	}
	if a.CompartmentName != "" {
		parts = append(parts, "compartment "+a.CompartmentName)
	} else if a.CompartmentID != "" {
		parts = append(parts, "compartment "+a.CompartmentID)
	}
	if a.Region != "" {
		parts = append(parts, "region "+a.Region)
	}
	if a.AccountID != "" {
		parts = append(parts, "account "+a.AccountID)
	}
	return strings.Join(parts, "; ")
}

func formatCost(currency string, amount float64) string {
	if currency == "" {
		return fmt.Sprintf("%.2f", amount)
	}
	return fmt.Sprintf("%s %.2f", strings.ToUpper(currency), amount)
}

func notificationText(notification Notification) string {
	var b strings.Builder
	if notification.Message != "" {
		b.WriteString(notification.Message)
	}
	for _, field := range notification.Fields {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(field.Name)
		b.WriteString(": ")
		b.WriteString(field.Value)
	}
	return b.String()
}

func notificationColor(severity string) int {
	switch strings.ToLower(severity) {
	case "error", "failed", "failure":
		return 15158332
	case "warning", "partial_failure":
		return 16776960
	default:
		return 3066993
	}
}

func notificationColorHex(severity string) string {
	switch strings.ToLower(severity) {
	case "error", "failed", "failure":
		return "E81123"
	case "warning", "partial_failure":
		return "FFCC00"
	default:
		return "2ECC71"
	}
}

func teamsColor(anomalyCount int) string {
	if anomalyCount > 0 {
		return "E81123"
	}
	return "0076D7"
}

func title(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
