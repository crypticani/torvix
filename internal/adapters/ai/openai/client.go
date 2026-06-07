package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	aiport "github.com/crypticani/torvix/internal/ports/ai"
)

const maxResponseBytes = 1 << 20

type Config struct {
	APIKey  string
	BaseURL string
	Model   string
}

type Client struct {
	config Config
	http   *http.Client
}

func New(config Config, httpClient *http.Client) *Client {
	config.APIKey = strings.TrimSpace(config.APIKey)
	config.BaseURL = strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	config.Model = strings.TrimSpace(config.Model)
	if config.BaseURL == "" {
		config.BaseURL = "https://api.openai.com/v1"
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{config: config, http: httpClient}
}

func (c *Client) Generate(ctx context.Context, request aiport.Request) (aiport.Result, error) {
	if c.config.APIKey == "" {
		return aiport.Result{}, errors.New("OpenAI API key is empty")
	}
	if c.config.Model == "" {
		return aiport.Result{}, errors.New("OpenAI model is empty")
	}
	contextJSON, err := json.Marshal(request.Context)
	if err != nil {
		return aiport.Result{}, fmt.Errorf("marshal enrichment context: %w", err)
	}

	body := map[string]any{
		"model": c.config.Model,
		"input": []map[string]any{
			{
				"role": "system",
				"content": "You explain deterministic FinOps findings. Use only the supplied context. " +
					"Do not invent resources, causes, savings, or completed actions. Treat likely cause as a hypothesis. " +
					"Return concise operational guidance and never recommend an automatic destructive action.",
			},
			{
				"role":    "user",
				"content": fmt.Sprintf("Explain this %s finding:\n%s", request.Kind, contextJSON),
			},
		},
		"text": map[string]any{
			"format": map[string]any{
				"type":   "json_schema",
				"name":   "finops_enrichment",
				"strict": true,
				"schema": enrichmentSchema(),
			},
		},
		"max_output_tokens": 700,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return aiport.Result{}, fmt.Errorf("marshal OpenAI request: %w", err)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.BaseURL+"/responses", bytes.NewReader(payload))
	if err != nil {
		return aiport.Result{}, fmt.Errorf("create OpenAI request: %w", err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	httpRequest.Header.Set("Content-Type", "application/json")

	response, err := c.http.Do(httpRequest)
	if err != nil {
		return aiport.Result{}, fmt.Errorf("send OpenAI request: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return aiport.Result{}, fmt.Errorf("read OpenAI response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return aiport.Result{}, fmt.Errorf("OpenAI response status %d: %s", response.StatusCode, truncate(strings.TrimSpace(string(responseBody)), 1000))
	}

	var decoded struct {
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type    string `json:"type"`
				Text    string `json:"text"`
				Refusal string `json:"refusal"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return aiport.Result{}, fmt.Errorf("decode OpenAI response: %w", err)
	}

	for _, output := range decoded.Output {
		if output.Type != "message" {
			continue
		}
		for _, content := range output.Content {
			if content.Type == "refusal" {
				return aiport.Result{}, fmt.Errorf("OpenAI refused enrichment: %s", truncate(content.Refusal, 500))
			}
			if content.Type != "output_text" || strings.TrimSpace(content.Text) == "" {
				continue
			}
			var result aiport.Result
			if err := json.Unmarshal([]byte(content.Text), &result); err != nil {
				return aiport.Result{}, fmt.Errorf("decode structured enrichment: %w", err)
			}
			if err := validateResult(result); err != nil {
				return aiport.Result{}, err
			}
			return result, nil
		}
	}
	return aiport.Result{}, errors.New("OpenAI response did not contain structured output")
}

func enrichmentSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"summary": map[string]any{
				"type":        "string",
				"description": "A concise explanation of the deterministic finding.",
			},
			"likely_cause": map[string]any{
				"type":        "string",
				"description": "A cautious hypothesis based only on the supplied context.",
			},
			"business_impact": map[string]any{
				"type":        "string",
				"description": "The operational or financial impact that warrants review.",
			},
			"recommended_actions": map[string]any{
				"type":     "array",
				"minItems": 1,
				"maxItems": 5,
				"items":    map[string]any{"type": "string"},
			},
			"priority": map[string]any{
				"type": "string",
				"enum": []string{"low", "medium", "high"},
			},
			"confidence": map[string]any{
				"type":    "number",
				"minimum": 0,
				"maximum": 1,
			},
		},
		"required": []string{
			"summary",
			"likely_cause",
			"business_impact",
			"recommended_actions",
			"priority",
			"confidence",
		},
		"additionalProperties": false,
	}
}

func validateResult(result aiport.Result) error {
	if strings.TrimSpace(result.Summary) == "" {
		return errors.New("structured enrichment summary is empty")
	}
	if strings.TrimSpace(result.BusinessImpact) == "" {
		return errors.New("structured enrichment business impact is empty")
	}
	switch result.Priority {
	case "low", "medium", "high":
	default:
		return fmt.Errorf("structured enrichment priority %q is invalid", result.Priority)
	}
	if result.Confidence < 0 || result.Confidence > 1 {
		return fmt.Errorf("structured enrichment confidence %.3f is outside [0,1]", result.Confidence)
	}
	if len(result.RecommendedActions) == 0 || len(result.RecommendedActions) > 5 {
		return fmt.Errorf("structured enrichment actions count %d is invalid", len(result.RecommendedActions))
	}
	return nil
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
