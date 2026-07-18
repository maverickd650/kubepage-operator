package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

const (
	unifiTestSiteID  = "site-abc"
	unifiSitesPath   = "/proxy/network/integration/v1/sites"
	unifiDevicesPath = "/proxy/network/integration/v1/sites/" + unifiTestSiteID + "/devices"
	unifiClientsPath = "/proxy/network/integration/v1/sites/" + unifiTestSiteID + "/clients"
	testUnifiAPIKey  = "unifi-api-key"
)

func unifiTestSecrets() map[string]string {
	return map[string]string{"apiKey": testUnifiAPIKey}
}

// unifiMockHandler builds an http.HandlerFunc serving the three Integration
// API calls Poll makes in order (sites, devices, clients), recording every
// X-API-KEY header and request path it sees into gotAPIKeys/gotPaths.
func unifiMockHandler(devicesBody, clientsBody string, gotAPIKeys *[]string, gotPaths *[]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*gotAPIKeys = append(*gotAPIKeys, r.Header.Get("X-API-KEY"))
		if gotPaths != nil {
			*gotPaths = append(*gotPaths, r.URL.RequestURI())
		}
		switch r.URL.Path {
		case unifiSitesPath:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"id":"` + unifiTestSiteID + `","name":"default"}]}`))
		case unifiDevicesPath:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(devicesBody))
		case unifiClientsPath:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(clientsBody))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func TestUnifiWidgetPoll(t *testing.T) {
	var gotAPIKeys, gotPaths []string
	srv := httptest.NewServer(unifiMockHandler(
		`{"data":[{"state":"ONLINE"},{"state":"ONLINE"}],"totalCount":2}`,
		`{"data":[{"type":"WIRED"},{"type":"WIRED"},{"type":"WIRELESS"},{"type":"WIRELESS"},{"type":"WIRELESS"}],"totalCount":5}`,
		&gotAPIKeys, &gotPaths,
	))
	defer srv.Close()

	got, err := (unifiWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: unifiTestSecrets(),
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{
		{Label: labelStatus, Value: statusHealthy},
		{Label: labelLANUsers, Value: "2"},
		{Label: labelWLANUsers, Value: "3"},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
	for _, k := range gotAPIKeys {
		if k != testUnifiAPIKey {
			t.Errorf("X-API-KEY header = %q, want %q", k, testUnifiAPIKey)
		}
	}
	if len(gotAPIKeys) != 3 {
		t.Errorf("request count = %d, want 3 (sites, devices, clients)", len(gotAPIKeys))
	}
	if len(gotPaths) != 3 || gotPaths[2] != unifiClientsPath+"?limit=200" {
		t.Errorf("request paths = %v, want the clients request to carry ?limit=200", gotPaths)
	}
}

func TestUnifiWidgetPollDegradedDevice(t *testing.T) {
	var gotAPIKeys, gotPaths []string
	srv := httptest.NewServer(unifiMockHandler(
		`{"data":[{"state":"ONLINE"},{"state":"OFFLINE"}],"totalCount":2}`,
		`{"data":[],"totalCount":0}`,
		&gotAPIKeys, &gotPaths,
	))
	defer srv.Close()

	got, err := (unifiWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: unifiTestSecrets(),
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{
		{Label: labelStatus, Value: statusDegraded},
		{Label: labelLANUsers, Value: "0"},
		{Label: labelWLANUsers, Value: "0"},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestUnifiWidgetPollNoDevices(t *testing.T) {
	var gotAPIKeys, gotPaths []string
	srv := httptest.NewServer(unifiMockHandler(
		`{"data":[],"totalCount":0}`,
		`{"data":[],"totalCount":0}`,
		&gotAPIKeys, &gotPaths,
	))
	defer srv.Close()

	got, err := (unifiWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: unifiTestSecrets(),
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{
		{Label: labelStatus, Value: statusUnknown},
		{Label: labelLANUsers, Value: "0"},
		{Label: labelWLANUsers, Value: "0"},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestUnifiWidgetPollSiteNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case unifiSitesPath:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"id":"other-id","name":"other-site"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	got, err := (unifiWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: unifiTestSecrets(),
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestUnifiWidgetPollNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	got, err := (unifiWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: unifiTestSecrets(),
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: testHTTP401}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestUnifiWidgetPollInsecureTLS(t *testing.T) {
	var gotAPIKeys, gotPaths []string
	srv := httptest.NewTLSServer(unifiMockHandler(
		`{"data":[{"state":"ONLINE"}],"totalCount":1}`,
		`{"data":[{"type":"WIRED"},{"type":"WIRELESS"}],"totalCount":2}`,
		&gotAPIKeys, &gotPaths,
	))
	defer srv.Close()

	// A plain client that does not trust the test server's self-signed cert,
	// to prove insecureTLS:true makes the widget build its own trusting
	// client rather than depending on the shared one already trusting it.
	plainClient := &http.Client{}

	got, err := (unifiWidget{}).Poll(t.Context(), plainClient, WidgetConfig{
		URL:     srv.URL,
		Secrets: unifiTestSecrets(),
		Config:  []byte(`{"insecureTLS":true}`),
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{
		{Label: labelStatus, Value: statusHealthy},
		{Label: labelLANUsers, Value: "1"},
		{Label: labelWLANUsers, Value: "1"},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestUnifiWidgetPollTLSVerificationFailsWithoutOptIn(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	plainClient := &http.Client{}

	got, err := (unifiWidget{}).Poll(t.Context(), plainClient, WidgetConfig{
		URL:     srv.URL,
		Secrets: unifiTestSecrets(),
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestUnifiWidgetPollDevicesNon200(t *testing.T) {
	// Sites resolves fine, but the devices call returns a non-200 — the
	// mid-flow doJSONRequest surfaces it as an HTTP status Field.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case unifiSitesPath:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"id":"` + unifiTestSiteID + `","name":"default"}]}`))
		case unifiDevicesPath:
			w.WriteHeader(http.StatusUnauthorized)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	got, err := (unifiWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: unifiTestSecrets(),
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: testHTTP401}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestUnifiWidgetPollInvalidConfig(t *testing.T) {
	if _, err := (unifiWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{
		URL:     testUnreachableAddr,
		Secrets: unifiTestSecrets(),
		Config:  []byte(`{not valid json`),
	}); err == nil {
		t.Fatal("Poll() expected error for malformed config, got nil")
	}
}

func TestUnifiWidgetPollMissingURL(t *testing.T) {
	if _, err := (unifiWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{
		Secrets: unifiTestSecrets(),
	}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestUnifiWidgetPollMissingAPIKey(t *testing.T) {
	if _, err := (unifiWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: testUnreachableAddr}); err == nil {
		t.Fatal("Poll() expected error for missing secrets.apiKey, got nil")
	}
}

func TestUnifiWidgetPollUnreachable(t *testing.T) {
	got, err := (unifiWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{
		URL:     testUnreachableAddr,
		Secrets: unifiTestSecrets(),
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestUnifiWidgetSample(t *testing.T) {
	got := (unifiWidget{}).Sample(WidgetConfig{})
	if len(got) != 3 || got[0].Label != labelStatus || got[1].Label != labelLANUsers || got[2].Label != labelWLANUsers {
		t.Errorf("Sample() = %+v, want Status/LAN Users/WLAN Users fields", got)
	}
	assertSampleDeterministic(t, unifiWidget{})
}
