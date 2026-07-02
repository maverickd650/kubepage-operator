package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestOpenWeatherMapWidgetPoll(t *testing.T) {
	tests := map[string]struct {
		config     string
		secrets    map[string]string
		response   string
		statusCode int
		want       []Field
		wantErr    bool
	}{
		"metric clear": {
			config:     testCoordsConfig,
			secrets:    map[string]string{openWeatherMapSecretAPIKey: testAPIKey},
			response:   `{"main":{"temp":12.3},"weather":[{"main":"Clear"}]}`,
			statusCode: http.StatusOK,
			want:       []Field{{Label: labelWeather, Value: "12.3°C"}, {Label: labelConditions, Value: "Clear"}},
		},
		"imperial with label": {
			config:     `{"latitude":40.7,"longitude":-74,"units":"imperial","label":"NYC"}`,
			secrets:    map[string]string{openWeatherMapSecretAPIKey: testAPIKey},
			response:   `{"main":{"temp":61},"weather":[{"main":"Rain"}]}`,
			statusCode: http.StatusOK,
			want:       []Field{{Label: "NYC", Value: "61°F"}, {Label: labelConditions, Value: "Rain"}},
		},
		testCaseNon200: {
			config:     testCoordsConfig,
			secrets:    map[string]string{openWeatherMapSecretAPIKey: testAPIKey},
			statusCode: http.StatusInternalServerError,
			want:       []Field{{Label: labelStatus, Value: testHTTP500}},
		},
		"missing coords": {
			config:  `{}`,
			secrets: map[string]string{openWeatherMapSecretAPIKey: testAPIKey},
			wantErr: true,
		},
		"missing api key": {
			config:  testCoordsConfig,
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.URL.Query().Get("appid"); tc.statusCode != 0 && got != tc.secrets[openWeatherMapSecretAPIKey] {
					t.Errorf("appid = %q, want %q", got, tc.secrets[openWeatherMapSecretAPIKey])
				}
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.response))
			}))
			defer srv.Close()

			got, err := (openWeatherMapWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
				URL:     srv.URL,
				Config:  []byte(tc.config),
				Secrets: tc.secrets,
			})
			if tc.wantErr {
				if err == nil {
					t.Fatal("Poll() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Poll() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(tc.want, got) {
				t.Errorf("Poll() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestOpenWeatherMapWidgetPollUnreachable(t *testing.T) {
	got, err := (openWeatherMapWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{
		URL:     testUnreachableAddr,
		Config:  []byte(testCoordsConfig),
		Secrets: map[string]string{openWeatherMapSecretAPIKey: testAPIKey},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}
