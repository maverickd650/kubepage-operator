package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestHomeassistantWidgetPoll(t *testing.T) {
	tests := map[string]struct {
		response   string
		statusCode int
		want       []Field
	}{
		"reachable": {
			response:   `{"version":"2024.1.0","location_name":"Home"}`,
			statusCode: http.StatusOK,
			want: []Field{
				{Label: labelStatus, Value: statusHealthy},
				{Label: labelVersion, Value: "2024.1.0"},
			},
		},
		testCaseNon200: {
			statusCode: http.StatusUnauthorized,
			want:       []Field{{Label: labelStatus, Value: testHTTP401}},
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

			got, err := (homeassistantWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
				URL:     srv.URL,
				Secrets: map[string]string{testSecretField: "haTok"},
			})
			if err != nil {
				t.Fatalf("Poll() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(tc.want, got) {
				t.Errorf("Poll() = %+v, want %+v", got, tc.want)
			}
			if gotAuth != "Bearer haTok" {
				t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer haTok")
			}
		})
	}
}

func TestHomeassistantWidgetPollMissingURL(t *testing.T) {
	if _, err := (homeassistantWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestHomeassistantWidgetPollUnreachable(t *testing.T) {
	got, err := (homeassistantWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: testUnreachableAddr})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestHomeassistantWidgetSample(t *testing.T) {
	got := (homeassistantWidget{}).Sample(WidgetConfig{})
	if len(got) != 2 || got[0].Label != labelStatus || got[1].Label != labelVersion {
		t.Errorf("Sample() = %+v, want Status/Version fields", got)
	}
	assertSampleDeterministic(t, homeassistantWidget{})
}
