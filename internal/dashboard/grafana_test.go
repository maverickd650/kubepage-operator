package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestGrafanaWidgetPoll(t *testing.T) {
	tests := map[string]struct {
		response   string
		statusCode int
		want       []Field
	}{
		"healthy": {
			response:   `{"database":"ok","version":"` + testGrafanaVersion + `"}`,
			statusCode: http.StatusOK,
			want: []Field{
				{Label: labelStatus, Value: statusHealthy},
				{Label: labelVersion, Value: testGrafanaVersion},
			},
		},
		"database degraded": {
			response:   `{"database":"failing","version":"` + testGrafanaVersion + `"}`,
			statusCode: http.StatusOK,
			want: []Field{
				{Label: labelStatus, Value: statusDegraded},
				{Label: labelVersion, Value: testGrafanaVersion},
			},
		},
		testCaseNon200: {
			statusCode: http.StatusInternalServerError,
			want: []Field{
				{Label: labelStatus, Value: testHTTP500},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var gotAuth string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAuth = r.Header.Get("Authorization")
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.response))
			}))
			defer srv.Close()

			got, err := (grafanaWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
				URL:     srv.URL,
				Secrets: map[string]string{testSecretField: "tok123"},
			})
			if err != nil {
				t.Fatalf("Poll() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(tc.want, got) {
				t.Errorf("Poll() = %+v, want %+v", got, tc.want)
			}
			if gotAuth != "Bearer tok123" {
				t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer tok123")
			}
		})
	}
}

func TestGrafanaWidgetPollMissingURL(t *testing.T) {
	if _, err := (grafanaWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestGrafanaWidgetPollUnreachable(t *testing.T) {
	got, err := (grafanaWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: testUnreachableAddr})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}
