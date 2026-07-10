package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestGlancesWidgetPoll(t *testing.T) {
	fifty := 50
	seventy := 70
	tests := map[string]struct {
		config     string
		response   string
		statusCode int
		want       []Field
	}{
		"default version": {
			response:   `{"cpu":49.6,"mem":70.1}`,
			statusCode: http.StatusOK,
			want: []Field{
				{Label: labelCPU, Value: "50%", Percent: &fifty},
				{Label: labelMemory, Value: "70%", Percent: &seventy},
			},
		},
		"v3": {
			config:     `{"apiVersion":"3"}`,
			response:   `{"cpu":49.6,"mem":70.1}`,
			statusCode: http.StatusOK,
			want: []Field{
				{Label: labelCPU, Value: "50%", Percent: &fifty},
				{Label: labelMemory, Value: "70%", Percent: &seventy},
			},
		},
		testCaseNon200: {
			statusCode: http.StatusInternalServerError,
			want:       []Field{{Label: labelStatus, Value: testHTTP500}},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.response))
			}))
			defer srv.Close()

			got, err := (glancesWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
				URL:    srv.URL,
				Config: []byte(tc.config),
			})
			if err != nil {
				t.Fatalf("Poll() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(tc.want, got) {
				t.Errorf("Poll() = %+v, want %+v", got, tc.want)
			}
			if tc.statusCode == http.StatusOK {
				wantVersion := "4"
				if tc.config != "" {
					wantVersion = "3"
				}
				if wantPath := "/api/" + wantVersion + "/quicklook"; gotPath != wantPath {
					t.Errorf("request path = %q, want %q", gotPath, wantPath)
				}
			}
		})
	}
}

func TestGlancesWidgetPollMissingURL(t *testing.T) {
	if _, err := (glancesWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestGlancesWidgetPollUnreachable(t *testing.T) {
	got, err := (glancesWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: testUnreachableAddr})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestGlancesWidgetSample(t *testing.T) {
	got := (glancesWidget{}).Sample(WidgetConfig{})
	if len(got) != 2 || got[0].Label != labelCPU || got[1].Label != labelMemory {
		t.Fatalf("Sample() = %+v, want CPU/Memory fields", got)
	}
	if got[0].Percent == nil || got[1].Percent == nil {
		t.Error("Sample() fields have no Percent, want usage bars in a preview")
	}
	assertSampleDeterministic(t, glancesWidget{})
}
