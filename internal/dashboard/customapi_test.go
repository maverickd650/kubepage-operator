package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestCustomAPIWidgetPoll(t *testing.T) {
	tests := map[string]struct {
		config     string
		response   string
		statusCode int
		want       []Field
		wantErr    bool
	}{
		"scalar and nested fields": {
			config:     `{"mappings":[{"label":"Status","jsonpath":"status"},{"label":"Used","jsonpath":"disk.used","suffix":"%"}]}`,
			response:   `{"status":"ok","disk":{"used":42.5}}`,
			statusCode: http.StatusOK,
			want:       []Field{{Label: labelStatus, Value: "ok"}, {Label: "Used", Value: "42.5%"}},
		},
		"array index": {
			config:     `{"mappings":[{"label":"First","jsonpath":"items.0.name"}]}`,
			response:   `{"items":[{"name":"alpha"},{"name":"beta"}]}`,
			statusCode: http.StatusOK,
			want:       []Field{{Label: testLabelFirst, Value: testValueAlpha}},
		},
		"missing path yields unknown": {
			config:     `{"mappings":[{"label":"Missing","jsonpath":"nope.nope"}]}`,
			response:   `{"status":"ok"}`,
			statusCode: http.StatusOK,
			want:       []Field{{Label: "Missing", Value: statusUnknown}},
		},
		"mapping with no label or path is skipped": {
			config:     `{"mappings":[{"label":"","jsonpath":"status"},{"label":"Status","jsonpath":"status"}]}`,
			response:   `{"status":"ok"}`,
			statusCode: http.StatusOK,
			want:       []Field{{Label: labelStatus, Value: "ok"}},
		},
		testCaseNon200: {
			config:     `{"mappings":[{"label":"Status","jsonpath":"status"}]}`,
			statusCode: http.StatusInternalServerError,
			want:       []Field{{Label: labelStatus, Value: testHTTP500}},
		},
		"missing mappings": {
			config:  `{}`,
			wantErr: true,
		},
		"bool and object values": {
			config:     `{"mappings":[{"label":"Ready","jsonpath":"ready"},{"label":"Extra","jsonpath":"nested"}]}`,
			response:   `{"ready":true,"nested":{"a":1}}`,
			statusCode: http.StatusOK,
			want:       []Field{{Label: "Ready", Value: "true"}, {Label: "Extra", Value: `{"a":1}`}},
		},
		"null value formats as empty string": {
			config:     `{"mappings":[{"label":"Missing","jsonpath":"missing"}]}`,
			response:   `{"missing":null}`,
			statusCode: http.StatusOK,
			want:       []Field{{Label: "Missing", Value: ""}},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.response))
			}))
			defer srv.Close()

			got, err := (customAPIWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
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
		})
	}
}

func TestCustomAPIWidgetPollMissingURL(t *testing.T) {
	if _, err := (customAPIWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{Config: []byte(`{"mappings":[]}`)}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestCustomAPIWidgetPollMissingConfig(t *testing.T) {
	if _, err := (customAPIWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: testExampleURL}); err == nil {
		t.Fatal("Poll() expected error for empty Config, got nil")
	}
}

func TestCustomAPIWidgetPollMalformedConfig(t *testing.T) {
	_, err := (customAPIWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{
		URL: testExampleURL, Config: []byte(`{not valid json`),
	})
	if err == nil {
		t.Fatal("Poll() expected error for malformed Config JSON, got nil")
	}
}

func TestCustomAPIWidgetPollUnreachable(t *testing.T) {
	got, err := (customAPIWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{
		URL:    testUnreachableAddr,
		Config: []byte(`{"mappings":[{"label":"Status","jsonpath":"status"}]}`),
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestCustomAPIWidgetPollBearerToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	_, err := (customAPIWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Config:  []byte(`{"mappings":[{"label":"Status","jsonpath":"status"}]}`),
		Secrets: map[string]string{"token": "tok123"},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	if gotAuth != "Bearer tok123" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer tok123")
	}
}

func TestCustomAPIWidgetSample(t *testing.T) {
	tests := map[string]struct {
		config string
		want   []Field
	}{
		"no config falls back to a generic Value field": {
			want: []Field{{Label: labelValue, Value: sampleCustomAPIValue}},
		},
		"echoes configured labels with a placeholder value": {
			config: `{"mappings":[{"label":"Status","jsonpath":"status"},{"label":"Used","jsonpath":"disk.used","suffix":"%"}]}`,
			want:   []Field{{Label: labelStatus, Value: sampleCustomAPIValue}, {Label: "Used", Value: sampleCustomAPIValue + "%"}},
		},
		"unlabeled mappings are skipped, falling back if none remain": {
			config: `{"mappings":[{"jsonpath":"status"}]}`,
			want:   []Field{{Label: labelValue, Value: sampleCustomAPIValue}},
		},
		"malformed config falls back": {
			config: `{not valid json`,
			want:   []Field{{Label: labelValue, Value: sampleCustomAPIValue}},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := (customAPIWidget{}).Sample(WidgetConfig{Config: []byte(tc.config)})
			if !reflect.DeepEqual(tc.want, got) {
				t.Errorf("Sample() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestJSONPathLookup(t *testing.T) {
	body := map[string]any{
		"status":      "ok",
		testLabelDisk: map[string]any{"used": 42.5},
		"items":       []any{map[string]any{"name": testValueAlpha}},
	}

	tests := map[string]struct {
		path   string
		want   any
		wantOK bool
	}{
		"top-level":             {"status", "ok", true},
		"nested":                {"disk.used", 42.5, true},
		"array index":           {"items.0.name", testValueAlpha, true},
		"missing key":           {"nope", nil, false},
		"index oob":             {"items.5", nil, false},
		"index not int":         {"items.x", nil, false},
		"through scalar":        {"status.nope", nil, false},
		"leading empty segment": {".status", "ok", true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, ok := jsonPathLookup(body, tc.path)
			if ok != tc.wantOK {
				t.Fatalf("jsonPathLookup(%q) ok = %v, want %v", tc.path, ok, tc.wantOK)
			}
			if ok && got != tc.want {
				t.Errorf("jsonPathLookup(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}
