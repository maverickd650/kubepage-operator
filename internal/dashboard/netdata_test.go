package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestNetdataWidgetPoll(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"alarms":{"warning":2,"critical":0}}`))
	}))
	defer srv.Close()

	got, err := (netdataWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{URL: srv.URL})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{
		{Label: labelWarnings, Value: "2"},
		{Label: labelCriticals, Value: "0"},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
	if gotPath != "/api/v1/info" {
		t.Errorf("request path = %q, want %q", gotPath, "/api/v1/info")
	}
}

func TestNetdataWidgetPollNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	got, err := (netdataWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{URL: srv.URL})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: testHTTP500}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestNetdataWidgetPollMissingURL(t *testing.T) {
	if _, err := (netdataWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestNetdataWidgetPollUnreachable(t *testing.T) {
	got, err := (netdataWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: testUnreachableAddr})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestNetdataWidgetSample(t *testing.T) {
	got := (netdataWidget{}).Sample(WidgetConfig{})
	if len(got) != 2 || got[0].Label != labelWarnings || got[1].Label != labelCriticals {
		t.Errorf("Sample() = %+v, want Warnings/Criticals fields", got)
	}
	assertSampleDeterministic(t, netdataWidget{})
}
