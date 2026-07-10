package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestTautulliWidgetPoll(t *testing.T) {
	var gotAPIKey, gotCmd string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.URL.Query().Get("apikey")
		gotCmd = r.URL.Query().Get("cmd")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"response":{"result":"success","data":{"stream_count":"2","total_bandwidth":12300}}}`))
	}))
	defer srv.Close()

	got, err := (tautulliWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{"apiKey": "tautok"},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{
		{Label: labelStreams, Value: "2"},
		{Label: labelBandwidth, Value: "12.3 Mbps"},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
	if gotAPIKey != "tautok" {
		t.Errorf("apikey query param = %q, want %q", gotAPIKey, "tautok")
	}
	if gotCmd != "get_activity" {
		t.Errorf("cmd query param = %q, want %q", gotCmd, "get_activity")
	}
}

func TestTautulliWidgetPollNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	got, err := (tautulliWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{URL: srv.URL})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: testHTTP500}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestTautulliWidgetPollMissingURL(t *testing.T) {
	if _, err := (tautulliWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestTautulliWidgetPollUnreachable(t *testing.T) {
	got, err := (tautulliWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: testUnreachableAddr})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestTautulliWidgetSample(t *testing.T) {
	got := (tautulliWidget{}).Sample(WidgetConfig{})
	if len(got) != 2 || got[0].Label != labelStreams || got[1].Label != labelBandwidth {
		t.Errorf("Sample() = %+v, want Streams/Bandwidth fields", got)
	}
	assertSampleDeterministic(t, tautulliWidget{})
}
