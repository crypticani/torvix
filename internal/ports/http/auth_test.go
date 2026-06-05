package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestAPIEndpointsRequireBearerWhenConfigured(t *testing.T) {
	handler := NewWithOptions(nil, nil, nil, nil, nil, prometheus.NewRegistry(), HandlerOptions{
		APIAuthEnabled: true,
		APIAuthToken:   "secret",
	})

	for _, tc := range []struct {
		name   string
		method string
		path   string
	}{
		{name: "ingest", method: http.MethodPost, path: "/api/v1/ingest"},
		{name: "ingest status", method: http.MethodGet, path: "/api/v1/ingest/status/job-1"},
		{name: "analytics summary", method: http.MethodGet, path: "/api/v1/analytics/summary"},
		{name: "analytics variance", method: http.MethodGet, path: "/api/v1/analytics/variance"},
		{name: "analytics anomalies", method: http.MethodGet, path: "/api/v1/analytics/anomalies"},
		{name: "analytics forecast", method: http.MethodGet, path: "/api/v1/analytics/forecast"},
		{name: "daily report", method: http.MethodGet, path: "/api/v1/reports/daily"},
		{name: "weekly report", method: http.MethodGet, path: "/api/v1/reports/weekly"},
		{name: "monthly report", method: http.MethodGet, path: "/api/v1/reports/monthly"},
		{name: "metrics", method: http.MethodGet, path: "/metrics"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected unauthorized without token, got %d", rec.Code)
			}
		})
	}
}

func TestHealthAndSwaggerRemainPublicWhenBearerConfigured(t *testing.T) {
	handler := NewWithOptions(nil, nil, nil, nil, nil, prometheus.NewRegistry(), HandlerOptions{
		APIAuthEnabled: true,
		APIAuthToken:   "secret",
	})

	for _, path := range []string{"/healthz", "/swagger/"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code == http.StatusUnauthorized {
				t.Fatalf("expected %s to remain public, got unauthorized", path)
			}
		})
	}
}

func TestBearerAllowsProtectedEndpointWhenConfigured(t *testing.T) {
	handler := NewWithOptions(nil, nil, nil, nil, nil, prometheus.NewRegistry(), HandlerOptions{
		APIAuthEnabled: true,
		APIAuthToken:   "secret",
	})

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected metrics to be available with token, got %d", rec.Code)
	}
}
