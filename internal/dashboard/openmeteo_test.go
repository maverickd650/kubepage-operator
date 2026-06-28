package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestOpenMeteoWidgetPoll(t *testing.T) {
	tests := map[string]struct {
		config     string
		response   string
		statusCode int
		want       []Field
		wantErr    bool
	}{
		"metric clear": {
			config:     `{"latitude":51.5,"longitude":-0.12}`,
			response:   `{"current_weather":{"temperature":12.3,"weathercode":0}}`,
			statusCode: http.StatusOK,
			want:       []Field{{Label: labelWeather, Value: "12.3°C"}, {Label: labelConditions, Value: condClear}},
		},
		"imperial rain with label": {
			config:     `{"latitude":40.7,"longitude":-74,"units":"imperial","label":"NYC"}`,
			response:   `{"current_weather":{"temperature":61,"weathercode":63}}`,
			statusCode: http.StatusOK,
			want:       []Field{{Label: "NYC", Value: "61°F"}, {Label: labelConditions, Value: condRain}},
		},
		"thunderstorm": {
			config:     `{"latitude":1,"longitude":1}`,
			response:   `{"current_weather":{"temperature":20,"weathercode":95}}`,
			statusCode: http.StatusOK,
			want:       []Field{{Label: labelWeather, Value: "20°C"}, {Label: labelConditions, Value: condThunderstorm}},
		},
		testCaseNon200: {
			config:     `{"latitude":1,"longitude":1}`,
			statusCode: http.StatusInternalServerError,
			want:       []Field{{Label: labelStatus, Value: testHTTP500}},
		},
		"missing coords": {
			config:  `{}`,
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.response))
			}))
			defer srv.Close()

			got, err := (openMeteoWidget{}).Poll(context.Background(), srv.Client(), WidgetConfig{
				URL:    srv.URL,
				Config: []byte(tc.config),
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

func TestOpenMeteoWidgetPollUnreachable(t *testing.T) {
	got, err := (openMeteoWidget{}).Poll(context.Background(), http.DefaultClient, WidgetConfig{
		URL:    testUnreachableAddr,
		Config: []byte(`{"latitude":1,"longitude":1}`),
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestWeatherCondition(t *testing.T) {
	tests := map[string]struct {
		code int
		want string
	}{
		"clear":         {code: 0, want: condClear},
		"partly cloudy": {code: 2, want: "Partly cloudy"},
		"fog":           {code: 45, want: "Fog"},
		"drizzle":       {code: 51, want: "Drizzle"},
		"rain":          {code: 63, want: condRain},
		"snow":          {code: 73, want: "Snow"},
		"rain showers":  {code: 80, want: "Rain showers"},
		"snow showers":  {code: 85, want: "Snow showers"},
		"thunderstorm":  {code: 95, want: condThunderstorm},
		"unknown":       {code: 100, want: statusUnknown},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := weatherCondition(tc.code); got != tc.want {
				t.Errorf("weatherCondition(%d) = %q, want %q", tc.code, got, tc.want)
			}
		})
	}
}
