package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProbeHTTP(t *testing.T) {
	tests := map[string]struct {
		status       int
		wantUp       bool
		method       string // expected to actually be served (HEAD or GET fallback)
		headFailWith int    // if non-zero, server responds with this status to HEAD, forcing the GET fallback
	}{
		"200 up":            {status: http.StatusOK, wantUp: true},
		"301 up":            {status: http.StatusMovedPermanently, wantUp: true},
		"500 down":          {status: http.StatusInternalServerError, wantUp: false},
		"head 405 then get": {status: http.StatusOK, wantUp: true, headFailWith: http.StatusMethodNotAllowed},
		"head 501 then get": {status: http.StatusOK, wantUp: true, headFailWith: http.StatusNotImplemented},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.headFailWith != 0 && r.Method == http.MethodHead {
					w.WriteHeader(tc.headFailWith)
					return
				}
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()

			up, latency, err := probeHTTP(t.Context(), srv.Client(), srv.URL)
			if err != nil {
				t.Fatalf("probeHTTP() unexpected error: %v", err)
			}
			if up != tc.wantUp {
				t.Errorf("probeHTTP() up = %v, want %v", up, tc.wantUp)
			}
			if up && latency <= 0 {
				t.Errorf("probeHTTP() latency = %v, want > 0 when up", latency)
			}
		})
	}
}

func TestProbeHTTPUnreachable(t *testing.T) {
	up, _, err := probeHTTP(t.Context(), http.DefaultClient, testUnreachableAddr)
	if err == nil {
		t.Fatal("probeHTTP() expected transport error for unreachable host, got nil")
	}
	if up {
		t.Error("probeHTTP() up = true for unreachable host, want false")
	}
}

// TestProbeHTTPMalformedURL exercises doProbe's request-building error path
// (http.NewRequestWithContext itself rejecting the URL), distinct from
// TestProbeHTTPUnreachable's transport-level failure. A URL containing a raw
// control character is invalid at the net/url parsing stage, before any
// network call happens.
func TestProbeHTTPMalformedURL(t *testing.T) {
	const malformedURL = "http://example.com/\x7f"

	up, _, err := probeHTTP(t.Context(), http.DefaultClient, malformedURL)
	if err == nil {
		t.Fatal("probeHTTP() error = nil, want a request-building error for a malformed URL")
	}
	if !strings.Contains(err.Error(), "building HEAD request") {
		t.Errorf("probeHTTP() error = %q, want it to mention building the request", err.Error())
	}
	if up {
		t.Error("probeHTTP() up = true for a malformed URL, want false")
	}
}

func TestMonitorResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	status, latency := monitorResult(t.Context(), srv.Client(), srv.URL)
	if status != "Up" {
		t.Errorf("monitorResult() status = %q, want Up", status)
	}
	if latency == "" {
		t.Error("monitorResult() latency empty, want a duration when up")
	}

	downStatus, downLatency := monitorResult(t.Context(), http.DefaultClient, testUnreachableAddr)
	if downStatus != statusDown {
		t.Errorf("monitorResult() status = %q, want Down", downStatus)
	}
	if downLatency != "" {
		t.Errorf("monitorResult() latency = %q, want empty when down", downLatency)
	}
}
