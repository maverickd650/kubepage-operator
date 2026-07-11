package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestGatusWidgetPoll(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"core_frontend": {"results":[{"success":true},{"success":true}]},
			"core_backend": {"results":[{"success":true},{"success":false}]},
			"core_empty": {"results":[]}
		}`))
	}))
	defer srv.Close()

	got, err := (gatusWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{URL: srv.URL})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{
		{Label: labelUp, Value: "1"},
		{Label: labelDown, Value: "1"},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
	if gotPath != "/api/v1/endpoints/statuses" {
		t.Errorf("request path = %q, want %q", gotPath, "/api/v1/endpoints/statuses")
	}
}

func TestGatusWidgetPollNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	got, err := (gatusWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{URL: srv.URL})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: "HTTP 503"}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestGatusWidgetPollMissingURL(t *testing.T) {
	if _, err := (gatusWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestGatusWidgetPollUnreachable(t *testing.T) {
	got, err := (gatusWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: testUnreachableAddr})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestGatusWidgetSample(t *testing.T) {
	got := (gatusWidget{}).Sample(WidgetConfig{})
	if len(got) != 2 || got[0].Label != labelUp || got[1].Label != labelDown {
		t.Errorf("Sample() = %+v, want Up/Down fields", got)
	}
	assertSampleDeterministic(t, gatusWidget{})
}
