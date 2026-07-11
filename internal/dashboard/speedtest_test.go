package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestSpeedtestWidgetPollV1(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"download":94.3,"upload":11.2,"ping":8}}`))
	}))
	defer srv.Close()

	got, err := (speedtestWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{URL: srv.URL})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{
		{Label: labelDownload, Value: speedtestSampleDownload},
		{Label: labelUpload, Value: speedtestSampleUpload},
		{Label: labelPing, Value: speedtestSamplePing},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
	if gotPath != "/api/speedtest/latest" {
		t.Errorf("request path = %q, want %q", gotPath, "/api/speedtest/latest")
	}
	if gotAuth != "" {
		t.Errorf("Authorization header = %q, want empty (v1 needs no key)", gotAuth)
	}
}

func TestSpeedtestWidgetPollV2(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"download":11787500,"upload":1400000,"ping":8}}`))
	}))
	defer srv.Close()

	got, err := (speedtestWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{secretAPIKey: "sttok"},
		Config:  []byte(`{"version":2}`),
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{
		{Label: labelDownload, Value: speedtestSampleDownload},
		{Label: labelUpload, Value: speedtestSampleUpload},
		{Label: labelPing, Value: speedtestSamplePing},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
	if gotPath != "/api/v1/results/latest" {
		t.Errorf("request path = %q, want %q", gotPath, "/api/v1/results/latest")
	}
	if gotAuth != "Bearer sttok" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer sttok")
	}
}

func TestSpeedtestWidgetPollNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	got, err := (speedtestWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{URL: srv.URL})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: testHTTP403}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestSpeedtestWidgetPollMissingURL(t *testing.T) {
	if _, err := (speedtestWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestSpeedtestWidgetPollUnreachable(t *testing.T) {
	got, err := (speedtestWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: testUnreachableAddr})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestSpeedtestWidgetSample(t *testing.T) {
	got := (speedtestWidget{}).Sample(WidgetConfig{})
	if len(got) != 3 || got[0].Label != labelDownload || got[2].Label != labelPing {
		t.Errorf("Sample() = %+v, want Download/Upload/Ping fields", got)
	}
	assertSampleDeterministic(t, speedtestWidget{})
}
