package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

const testTunnelName = "home-tunnel"

func TestCloudflaredWidgetPoll(t *testing.T) {
	tests := map[string]struct {
		response   string
		statusCode int
		want       []Field
	}{
		"healthy tunnel": {
			response:   `{"result":{"name":"home-tunnel","status":"healthy"}}`,
			statusCode: http.StatusOK,
			want: []Field{
				{Label: labelStatus, Value: statusHealthy},
				{Label: labelTunnel, Value: testTunnelName},
			},
		},
		"degraded tunnel": {
			response:   `{"result":{"name":"home-tunnel","status":"degraded"}}`,
			statusCode: http.StatusOK,
			want: []Field{
				{Label: labelStatus, Value: statusDegraded},
				{Label: labelTunnel, Value: testTunnelName},
			},
		},
		"down tunnel": {
			response:   `{"result":{"name":"home-tunnel","status":"down"}}`,
			statusCode: http.StatusOK,
			want: []Field{
				{Label: labelStatus, Value: statusUnreach},
				{Label: labelTunnel, Value: testTunnelName},
			},
		},
		testCaseNon200: {
			statusCode: http.StatusForbidden,
			want: []Field{
				{Label: labelStatus, Value: "HTTP 403"},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var gotPath, gotAuth string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotAuth = r.Header.Get("Authorization")
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.response))
			}))
			defer srv.Close()

			got, err := (cloudflaredWidget{}).Poll(context.Background(), srv.Client(), WidgetConfig{
				URL:     srv.URL,
				Config:  []byte(`{"accountId":"acct123","tunnelId":"tun456"}`),
				Secrets: map[string]string{testSecretField: "cftok"},
			})
			if err != nil {
				t.Fatalf("Poll() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(tc.want, got) {
				t.Errorf("Poll() = %+v, want %+v", got, tc.want)
			}
			if gotPath != "/accounts/acct123/cfd_tunnel/tun456" {
				t.Errorf("path = %q, want %q", gotPath, "/accounts/acct123/cfd_tunnel/tun456")
			}
			if gotAuth != "Bearer cftok" {
				t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer cftok")
			}
		})
	}
}

func TestCloudflaredWidgetPollMissingConfig(t *testing.T) {
	if _, err := (cloudflaredWidget{}).Poll(context.Background(), http.DefaultClient, WidgetConfig{}); err == nil {
		t.Fatal("Poll() expected error for missing config, got nil")
	}
}

func TestCloudflaredWidgetPollUnreachable(t *testing.T) {
	got, err := (cloudflaredWidget{}).Poll(context.Background(), http.DefaultClient, WidgetConfig{
		URL:    testUnreachableAddr,
		Config: []byte(`{"accountId":"a","tunnelId":"t"}`),
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}
