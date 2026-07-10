package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestPortainerWidgetPoll(t *testing.T) {
	var gotAPIKey, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get(headerXAPIKey)
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"State":"running"},{"State":"running"},{"State":"exited"}]`))
	}))
	defer srv.Close()

	got, err := (portainerWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{secretAPIKey: "portok"},
		Config:  []byte(`{"endpointId":3}`),
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{
		{Label: labelRunning, Value: "2"},
		{Label: labelStopped, Value: "1"},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
	if gotAPIKey != "portok" {
		t.Errorf("X-Api-Key header = %q, want %q", gotAPIKey, "portok")
	}
	if gotPath != "/api/endpoints/3/docker/containers/json" {
		t.Errorf("path = %q, want %q", gotPath, "/api/endpoints/3/docker/containers/json")
	}
}

func TestPortainerWidgetPollMissingEndpointID(t *testing.T) {
	if _, err := (portainerWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: testUnreachableAddr}); err == nil {
		t.Fatal("Poll() expected error for missing config.endpointId, got nil")
	}
}

func TestPortainerWidgetPollMissingURL(t *testing.T) {
	if _, err := (portainerWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestPortainerWidgetPollNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	got, err := (portainerWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:    srv.URL,
		Config: []byte(`{"endpointId":1}`),
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: testHTTP401}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestPortainerWidgetPollUnreachable(t *testing.T) {
	got, err := (portainerWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{
		URL:    testUnreachableAddr,
		Config: []byte(`{"endpointId":1}`),
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestPortainerWidgetSample(t *testing.T) {
	got := (portainerWidget{}).Sample(WidgetConfig{})
	if len(got) != 2 || got[0].Label != labelRunning || got[1].Label != labelStopped {
		t.Errorf("Sample() = %+v, want Running/Stopped fields", got)
	}
	assertSampleDeterministic(t, portainerWidget{})
}
