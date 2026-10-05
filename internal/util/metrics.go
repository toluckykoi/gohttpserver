package util

import (
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

// Metrics holds the process-wide request counters exposed at /metrics.
//
// It is intentionally dependency-free: the server ships as a single
// static binary, and the handful of counters below are cheap enough to
// keep with atomics instead of pulling in a full Prometheus client. The
// exposition format matches Prometheus' text format so an existing
// scraper can consume it without extra configuration.
type Metrics struct {
	start time.Time

	requestsTotal    atomic.Int64
	requestsInFlight atomic.Int64
	responses2xx     atomic.Int64
	responses3xx     atomic.Int64
	responses4xx     atomic.Int64
	responses5xx     atomic.Int64
	responseBytes    atomic.Int64
}

// metrics is the process-wide registry used by the logging middleware.
var metrics = &Metrics{start: time.Now()}

// MetricsRegistry returns the process-wide metrics registry.
func MetricsRegistry() *Metrics { return metrics }

// Observe records the outcome of a single request. It is called by the
// logging middleware, which already has the status code and byte count.
func (m *Metrics) Observe(status, bytes int) {
	m.requestsTotal.Add(1)
	m.responseBytes.Add(int64(bytes))
	switch {
	case status >= 500:
		m.responses5xx.Add(1)
	case status >= 400:
		m.responses4xx.Add(1)
	case status >= 300:
		m.responses3xx.Add(1)
	default:
		m.responses2xx.Add(1)
	}
}

// requestStarted increments the in-flight gauge; the returned function
// decrements it and must always be called (defer it).
func (m *Metrics) requestStarted() func() {
	m.requestsInFlight.Add(1)
	return func() { m.requestsInFlight.Add(-1) }
}

// writePrometheusMetrics renders the registry in Prometheus
// text-exposition format.
func writePrometheusMetrics(w http.ResponseWriter, m *Metrics, version string) {
	writeCounter := func(name, help string, value int64) {
		fmt.Fprintf(w, "# HELP %s %s\n", name, help)
		fmt.Fprintf(w, "# TYPE %s counter\n", name)
		fmt.Fprintf(w, "%s %d\n", name, value)
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	writeCounter("gohttpserver_requests_total", "Total number of HTTP requests served.", m.requestsTotal.Load())
	writeCounter("gohttpserver_response_bytes_total", "Total number of response body bytes written.", m.responseBytes.Load())

	fmt.Fprint(w, "# HELP gohttpserver_response_status_total HTTP responses by status class.\n")
	fmt.Fprint(w, "# TYPE gohttpserver_response_status_total counter\n")
	for _, s := range []struct {
		class string
		value int64
	}{
		{"2xx", m.responses2xx.Load()},
		{"3xx", m.responses3xx.Load()},
		{"4xx", m.responses4xx.Load()},
		{"5xx", m.responses5xx.Load()},
	} {
		fmt.Fprintf(w, "gohttpserver_response_status_total{class=%q} %d\n", s.class, s.value)
	}

	fmt.Fprint(w, "# HELP gohttpserver_requests_in_flight HTTP requests currently being served.\n")
	fmt.Fprint(w, "# TYPE gohttpserver_requests_in_flight gauge\n")
	fmt.Fprintf(w, "gohttpserver_requests_in_flight %d\n", m.requestsInFlight.Load())

	fmt.Fprint(w, "# HELP gohttpserver_uptime_seconds Seconds since the process started.\n")
	fmt.Fprint(w, "# TYPE gohttpserver_uptime_seconds gauge\n")
	fmt.Fprintf(w, "gohttpserver_uptime_seconds %s\n", strconv.FormatFloat(time.Since(m.start).Seconds(), 'f', 3, 64))

	fmt.Fprint(w, "# HELP gohttpserver_build_info Build metadata; value is always 1.\n")
	fmt.Fprint(w, "# TYPE gohttpserver_build_info gauge\n")
	fmt.Fprintf(w, "gohttpserver_build_info{version=%q} 1\n", version)
}

// MetricsHandler returns the /metrics handler.
func MetricsHandler(version string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writePrometheusMetrics(w, metrics, version)
	}
}
