package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

const uptimeKumaTestSlug = "public"

func TestUptimeKumaWidgetPoll(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status-page/" + uptimeKumaTestSlug:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"publicGroupList":[{"monitorList":[{"id":1},{"id":2},{"id":3}]}]}`))
		case "/api/status-page/heartbeat/" + uptimeKumaTestSlug:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"heartbeatList":{"1":[{"status":0},{"status":1}],"2":[{"status":0}]}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	got, err := (uptimeKumaWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:    srv.URL,
		Config: []byte(`{"slug":"` + uptimeKumaTestSlug + `"}`),
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	// monitor 1 -> last status 1 (up), monitor 2 -> last status 0 (down),
	// monitor 3 -> no heartbeat data at all (counted down).
	want := []Field{
		{Label: labelUp, Value: "1"},
		{Label: labelDown, Value: "2"},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestUptimeKumaWidgetPollMissingSlug(t *testing.T) {
	if _, err := (uptimeKumaWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: testUnreachableAddr}); err == nil {
		t.Fatal("Poll() expected error for missing config.slug, got nil")
	}
}

func TestUptimeKumaWidgetPollMissingURL(t *testing.T) {
	if _, err := (uptimeKumaWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestUptimeKumaWidgetPollNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	got, err := (uptimeKumaWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:    srv.URL,
		Config: []byte(`{"slug":"` + uptimeKumaTestSlug + `"}`),
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: "HTTP 404"}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestUptimeKumaWidgetPollUnreachable(t *testing.T) {
	got, err := (uptimeKumaWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{
		URL:    testUnreachableAddr,
		Config: []byte(`{"slug":"` + uptimeKumaTestSlug + `"}`),
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestUptimeKumaWidgetSample(t *testing.T) {
	got := (uptimeKumaWidget{}).Sample(WidgetConfig{})
	if len(got) != 2 || got[0].Label != labelUp || got[1].Label != labelDown {
		t.Errorf("Sample() = %+v, want Up/Down fields", got)
	}
	assertSampleDeterministic(t, uptimeKumaWidget{})
}
