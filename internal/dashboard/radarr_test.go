package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

const radarrTestAPIKey = "radartok"

func TestRadarrWidgetPoll(t *testing.T) {
	var gotAPIKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get(headerXAPIKey)
		switch r.URL.Path {
		case "/api/v3/movie":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{},{}]`))
		case "/api/v3/queue":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"totalRecords":1}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	got, err := (radarrWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{secretAPIKey: radarrTestAPIKey},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{
		{Label: labelMovies, Value: "2"},
		{Label: labelQueue, Value: "1"},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
	if gotAPIKey != radarrTestAPIKey {
		t.Errorf("X-Api-Key header = %q, want %q", gotAPIKey, radarrTestAPIKey)
	}
}

func TestRadarrWidgetPollNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	got, err := (radarrWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{secretAPIKey: radarrTestAPIKey},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: testHTTP500}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestRadarrWidgetPollMissingURL(t *testing.T) {
	if _, err := (radarrWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestRadarrWidgetPollUnreachable(t *testing.T) {
	got, err := (radarrWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: testUnreachableAddr})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestRadarrWidgetSample(t *testing.T) {
	got := (radarrWidget{}).Sample(WidgetConfig{})
	if len(got) != 2 || got[0].Label != labelMovies || got[1].Label != labelQueue {
		t.Errorf("Sample() = %+v, want Movies/Queue fields", got)
	}
	assertSampleDeterministic(t, radarrWidget{})
}
