package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestPrometheusWidgetPoll(t *testing.T) {
	tests := map[string]struct {
		response    string
		statusCode  int
		unreachable bool
		want        []Field
		wantErr     bool
	}{
		"all targets up": {
			response: `{"status":"success","data":{"activeTargets":[
				{"health":"up"},{"health":"up"}
			]}}`,
			statusCode: http.StatusOK,
			want: []Field{
				{Label: labelStatus, Value: statusHealthy},
				{Label: labelTargetsUp, Value: "2 / 2"},
			},
		},
		"some targets down": {
			response: `{"status":"success","data":{"activeTargets":[
				{"health":"up"},{"health":"down"}
			]}}`,
			statusCode: http.StatusOK,
			want: []Field{
				{Label: labelStatus, Value: statusDegraded},
				{Label: labelTargetsUp, Value: "1 / 2"},
			},
		},
		"no targets": {
			response:   `{"status":"success","data":{"activeTargets":[]}}`,
			statusCode: http.StatusOK,
			want: []Field{
				{Label: labelStatus, Value: statusUnknown},
				{Label: labelTargetsUp, Value: "0 / 0"},
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
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.response))
			}))
			defer srv.Close()

			got, err := (prometheusWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{URL: srv.URL})
			if tc.wantErr != (err != nil) {
				t.Fatalf("Poll() error = %v, wantErr %v", err, tc.wantErr)
			}
			if !reflect.DeepEqual(tc.want, got) {
				t.Errorf("Poll() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestPrometheusWidgetPollUnreachable(t *testing.T) {
	got, err := (prometheusWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: testUnreachableAddr})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestPrometheusWidgetPollMissingURL(t *testing.T) {
	if _, err := (prometheusWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}
