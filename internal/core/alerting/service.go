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
	body, err := json.Marshal(report)
	if err != nil {
		return err
	}
	for _, wh := range s.webhooks {
		if !wh.Enabled {
			continue
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
