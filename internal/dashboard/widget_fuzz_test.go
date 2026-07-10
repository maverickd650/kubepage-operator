package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fuzzSecretValue is a placeholder value for every widget's expected
// secret fields; the fuzz target only exercises response parsing, not
// authentication, so a single shared value is fine.
const fuzzSecretValue = "fuzz"

func FuzzWidgetParsers(f *testing.F) {
	seeds := [][]byte{
		// grafana
		[]byte(`{"database":"ok","version":"10.0.0"}`),
		// plex
		[]byte(`{"MediaContainer":{"size":3}}`),
		// truenas
		[]byte(`{"version":"TrueNAS-SCALE-23.10.1","uptime_seconds":266461}`),
		// prometheus
		[]byte(`{"status":"success","data":{"activeTargets":[{"health":"up"},{"health":"down"}]}}`),
		// homeassistant
		[]byte(`{"version":"2024.1.0","location_name":"Home"}`),
		// customapi nested
		[]byte(`{"status":"ok","disk":{"used":42.5}}`),
		// customapi array
		[]byte(`{"items":[{"name":"alpha"},{"name":"beta"}]}`),
		// longhorn
		[]byte(`{"data":[{"name":"node1","diskStatus":{"disk1":{"storageMaximum":1000000000,"storageAvailable":500000000}}}]}`),
		// mealie
		[]byte(`{"totalRecipes":42,"totalUsers":3,"totalCategories":5,"totalTags":10,"totalTools":2}`),
		// linkwarden
		[]byte(`{"response":[{"id":1,"name":"test"}]}`),
		// glances
		[]byte(`[{"key":"cpu_percent","value":42.5}]`),
		// stash graphql
		[]byte(`{"data":{"stats":{"scene_count":100,"image_count":200,"gallery_count":50}}}`),
		// openmeteo
		[]byte(`{"current":{"temperature_2m":22.5,"weather_code":0}}`),
		// openweathermap
		[]byte(`{"main":{"temp":295.15},"weather":[{"description":"clear sky","icon":"01d"}]}`),
		// paperlessngx
		[]byte(`{"count":1234}`),
		// cloudflared
		[]byte(`{"success":true,"result":[{"id":"tunnel1","status":"healthy"}]}`),
		// prometheusmetric
		[]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1234567890,"42.5"]}]}}`),
		// malformed JSON
		[]byte(`{not valid json`),
		// empty
		[]byte(``),
		// null
		[]byte(`null`),
		// array at top level
		[]byte(`[1,2,3]`),
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(data)
		}))
		defer srv.Close()

		cfg := WidgetConfig{
			URL: srv.URL,
			Secrets: map[string]string{
				testSecretField:     fuzzSecretValue,
				secretAPIKey:        fuzzSecretValue,
				secretPassword:      fuzzSecretValue,
				unifiSecretUsername: fuzzSecretValue,
			},
			Config: json.RawMessage(`{"fields":[{"path":"status"}],"slug":"fuzz","endpointId":1}`),
		}

		for _, wtype := range RegisteredTypes() {
			w, ok := Lookup(wtype)
			if !ok {
				continue
			}
			// ClusterWidgets (kubemetrics) have a no-op Poll; safe to call.
			// Both return values are intentionally discarded: the fuzz target
			// only asserts Poll never panics, and errors are an expected,
			// valid outcome for arbitrary/malformed input.
			_, _ = w.Poll(t.Context(), srv.Client(), cfg)
		}
	})
}
