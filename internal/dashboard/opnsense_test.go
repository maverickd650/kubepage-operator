package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

const (
	opnsenseActivityPath  = "/api/diagnostics/activity/getActivity"
	opnsenseInterfacePath = "/api/diagnostics/traffic/interface"
)

func TestOpnsenseWidgetPoll(t *testing.T) {
	var gotUser, gotPass string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, _ = r.BasicAuth()
		w.WriteHeader(http.StatusOK)
		switch r.URL.Path {
		case opnsenseActivityPath:
			_, _ = w.Write([]byte(`{"headers":["a","b","CPU: 5.0% user, 10.0% system, 85.0% idle","Mem: 512M Active, 200M Inact,"]}`))
		case opnsenseInterfacePath:
			_, _ = w.Write([]byte(`{"interfaces":{"wan":{"bytes transmitted":12400000000,"bytes received":84700000000}}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	got, err := (opnsenseWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{secretUsername: testOpnsenseAPIKey, secretPassword: "apisecret"},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	cpuPct := 15
	want := []Field{
		{Label: labelCPU, Value: "15%", Percent: &cpuPct, Highlight: usageHighlight(&cpuPct)},
		{Label: labelMemory, Value: "512M"},
		{Label: labelWANUpload, Value: opnsenseSampleWANUpload},
		{Label: labelWANDownload, Value: "84.7 GB"},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
	if gotUser != testOpnsenseAPIKey || gotPass != "apisecret" {
		t.Errorf("basic auth = (%q, %q), want (%q, %q)", gotUser, gotPass, testOpnsenseAPIKey, "apisecret")
	}
}

func TestOpnsenseWidgetPollConfiguredWAN(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		switch r.URL.Path {
		case opnsenseActivityPath:
			_, _ = w.Write([]byte(`{"headers":[]}`))
		case opnsenseInterfacePath:
			_, _ = w.Write([]byte(`{"interfaces":{"wan2":{"bytes transmitted":1000,"bytes received":2000}}}`))
		}
	}))
	defer srv.Close()

	got, err := (opnsenseWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:    srv.URL,
		Config: []byte(`{"wan":"wan2"}`),
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{
		{Label: labelWANUpload, Value: "1.0 KB"},
		{Label: labelWANDownload, Value: "2.0 KB"},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestOpnsenseWidgetPollMalformedConfig(t *testing.T) {
	if _, err := (opnsenseWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{
		URL:    testExampleURL,
		Config: []byte(`{not valid json`),
	}); err == nil {
		t.Fatal("Poll() expected error for malformed config, got nil")
	}
}

// TestOpnsenseWidgetPollInterfaceRequestFails covers the second
// fetchJSONBasicAuth call's own error path: the activity endpoint succeeds
// but the traffic/interface endpoint doesn't, which must still surface as
// the interface request's own status Field rather than being swallowed.
func TestOpnsenseWidgetPollInterfaceRequestFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case opnsenseActivityPath:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"headers":[]}`))
		case opnsenseInterfacePath:
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer srv.Close()

	got, err := (opnsenseWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{URL: srv.URL})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: testHTTP403}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestOpnsenseWidgetPollNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	got, err := (opnsenseWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{URL: srv.URL})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: testHTTP401}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestOpnsenseWidgetPollMissingURL(t *testing.T) {
	if _, err := (opnsenseWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestOpnsenseWidgetPollUnreachable(t *testing.T) {
	got, err := (opnsenseWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: testUnreachableAddr})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestOpnsenseWidgetSample(t *testing.T) {
	got := (opnsenseWidget{}).Sample(WidgetConfig{})
	if len(got) != 4 || got[0].Label != labelCPU || got[3].Label != labelWANDownload {
		t.Errorf("Sample() = %+v, want CPU/Memory/WAN Upload/WAN Download fields", got)
	}
	assertSampleDeterministic(t, opnsenseWidget{})
}

func TestFormatBytesHumanized(t *testing.T) {
	tests := []struct {
		bytes float64
		want  string
	}{
		{500, "500 B"},
		{1234567, "1.2 MB"},
		{12400000000, opnsenseSampleWANUpload},
	}
	for _, tt := range tests {
		if got := formatBytesHumanized(tt.bytes); got != tt.want {
			t.Errorf("formatBytesHumanized(%v) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}
