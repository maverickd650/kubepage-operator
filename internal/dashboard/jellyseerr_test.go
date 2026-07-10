package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestJellyseerrWidgetPoll(t *testing.T) {
	var gotAPIKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get(headerXAPIKey)
		switch r.URL.Path {
		case "/api/v1/status":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"version":"2.1.0"}`))
		case "/api/v1/request/count":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"pending":3}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	got, err := (jellyseerrWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{secretAPIKey: "seerrtok"},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{
		{Label: labelVersion, Value: "2.1.0"},
		{Label: labelPending, Value: "3"},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
	if gotAPIKey != "seerrtok" {
		t.Errorf("X-Api-Key header = %q, want %q", gotAPIKey, "seerrtok")
	}
}

func TestJellyseerrWidgetPollNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	got, err := (jellyseerrWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{URL: srv.URL})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: testHTTP401}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestJellyseerrWidgetPollMissingURL(t *testing.T) {
	if _, err := (jellyseerrWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestJellyseerrWidgetPollUnreachable(t *testing.T) {
	got, err := (jellyseerrWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: testUnreachableAddr})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestJellyseerrWidgetSample(t *testing.T) {
	got := (jellyseerrWidget{}).Sample(WidgetConfig{})
	if len(got) != 2 || got[0].Label != labelVersion || got[1].Label != labelPending {
		t.Errorf("Sample() = %+v, want Version/Pending fields", got)
	}
	assertSampleDeterministic(t, jellyseerrWidget{})
}
