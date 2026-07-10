package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

const argocdActivityFixture = `{"items":[
	{"status":{"sync":{"status":"Synced"},"health":{"status":"Healthy"}}},
	{"status":{"sync":{"status":"Synced"},"health":{"status":"Progressing"}}},
	{"status":{"sync":{"status":"OutOfSync"},"health":{"status":"Degraded"}}}
]}`

func TestArgocdWidgetPoll(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(argocdActivityFixture))
	}))
	defer srv.Close()

	got, err := (argocdWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{testSecretField: "argotok"},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{
		{Label: labelApps, Value: "3"},
		{Label: labelSynced, Value: "2"},
		{Label: labelHealthy, Value: "1"},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
	if gotAuth != "Bearer argotok" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer argotok")
	}
}

func TestArgocdWidgetPollNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	got, err := (argocdWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{URL: srv.URL})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: testHTTP401}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestArgocdWidgetPollMissingURL(t *testing.T) {
	if _, err := (argocdWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestArgocdWidgetPollUnreachable(t *testing.T) {
	got, err := (argocdWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: testUnreachableAddr})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestArgocdWidgetSample(t *testing.T) {
	got := (argocdWidget{}).Sample(WidgetConfig{})
	if len(got) != 3 || got[0].Label != labelApps || got[1].Label != labelSynced || got[2].Label != labelHealthy {
		t.Errorf("Sample() = %+v, want Apps/Synced/Healthy fields", got)
	}
	assertSampleDeterministic(t, argocdWidget{})
}
