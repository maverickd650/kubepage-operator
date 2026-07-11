package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestNextcloudWidgetPollNCToken(t *testing.T) {
	var gotToken, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get(headerNCToken)
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ocs":{"data":{"nextcloud":{"system":{"cpuload":[0.42,0.3,0.2],"mem_total":"1000","mem_free":"600","freespace":872415232}},"activeUsers":{"last24hours":17}}}}`))
	}))
	defer srv.Close()

	got, err := (nextcloudWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{secretKeyNCToken: "nctok"},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	memPct := 40
	want := []Field{
		{Label: labelCPULoad, Value: "0.42%"},
		{Label: labelMemoryUsage, Value: "40%", Percent: &memPct, Highlight: usageHighlight(&memPct)},
		{Label: labelFreeSpace, Value: "0.8 GiB"},
		{Label: labelActiveUsers, Value: "17"},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
	if gotToken != "nctok" {
		t.Errorf("NC-Token header = %q, want %q", gotToken, "nctok")
	}
	if gotAuth != "" {
		t.Errorf("Authorization header = %q, want empty (key auth should not also send basic auth)", gotAuth)
	}
}

func TestNextcloudWidgetPollBasicAuthFallback(t *testing.T) {
	var gotUser, gotPass string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, _ = r.BasicAuth()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ocs":{"data":{"nextcloud":{"system":{"cpuload":[],"mem_total":0,"mem_free":0,"freespace":0}},"activeUsers":{"last24hours":0}}}}`))
	}))
	defer srv.Close()

	_, err := (nextcloudWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{secretUsername: testAdminUser, secretPassword: "pw"},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	if gotUser != testAdminUser || gotPass != "pw" {
		t.Errorf("basic auth = (%q, %q), want (%q, %q)", gotUser, gotPass, testAdminUser, "pw")
	}
}

func TestNextcloudWidgetPollNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	got, err := (nextcloudWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{URL: srv.URL})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: testHTTP401}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestNextcloudWidgetPollMissingURL(t *testing.T) {
	if _, err := (nextcloudWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestNextcloudWidgetPollUnreachable(t *testing.T) {
	got, err := (nextcloudWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: testUnreachableAddr})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestNextcloudWidgetSample(t *testing.T) {
	got := (nextcloudWidget{}).Sample(WidgetConfig{})
	if len(got) != 4 || got[0].Label != labelCPULoad || got[3].Label != labelActiveUsers {
		t.Errorf("Sample() = %+v, want CPU Load/.../Active Users fields", got)
	}
	assertSampleDeterministic(t, nextcloudWidget{})
}
