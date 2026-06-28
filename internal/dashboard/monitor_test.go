package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeHTTP(t *testing.T) {
	tests := map[string]struct {
		status   int
		wantUp   bool
		method   string // expected to actually be served (HEAD or GET fallback)
		headFail bool   // server 405s HEAD, forcing the GET fallback
	}{
		"200 up":            {status: http.StatusOK, wantUp: true},
		"301 up":            {status: http.StatusMovedPermanently, wantUp: true},
		"500 down":          {status: http.StatusInternalServerError, wantUp: false},
		"head 405 then get": {status: http.StatusOK, wantUp: true, headFail: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.headFail && r.Method == http.MethodHead {
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()

			up, latency, err := probeHTTP(context.Background(), srv.Client(), srv.URL)
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
	up, _, err := probeHTTP(context.Background(), http.DefaultClient, testUnreachableAddr)
	if err == nil {
		t.Fatal("probeHTTP() expected transport error for unreachable host, got nil")
	}
	if up {
		t.Error("probeHTTP() up = true for unreachable host, want false")
	}
}

func TestMonitorResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	status, latency := monitorResult(context.Background(), srv.Client(), srv.URL)
	if status != "Up" {
		t.Errorf("monitorResult() status = %q, want Up", status)
	}
	if latency == "" {
		t.Error("monitorResult() latency empty, want a duration when up")
	}

	downStatus, downLatency := monitorResult(context.Background(), http.DefaultClient, testUnreachableAddr)
	if downStatus != statusDown {
		t.Errorf("monitorResult() status = %q, want Down", downStatus)
	}
	if downLatency != "" {
		t.Errorf("monitorResult() latency = %q, want empty when down", downLatency)
	}
}
