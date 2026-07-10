package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

const sonarrTestAPIKey = "sonartok"

func TestSonarrWidgetPoll(t *testing.T) {
	var gotAPIKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get(headerXAPIKey)
		switch r.URL.Path {
		case "/api/v3/series":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{},{},{}]`))
		case "/api/v3/queue":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"totalRecords":2}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	got, err := (sonarrWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{secretAPIKey: sonarrTestAPIKey},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{
		{Label: labelSeries, Value: "3"},
		{Label: labelQueue, Value: "2"},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
	if gotAPIKey != sonarrTestAPIKey {
		t.Errorf("X-Api-Key header = %q, want %q", gotAPIKey, sonarrTestAPIKey)
	}
}

func TestSonarrWidgetPollNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	got, err := (sonarrWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{secretAPIKey: sonarrTestAPIKey},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: testHTTP401}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestSonarrWidgetPollMissingURL(t *testing.T) {
	if _, err := (sonarrWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestSonarrWidgetPollUnreachable(t *testing.T) {
	got, err := (sonarrWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: testUnreachableAddr})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestSonarrWidgetSample(t *testing.T) {
	got := (sonarrWidget{}).Sample(WidgetConfig{})
	if len(got) != 2 || got[0].Label != labelSeries || got[1].Label != labelQueue {
		t.Errorf("Sample() = %+v, want Series/Queue fields", got)
	}
	assertSampleDeterministic(t, sonarrWidget{})
}
