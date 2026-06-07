package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	aiport "github.com/crypticani/torvix/internal/ports/ai"
)

func TestClientGenerateUsesStructuredResponsesOutput(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("unexpected authorization header %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		text, ok := body["text"].(map[string]any)
		if !ok {
			t.Fatalf("request missing text configuration: %+v", body)
		}
		format, ok := text["format"].(map[string]any)
		if !ok || format["type"] != "json_schema" || format["strict"] != true {
			t.Fatalf("request must use strict JSON schema output: %+v", format)
		}
		return jsonResponse(http.StatusOK, `{
			"id":"resp_test",
			"model":"gpt-5.4-mini",
			"output":[{
				"type":"message",
				"content":[{
					"type":"output_text",
					"text":"{\"summary\":\"Spend increased materially.\",\"likely_cause\":\"Compute usage changed.\",\"business_impact\":\"Review the unexpected increase.\",\"recommended_actions\":[\"Check recent deployments\",\"Confirm workload ownership\"],\"priority\":\"high\",\"confidence\":0.82}"
				}]
			}]
		}`), nil
	})}

	client := New(Config{
		APIKey:  "secret",
		BaseURL: "https://api.openai.test/v1",
		Model:   "gpt-5.4-mini",
	}, httpClient)
	got, err := client.Generate(context.Background(), aiport.Request{
		Kind:    "anomaly",
		Context: map[string]any{"service": "Compute", "change_percent": 51.2},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if got.Summary != "Spend increased materially." || got.Priority != "high" || got.Confidence != 0.82 {
		t.Fatalf("unexpected result: %+v", got)
	}
	if len(got.RecommendedActions) != 2 {
		t.Fatalf("expected two actions, got %+v", got.RecommendedActions)
	}
}

func TestClientGenerateRejectsNonSuccessResponse(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusTooManyRequests, `{"error":{"message":"rate limited"}}`), nil
	})}

	client := New(Config{APIKey: "secret", BaseURL: "https://api.openai.test/v1", Model: "gpt-5.4-mini"}, httpClient)
	if _, err := client.Generate(context.Background(), aiport.Request{Kind: "waste", Context: map[string]any{}}); err == nil {
		t.Fatal("expected non-success response error")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}
