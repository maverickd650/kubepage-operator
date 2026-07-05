package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestTruenasWidgetPoll(t *testing.T) {
	tests := map[string]struct {
		response   string
		statusCode int
		want       []Field
	}{
		"multi-day uptime": {
			response:   `{"version":"TrueNAS-SCALE-23.10.1","uptime_seconds":266461}`,
			statusCode: http.StatusOK,
			want: []Field{
				{Label: labelVersion, Value: "TrueNAS-SCALE-23.10.1"},
				{Label: labelUptime, Value: "3d 2h"},
			},
		},
		"sub-day uptime": {
			response:   `{"version":"TrueNAS-SCALE-23.10.1","uptime_seconds":5400}`,
			statusCode: http.StatusOK,
			want: []Field{
				{Label: labelVersion, Value: "TrueNAS-SCALE-23.10.1"},
				{Label: labelUptime, Value: "1h 30m"},
			},
		},
		testCaseNon200: {
			statusCode: http.StatusUnauthorized,
			want: []Field{
				{Label: labelStatus, Value: testHTTP401},
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

			got, err := (truenasWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
				URL:     srv.URL,
				Secrets: map[string]string{testSecretField: "nastok"},
			})
			if err != nil {
				t.Fatalf("Poll() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(tc.want, got) {
				t.Errorf("Poll() = %+v, want %+v", got, tc.want)
			}
			if gotAuth != "Bearer nastok" {
				t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer nastok")
			}
		})
	}
}

func TestTruenasWidgetPollMissingURL(t *testing.T) {
	if _, err := (truenasWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestTruenasWidgetPollUnreachable(t *testing.T) {
	got, err := (truenasWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: testUnreachableAddr})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestTruenasWidgetSample(t *testing.T) {
	got := (truenasWidget{}).Sample(WidgetConfig{})
	if len(got) != 2 || got[0].Label != labelVersion || got[1].Label != labelUptime {
		t.Errorf("Sample() = %+v, want Version/Uptime fields", got)
	}
	if !reflect.DeepEqual(got, (truenasWidget{}).Sample(WidgetConfig{})) {
		t.Error("Sample() is not deterministic")
	}
}
