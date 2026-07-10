package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestImmichWidgetPoll(t *testing.T) {
	var gotAPIKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get(headerXAPIKeyLower)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"photos":18234,"videos":412}`))
	}))
	defer srv.Close()

	got, err := (immichWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{secretAPIKey: "immichtok"},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{
		{Label: labelPhotos, Value: "18234"},
		{Label: labelVideos, Value: "412"},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
	if gotAPIKey != "immichtok" {
		t.Errorf("x-api-key header = %q, want %q", gotAPIKey, "immichtok")
	}
}

func TestImmichWidgetPollNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	got, err := (immichWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{URL: srv.URL})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: testHTTP401}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestImmichWidgetPollMissingURL(t *testing.T) {
	if _, err := (immichWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestImmichWidgetPollUnreachable(t *testing.T) {
	got, err := (immichWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: testUnreachableAddr})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestImmichWidgetSample(t *testing.T) {
	got := (immichWidget{}).Sample(WidgetConfig{})
	if len(got) != 2 || got[0].Label != labelPhotos || got[1].Label != labelVideos {
		t.Errorf("Sample() = %+v, want Photos/Videos fields", got)
	}
	assertSampleDeterministic(t, immichWidget{})
}
