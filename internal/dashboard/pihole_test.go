package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

const (
	piholeTestSID      = "test-sid-123"
	piholeTestPassword = "app-pw"
)

func TestPiholeWidgetPoll(t *testing.T) {
	var gotPassword, gotSID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/auth":
			var body struct {
				Password string `json:"password"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			gotPassword = body.Password
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"session":{"valid":true,"sid":"` + piholeTestSID + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/stats/summary":
			gotSID = r.Header.Get("X-FTL-SID")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"queries":{"total":63482,"blocked":12904,"percent_blocked":20.33}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	got, err := (piholeWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{secretPassword: piholeTestPassword},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{
		{Label: labelQueries, Value: "63482"},
		{Label: labelBlocked, Value: "12904"},
		{Label: labelBlockPercent, Value: "20.3%"},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
	if gotPassword != piholeTestPassword {
		t.Errorf("auth password = %q, want %q", gotPassword, piholeTestPassword)
	}
	if gotSID != piholeTestSID {
		t.Errorf("X-FTL-SID header = %q, want %q", gotSID, piholeTestSID)
	}
}

func TestPiholeWidgetPollInvalidSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"session":{"valid":false}}`))
	}))
	defer srv.Close()

	got, err := (piholeWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{secretPassword: "wrong"},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestPiholeWidgetPollNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	got, err := (piholeWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{secretPassword: piholeTestPassword},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: testHTTP500}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestPiholeWidgetPollMissingURL(t *testing.T) {
	if _, err := (piholeWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{Secrets: map[string]string{secretPassword: "x"}}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestPiholeWidgetPollMissingPassword(t *testing.T) {
	if _, err := (piholeWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: testUnreachableAddr}); err == nil {
		t.Fatal("Poll() expected error for missing password, got nil")
	}
}

func TestPiholeWidgetPollUnreachable(t *testing.T) {
	got, err := (piholeWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{
		URL:     testUnreachableAddr,
		Secrets: map[string]string{secretPassword: piholeTestPassword},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

// TestPiholeWidgetPollStatsRequestFails covers the second request's own
// error path (line ~90 of pihole.go): auth succeeds, but the stats/summary
// call itself fails (non-200), which must surface as this poll's result
// rather than the auth response's fields.
func TestPiholeWidgetPollStatsRequestFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/auth":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"session":{"valid":true,"sid":"` + piholeTestSID + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/stats/summary":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	got, err := (piholeWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{secretPassword: piholeTestPassword},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: testHTTP500}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v (the stats request's own failure, not the auth response)", got, want)
	}
}

func TestPiholeWidgetSample(t *testing.T) {
	got := (piholeWidget{}).Sample(WidgetConfig{})
	if len(got) != 3 || got[0].Label != labelQueries || got[1].Label != labelBlocked || got[2].Label != labelBlockPercent {
		t.Errorf("Sample() = %+v, want Queries/Blocked/Block %% fields", got)
	}
	assertSampleDeterministic(t, piholeWidget{})
}
