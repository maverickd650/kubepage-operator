package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
)

const (
	unifiHealthFixture = `{"data":[{"subsystem":"wlan","status":"ok","num_user":5},{"subsystem":"lan","status":"ok","num_user":2}]}`
	unifiUDMLoginPath  = "/api/auth/login"
	unifiClassicLogin  = "/api/login"
	unifiUDMHealthPath = "/proxy/network/api/s/default/stat/health"
	unifiClassicHealth = "/api/s/default/stat/health"
	testUnifiUsername  = "admin"
	testUnifiPassword  = "pw"
	testUnifiUDMToken  = "udm-token"
)

func unifiTestCreds() map[string]string {
	return map[string]string{"username": testUnifiUsername, "password": testUnifiPassword}
}

func TestUnifiWidgetPollUDM(t *testing.T) {
	var loginHits, healthHits int32
	var gotCookie, gotCSRF string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == unifiUDMLoginPath:
			atomic.AddInt32(&loginHits, 1)
			http.SetCookie(w, &http.Cookie{Name: "TOKEN", Value: testUnifiUDMToken})
			w.Header().Set("X-Csrf-Token", "csrf-abc")
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == unifiUDMHealthPath:
			atomic.AddInt32(&healthHits, 1)
			if c, err := r.Cookie("TOKEN"); err == nil {
				gotCookie = c.Value
			}
			gotCSRF = r.Header.Get("X-Csrf-Token")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(unifiHealthFixture))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	got, err := (unifiWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: unifiTestCreds(),
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{
		{Label: labelStatus, Value: statusHealthy},
		{Label: labelClients, Value: "7"},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
	if atomic.LoadInt32(&loginHits) != 1 {
		t.Errorf("login hits = %d, want 1", loginHits)
	}
	if atomic.LoadInt32(&healthHits) != 1 {
		t.Errorf("health hits = %d, want 1", healthHits)
	}
	if gotCookie != testUnifiUDMToken {
		t.Errorf("cookie sent on health request = %q, want %q", gotCookie, testUnifiUDMToken)
	}
	if gotCSRF != "csrf-abc" {
		t.Errorf("CSRF header sent on health request = %q, want %q", gotCSRF, "csrf-abc")
	}
}

func TestUnifiWidgetPollClassicFallback(t *testing.T) {
	var udmLoginHits, classicLoginHits, healthHits int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == unifiUDMLoginPath:
			atomic.AddInt32(&udmLoginHits, 1)
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == unifiClassicLogin:
			atomic.AddInt32(&classicLoginHits, 1)
			http.SetCookie(w, &http.Cookie{Name: "unifises", Value: "classic-session"})
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == unifiClassicHealth:
			atomic.AddInt32(&healthHits, 1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(unifiHealthFixture))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	got, err := (unifiWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: unifiTestCreds(),
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{
		{Label: labelStatus, Value: statusHealthy},
		{Label: labelClients, Value: "7"},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
	if atomic.LoadInt32(&udmLoginHits) != 1 {
		t.Errorf("UDM login hits = %d, want 1", udmLoginHits)
	}
	if atomic.LoadInt32(&classicLoginHits) != 1 {
		t.Errorf("classic login hits = %d, want 1", classicLoginHits)
	}
	if atomic.LoadInt32(&healthHits) != 1 {
		t.Errorf("health hits = %d, want 1", healthHits)
	}
}

func TestUnifiWidgetPollDegradedSubsystem(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case unifiUDMLoginPath:
			w.WriteHeader(http.StatusOK)
		case unifiUDMHealthPath:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"subsystem":"wan","status":"error","num_user":0}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	got, err := (unifiWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: unifiTestCreds(),
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{
		{Label: labelStatus, Value: statusDegraded},
		{Label: labelClients, Value: "0"},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestUnifiWidgetPollInvalidCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	got, err := (unifiWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{"username": testUnifiUsername, "password": "wrong"},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestUnifiWidgetPollSessionReuseAndReloginOn401(t *testing.T) {
	var loginHits, healthHits int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == unifiUDMLoginPath:
			atomic.AddInt32(&loginHits, 1)
			http.SetCookie(w, &http.Cookie{Name: "TOKEN", Value: testUnifiUDMToken})
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == unifiUDMHealthPath:
			n := atomic.AddInt32(&healthHits, 1)
			if n == 2 {
				// Simulate the cached session having expired server-side.
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(unifiHealthFixture))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cfg := WidgetConfig{
		URL:     srv.URL,
		Secrets: unifiTestCreds(),
	}

	if _, err := (unifiWidget{}).Poll(t.Context(), srv.Client(), cfg); err != nil {
		t.Fatalf("first Poll() unexpected error: %v", err)
	}
	if atomic.LoadInt32(&loginHits) != 1 {
		t.Fatalf("after first Poll: login hits = %d, want 1 (session should be cached)", loginHits)
	}

	got, err := (unifiWidget{}).Poll(t.Context(), srv.Client(), cfg)
	if err != nil {
		t.Fatalf("second Poll() unexpected error: %v", err)
	}
	want := []Field{
		{Label: labelStatus, Value: statusHealthy},
		{Label: labelClients, Value: "7"},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
	if atomic.LoadInt32(&loginHits) != 2 {
		t.Errorf("after second Poll: login hits = %d, want 2 (one relogin after the simulated 401)", loginHits)
	}
	if atomic.LoadInt32(&healthHits) != 3 {
		t.Errorf("after second Poll: health hits = %d, want 3 (1 initial + 1 expired + 1 retry)", healthHits)
	}
}

func TestUnifiWidgetPollInsecureTLS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case unifiUDMLoginPath:
			w.WriteHeader(http.StatusOK)
		case unifiUDMHealthPath:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(unifiHealthFixture))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	// A plain client that does not trust the test server's self-signed cert,
	// to prove insecureTLS:true makes the widget build its own trusting
	// client rather than depending on the shared one already trusting it.
	plainClient := &http.Client{}

	got, err := (unifiWidget{}).Poll(t.Context(), plainClient, WidgetConfig{
		URL:     srv.URL,
		Secrets: unifiTestCreds(),
		Config:  []byte(`{"insecureTLS":true}`),
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{
		{Label: labelStatus, Value: statusHealthy},
		{Label: labelClients, Value: "7"},
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
		Secrets: unifiTestCreds(),
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestUnifiWidgetPollMissingURL(t *testing.T) {
	if _, err := (unifiWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{
		Secrets: unifiTestCreds(),
	}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestUnifiWidgetPollMissingCredentials(t *testing.T) {
	if _, err := (unifiWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: testUnreachableAddr}); err == nil {
		t.Fatal("Poll() expected error for missing credentials, got nil")
	}
}
