package openai

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	aiport "github.com/crypticani/torvix/internal/ports/ai"
)

func TestLiveStructuredEnrichment(t *testing.T) {
	if os.Getenv("TORVIX_LIVE_OPENAI") != "1" {
		t.Skip("set TORVIX_LIVE_OPENAI=1 to run the live OpenAI smoke test")
	}
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Fatal("OPENAI_API_KEY is required for the live smoke test")
	}
	model := os.Getenv("TORVIX_AI_MODEL")
	if model == "" {
		model = "gpt-5.4-mini"
	}
	baseURL := os.Getenv("OPENAI_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	timeout := 45 * time.Second
	client := New(Config{APIKey: apiKey, BaseURL: baseURL, Model: model}, &http.Client{Timeout: timeout})
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	result, err := client.Generate(ctx, aiport.Request{
		Kind: "cost_anomaly",
		Context: map[string]any{
			"provider":         "aws",
			"service":          "EC2",
			"region":           "us-east-1",
			"currency":         "USD",
			"observed_cost":    150,
			"expected_cost":    100,
			"percentage_delta": 50,
			"direction":        "increase",
			"severity":         "high",
		},
	})
	if err != nil {
		t.Fatalf("live Generate() error = %v", err)
	}
	if result.Summary == "" || result.BusinessImpact == "" || len(result.RecommendedActions) == 0 {
		t.Fatalf("incomplete live enrichment: %+v", result)
	}
}
