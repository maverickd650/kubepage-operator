package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestAdguardWidgetPoll(t *testing.T) {
	var gotUser, gotPass string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, _ = r.BasicAuth()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"num_dns_queries":48213,"num_blocked_filtering":9127}`))
	}))
	defer srv.Close()

	got, err := (adguardWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{secretUsername: testAdminUser, secretPassword: "pw"},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{
		{Label: labelQueries, Value: "48213"},
		{Label: labelBlocked, Value: "9127 (18.9%)"},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
	if gotUser != testAdminUser || gotPass != "pw" {
		t.Errorf("basic auth = (%q, %q), want (%q, %q)", gotUser, gotPass, testAdminUser, "pw")
	}
}

func TestAdguardWidgetPollNoQueries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"num_dns_queries":0,"num_blocked_filtering":0}`))
	}))
	defer srv.Close()

	got, err := (adguardWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{URL: srv.URL})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{
		{Label: labelQueries, Value: "0"},
		{Label: labelBlocked, Value: "0 (0.0%)"},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestAdguardWidgetPollNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	got, err := (adguardWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{URL: srv.URL})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: testHTTP403}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestAdguardWidgetPollMissingURL(t *testing.T) {
	if _, err := (adguardWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestAdguardWidgetPollUnreachable(t *testing.T) {
	got, err := (adguardWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: testUnreachableAddr})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestAdguardWidgetSample(t *testing.T) {
	got := (adguardWidget{}).Sample(WidgetConfig{})
	if len(got) != 2 || got[0].Label != labelQueries || got[1].Label != labelBlocked {
		t.Errorf("Sample() = %+v, want Queries/Blocked fields", got)
	}
	assertSampleDeterministic(t, adguardWidget{})
}
