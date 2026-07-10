package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestJellyfinWidgetPoll(t *testing.T) {
	var gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get(headerXEmbyToken)
		switch r.URL.Path {
		case "/System/Info":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"Version":"10.9.7"}`))
		case "/Sessions":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"NowPlayingItem":{"Name":"Movie"}},{}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	got, err := (jellyfinWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{testSecretField: "embytok"},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{
		{Label: labelVersion, Value: "10.9.7"},
		{Label: labelStreams, Value: "1"},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
	if gotToken != "embytok" {
		t.Errorf("X-Emby-Token header = %q, want %q", gotToken, "embytok")
	}
}

func TestJellyfinWidgetPollNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	got, err := (jellyfinWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{URL: srv.URL})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: testHTTP401}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestJellyfinWidgetPollMissingURL(t *testing.T) {
	if _, err := (jellyfinWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestJellyfinWidgetPollUnreachable(t *testing.T) {
	got, err := (jellyfinWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: testUnreachableAddr})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestJellyfinWidgetSample(t *testing.T) {
	got := (jellyfinWidget{}).Sample(WidgetConfig{})
	if len(got) != 2 || got[0].Label != labelVersion || got[1].Label != labelStreams {
		t.Errorf("Sample() = %+v, want Version/Streams fields", got)
	}
	assertSampleDeterministic(t, jellyfinWidget{})
}
