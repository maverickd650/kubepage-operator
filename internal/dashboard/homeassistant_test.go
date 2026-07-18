package dashboard

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const testHomeassistantToken = "haTok"

// homeassistantMockHandler answers every POST /api/template call by picking
// a canned plain-text response based on which upstream entity domain the
// request's template mentions (person/light/switch), in the same order
// Poll issues its three calls. It records every Authorization header and
// request path/method seen.
func homeassistantMockHandler(peopleResp, lightsResp, switchesResp string, gotAuths *[]string, gotPaths *[]string, gotMethods *[]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*gotAuths = append(*gotAuths, r.Header.Get("Authorization"))
		*gotPaths = append(*gotPaths, r.URL.Path)
		*gotMethods = append(*gotMethods, r.Method)

		var body struct {
			Template string `json:"template"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)

		w.WriteHeader(http.StatusOK)
		switch {
		case strings.Contains(body.Template, "states.person"):
			_, _ = io.WriteString(w, peopleResp)
		case strings.Contains(body.Template, "states.light"):
			_, _ = io.WriteString(w, lightsResp)
		case strings.Contains(body.Template, "states.switch"):
			_, _ = io.WriteString(w, switchesResp)
		}
	}
}

func TestHomeassistantWidgetPoll(t *testing.T) {
	var gotAuths, gotPaths, gotMethods []string
	srv := httptest.NewServer(homeassistantMockHandler("2 / 4\n", "5 / 12", "1 / 3", &gotAuths, &gotPaths, &gotMethods))
	defer srv.Close()

	got, err := (homeassistantWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{testSecretField: testHomeassistantToken},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{
		{Label: labelPeopleHome, Value: "2 / 4"},
		{Label: labelLightsOn, Value: "5 / 12"},
		{Label: labelSwitchesOn, Value: "1 / 3"},
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
	if len(gotAuths) != 3 {
		t.Fatalf("request count = %d, want 3 (people, lights, switches)", len(gotAuths))
	}
	for _, a := range gotAuths {
		if a != "Bearer haTok" {
			t.Errorf("Authorization header = %q, want %q", a, "Bearer haTok")
		}
	}
	for _, p := range gotPaths {
		if p != "/api/template" {
			t.Errorf("request path = %q, want /api/template", p)
		}
	}
	for _, m := range gotMethods {
		if m != http.MethodPost {
			t.Errorf("request method = %q, want POST", m)
		}
	}
}

func TestHomeassistantWidgetPollNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	got, err := (homeassistantWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{testSecretField: testHomeassistantToken},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: testHTTP401}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

// TestHomeassistantWidgetPollSecondCallFails exercises a mid-flow failure:
// the first (people) template call succeeds but the second (lights) fails,
// proving Poll bails out on the first non-200 rather than issuing all three
// calls unconditionally.
func TestHomeassistantWidgetPollSecondCallFails(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "2 / 4")
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	got, err := (homeassistantWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{testSecretField: testHomeassistantToken},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: testHTTP500}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
	if calls != 2 {
		t.Errorf("request count = %d, want 2 (stopped after the second call failed)", calls)
	}
}

func TestHomeassistantWidgetPollMissingURL(t *testing.T) {
	if _, err := (homeassistantWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestHomeassistantWidgetPollUnreachable(t *testing.T) {
	got, err := (homeassistantWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: testUnreachableAddr})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestHomeassistantWidgetSample(t *testing.T) {
	got := (homeassistantWidget{}).Sample(WidgetConfig{})
	if len(got) != 3 || got[0].Label != labelPeopleHome || got[1].Label != labelLightsOn || got[2].Label != labelSwitchesOn {
		t.Errorf("Sample() = %+v, want People Home/Lights On/Switches On fields", got)
	}
	assertSampleDeterministic(t, homeassistantWidget{})
}
