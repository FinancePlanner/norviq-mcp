package server_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FinancePlanner/norviq-mcp/internal/auth"
	"github.com/FinancePlanner/norviq-mcp/internal/server"
)

func get(t *testing.T, h http.Handler, path string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	body, _ := io.ReadAll(rec.Result().Body)
	return rec.Code, string(body)
}

// The public handler is what the ingress reaches, so it must not expose the
// request and auth-failure counters.
func TestPublicHandlerDoesNotServeMetrics(t *testing.T) {
	h := server.New(server.Config{
		BackendURL:   "http://backend.invalid",
		PublicURL:    "https://mcp.example.test",
		Introspector: auth.NewIntrospector("http://backend.invalid", "unused"),
	})

	code, body := get(t, h, "/metrics")
	if code != http.StatusNotFound {
		t.Fatalf("public handler answered /metrics with %d, want 404", code)
	}
	if strings.Contains(body, "norviq_mcp_") {
		t.Fatalf("public handler leaked metrics: %q", body)
	}

	// Health checks stay on the public port: the kubelet probes it.
	if code, _ := get(t, h, "/healthz"); code != http.StatusOK {
		t.Fatalf("/healthz answered %d, want 200", code)
	}
}

func TestMetricsHandlerServesPrometheusText(t *testing.T) {
	code, body := get(t, server.MetricsHandler(), "/metrics")
	if code != http.StatusOK {
		t.Fatalf("/metrics answered %d, want 200", code)
	}
	for _, want := range []string{
		"norviq_mcp_http_requests_total",
		"norviq_mcp_auth_failures_total",
		"norviq_mcp_ready 1",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output missing %q", want)
		}
	}

	// The metrics listener serves nothing else, in particular not /mcp.
	if code, _ := get(t, server.MetricsHandler(), "/mcp"); code != http.StatusNotFound {
		t.Errorf("metrics handler answered /mcp with %d, want 404", code)
	}
}
