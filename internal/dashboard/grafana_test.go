package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// grafanaTestResponse is one canned upstream response for a single Grafana
// API path in newGrafanaTestServer's routing table.
type grafanaTestResponse struct {
	statusCode int
	body       string
}

const grafanaTestStatsBody = `{"dashboards":24,"datasources":6,"alerts":18}`

func grafanaTestFields(triggered string) []Field {
	return []Field{
		{Label: labelDashboards, Value: "24"},
		{Label: labelDatasources, Value: "6"},
		{Label: labelTotalAlerts, Value: "18"},
		{Label: labelAlertsTriggered, Value: triggered},
	}
}

func newGrafanaTestServer(t *testing.T, responses map[string]grafanaTestResponse, gotAuth map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth[r.URL.Path] = r.Header.Get("Authorization")
		resp, ok := responses[r.URL.Path]
		if !ok {
			t.Errorf("unexpected request path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(resp.statusCode)
		_, _ = w.Write([]byte(resp.body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestGrafanaWidgetPoll(t *testing.T) {
	tests := map[string]struct {
		responses map[string]grafanaTestResponse
		want      []Field
	}{
		"stats with legacy alerting alerts": {
			responses: map[string]grafanaTestResponse{
				grafanaStatsPath:        {http.StatusOK, grafanaTestStatsBody},
				grafanaLegacyAlertsPath: {http.StatusOK, `[{"state":"alerting"},{"state":"ok"},{"state":"alerting"}]`},
			},
			want: grafanaTestFields("2"),
		},
		"legacy alerts endpoint gone, alertmanager fallback": {
			responses: map[string]grafanaTestResponse{
				grafanaStatsPath:        {http.StatusOK, grafanaTestStatsBody},
				grafanaLegacyAlertsPath: {http.StatusNotFound, `{}`},
				grafanaAlertmanagerPath: {http.StatusOK, `[{"labels":{}},{"labels":{}},{"labels":{}}]`},
			},
			want: grafanaTestFields("3"),
		},
		"legacy alerts empty, alertmanager fallback": {
			responses: map[string]grafanaTestResponse{
				grafanaStatsPath:        {http.StatusOK, grafanaTestStatsBody},
				grafanaLegacyAlertsPath: {http.StatusOK, `[]`},
				grafanaAlertmanagerPath: {http.StatusOK, `[{"labels":{}}]`},
			},
			want: grafanaTestFields("1"),
		},
		"legacy alerts empty and alertmanager failing shows zero": {
			responses: map[string]grafanaTestResponse{
				grafanaStatsPath:        {http.StatusOK, grafanaTestStatsBody},
				grafanaLegacyAlertsPath: {http.StatusOK, `[]`},
				grafanaAlertmanagerPath: {http.StatusInternalServerError, ``},
			},
			want: grafanaTestFields("0"),
		},
		"both alert endpoints failing surfaces the failure": {
			responses: map[string]grafanaTestResponse{
				grafanaStatsPath:        {http.StatusOK, grafanaTestStatsBody},
				grafanaLegacyAlertsPath: {http.StatusInternalServerError, ``},
				grafanaAlertmanagerPath: {http.StatusInternalServerError, ``},
			},
			want: []Field{{Label: labelStatus, Value: testHTTP500}},
		},
		testCaseNon200: {
			responses: map[string]grafanaTestResponse{
				grafanaStatsPath: {http.StatusInternalServerError, ``},
			},
			want: []Field{{Label: labelStatus, Value: testHTTP500}},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			gotAuth := map[string]string{}
			srv := newGrafanaTestServer(t, tc.responses, gotAuth)

			got, err := (grafanaWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
				URL:     srv.URL,
				Secrets: map[string]string{testSecretField: "tok123"},
			})
			if err != nil {
				t.Fatalf("Poll() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(tc.want, got) {
				t.Errorf("Poll() = %+v, want %+v", got, tc.want)
			}
			for path, auth := range gotAuth {
				if auth != "Bearer tok123" {
					t.Errorf("Authorization header on %s = %q, want %q", path, auth, "Bearer tok123")
				}
			}
		})
	}
}

func TestGrafanaWidgetPollBasicAuth(t *testing.T) {
	gotAuth := map[string]string{}
	srv := newGrafanaTestServer(t, map[string]grafanaTestResponse{
		grafanaStatsPath:        {http.StatusOK, grafanaTestStatsBody},
		grafanaLegacyAlertsPath: {http.StatusOK, `[{"state":"alerting"}]`},
	}, gotAuth)

	got, err := (grafanaWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{secretUsername: testAdminUser, secretPassword: "hunter2"},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	if want := grafanaTestFields("1"); !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}

	req := &http.Request{Header: http.Header{}}
	req.SetBasicAuth(testAdminUser, "hunter2")
	wantAuth := req.Header.Get("Authorization")
	for path, auth := range gotAuth {
		if auth != wantAuth {
			t.Errorf("Authorization header on %s = %q, want %q", path, auth, wantAuth)
		}
	}
}

func TestGrafanaWidgetPollMissingURL(t *testing.T) {
	if _, err := (grafanaWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestGrafanaWidgetPollUnreachable(t *testing.T) {
	got, err := (grafanaWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: testUnreachableAddr})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestGrafanaWidgetSample(t *testing.T) {
	got := (grafanaWidget{}).Sample(WidgetConfig{})
	want := grafanaTestFields("2")
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Sample() = %+v, want %+v", got, want)
	}
	assertSampleDeterministic(t, grafanaWidget{})
}
