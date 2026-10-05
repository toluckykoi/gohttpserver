package util

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetricsObserveClassifiesStatusAndBytes(t *testing.T) {
	m := &Metrics{start: time.Now()}
	m.Observe(200, 10)
	m.Observe(204, 0)
	m.Observe(301, 0)
	m.Observe(404, 5)
	m.Observe(500, 0)

	if got := m.requestsTotal.Load(); got != 5 {
		t.Fatalf("requestsTotal = %d, want 5", got)
	}
	if got := m.responseBytes.Load(); got != 15 {
		t.Fatalf("responseBytes = %d, want 15", got)
	}
	if got := m.responses2xx.Load(); got != 2 {
		t.Fatalf("responses2xx = %d, want 2", got)
	}
	if got := m.responses3xx.Load(); got != 1 {
		t.Fatalf("responses3xx = %d, want 1", got)
	}
	if got := m.responses4xx.Load(); got != 1 {
		t.Fatalf("responses4xx = %d, want 1", got)
	}
	if got := m.responses5xx.Load(); got != 1 {
		t.Fatalf("responses5xx = %d, want 1", got)
	}
}

func TestMetricsHandlerRendersPrometheusFormat(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	MetricsHandler("v9.9.9").ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"gohttpserver_requests_total",
		"gohttpserver_response_status_total{class=\"2xx\"}",
		"gohttpserver_requests_in_flight",
		"gohttpserver_uptime_seconds",
		`gohttpserver_build_info{version="v9.9.9"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics body missing %q\n%s", want, body)
		}
	}
}

func TestMetricsHandlerRejectsPost(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/metrics", nil)
	MetricsHandler("dev").ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
