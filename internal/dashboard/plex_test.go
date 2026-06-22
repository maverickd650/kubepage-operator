package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestPlexWidgetPoll(t *testing.T) {
	tests := map[string]struct {
		response   string
		statusCode int
		want       []Field
	}{
		"streams active": {
			response:   `{"MediaContainer":{"size":3}}`,
			statusCode: http.StatusOK,
			want: []Field{
				{Label: labelStatus, Value: statusHealthy},
				{Label: labelStreams, Value: "3"},
			},
		},
		"no streams": {
			response:   `{"MediaContainer":{"size":0}}`,
			statusCode: http.StatusOK,
			want: []Field{
				{Label: labelStatus, Value: statusHealthy},
				{Label: labelStreams, Value: "0"},
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
			var gotToken string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotToken = r.Header.Get("X-Plex-Token")
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.response))
			}))
			defer srv.Close()

			got, err := (plexWidget{}).Poll(context.Background(), srv.Client(), WidgetConfig{
				URL:     srv.URL,
				Secrets: map[string]string{testSecretField: "plextok"},
			})
			if err != nil {
				t.Fatalf("Poll() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(tc.want, got) {
				t.Errorf("Poll() = %+v, want %+v", got, tc.want)
			}
			if gotToken != "plextok" {
				t.Errorf("X-Plex-Token header = %q, want %q", gotToken, "plextok")
			}
		})
	}
}

func TestPlexWidgetPollMissingURL(t *testing.T) {
	if _, err := (plexWidget{}).Poll(context.Background(), http.DefaultClient, WidgetConfig{}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestPlexWidgetPollUnreachable(t *testing.T) {
	got, err := (plexWidget{}).Poll(context.Background(), http.DefaultClient, WidgetConfig{URL: testUnreachableAddr})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}
