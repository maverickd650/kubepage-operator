package dashboard

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
)

func TestPrometheusMetricWidgetPoll(t *testing.T) {
	tests := map[string]struct {
		config     string
		response   string
		statusCode int
		want       []Field
		wantErr    bool
	}{
		"scalar value with custom label": {
			config:     `{"query":"up","label":"Up Targets"}`,
			response:   `{"status":"success","data":{"result":[{"value":[1700000000,"4"]}]}}`,
			statusCode: http.StatusOK,
			want:       []Field{{Label: "Up Targets", Value: "4"}},
		},
		"default label": {
			config:     `{"query":"sum(rate(http_requests_total[5m]))"}`,
			response:   `{"status":"success","data":{"result":[{"value":[1700000000,"12.5"]}]}}`,
			statusCode: http.StatusOK,
			want:       []Field{{Label: labelValue, Value: "12.5"}},
		},
		"empty result": {
			config:     `{"query":"nonexistent_metric"}`,
			response:   `{"status":"success","data":{"result":[]}}`,
			statusCode: http.StatusOK,
			want:       []Field{{Label: labelValue, Value: statusUnknown}},
		},
		testCaseNon200: {
			config:     `{"query":"up"}`,
			statusCode: http.StatusInternalServerError,
			want:       []Field{{Label: labelStatus, Value: testHTTP500}},
		},
		"missing query": {
			config:  `{}`,
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var gotQuery string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotQuery, _ = url.QueryUnescape(r.URL.Query().Get("query"))
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.response))
			}))
			defer srv.Close()

			got, err := (prometheusMetricWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
				URL:    srv.URL,
				Config: []byte(tc.config),
			})
			if tc.wantErr {
				if err == nil {
					t.Fatal("Poll() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Poll() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(tc.want, got) {
				t.Errorf("Poll() = %+v, want %+v", got, tc.want)
			}
			_ = gotQuery
		})
	}
}

func TestPrometheusMetricWidgetPollMissingURL(t *testing.T) {
	if _, err := (prometheusMetricWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{Config: []byte(`{"query":"up"}`)}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestPrometheusMetricWidgetSample(t *testing.T) {
	tests := map[string]struct {
		config string
		want   []Field
	}{
		"no config falls back to the default label": {
			want: []Field{{Label: labelValue, Value: "42"}},
		},
		"echoes the configured label": {
			config: `{"query":"up","label":"Up Targets"}`,
			want:   []Field{{Label: "Up Targets", Value: "42"}},
		},
		"malformed config falls back": {
			config: `{not valid json`,
			want:   []Field{{Label: labelValue, Value: "42"}},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := (prometheusMetricWidget{}).Sample(WidgetConfig{Config: []byte(tc.config)})
			if !reflect.DeepEqual(tc.want, got) {
				t.Errorf("Sample() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestPrometheusMetricWidgetPollUnreachable(t *testing.T) {
	got, err := (prometheusMetricWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{
		URL:    testUnreachableAddr,
		Config: []byte(`{"query":"up"}`),
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}
