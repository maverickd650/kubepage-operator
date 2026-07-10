package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const widgetTypeOpenWeatherMap = "openweathermap"

func init() {
	Register(widgetTypeOpenWeatherMap, &openWeatherMapWidget{})
}

const (
	openWeatherMapDefaultBase  = "https://api.openweathermap.org"
	openWeatherMapSecretAPIKey = "apiKey"

	// sampleWeatherCondition is openWeatherMapWidget.Sample's canned
	// labelConditions value.
	sampleWeatherCondition = "Clouds"
)

// openWeatherMapWidget is a header InfoWidget that shows current weather
// from OpenWeatherMap's Current Weather Data API. Unlike openmeteo it needs
// an API key: Secrets["apiKey"] is required. Config is a JSON object:
// {"latitude": <num>, "longitude": <num>, "units": "metric"|"imperial",
// "label": "<display label>"}. units defaults to metric; label defaults to
// "Weather". cfg.URL optionally overrides the API base (used by tests).
type openWeatherMapWidget struct{}

type openWeatherMapConfig struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Units     string  `json:"units"`
	Label     string  `json:"label"`
}

type openWeatherMapResponse struct {
	Main struct {
		Temp float64 `json:"temp"`
	} `json:"main"`
	Weather []struct {
		Main string `json:"main"`
	} `json:"weather"`
}

func (openWeatherMapWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	if len(cfg.Config) == 0 {
		return nil, errors.New("openweathermap widget: config.latitude/longitude are required")
	}
	var c openWeatherMapConfig
	if err := json.Unmarshal(cfg.Config, &c); err != nil {
		return nil, fmt.Errorf("decoding widget config: %w", err)
	}
	if c.Latitude == 0 && c.Longitude == 0 {
		return nil, errors.New("openweathermap widget: config.latitude/longitude are required")
	}
	apiKey := cfg.Secrets[openWeatherMapSecretAPIKey]
	if apiKey == "" {
		return nil, errors.New("openweathermap widget: secrets.apiKey is required")
	}

	label, tempSuffix := weatherLabelAndSuffix(c.Label, c.Units)
	units := "metric"
	if c.Units == unitsImperial {
		units = unitsImperial
	}

	base := cfg.URL
	if base == "" {
		base = openWeatherMapDefaultBase
	}
	q := url.Values{}
	q.Set("lat", strconv.FormatFloat(c.Latitude, 'f', -1, 64))
	q.Set("lon", strconv.FormatFloat(c.Longitude, 'f', -1, 64))
	q.Set("units", units)
	q.Set("appid", apiKey)
	endpoint := strings.TrimRight(base, "/") + "/data/2.5/weather?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	var parsed openWeatherMapResponse
	if fields, err := doJSONRequest(httpClient, req, &parsed); fields != nil || err != nil {
		return fields, err
	}

	temp := strconv.FormatFloat(parsed.Main.Temp, 'f', -1, 64) + tempSuffix
	conditions := statusUnknown
	if len(parsed.Weather) > 0 && parsed.Weather[0].Main != "" {
		conditions = parsed.Weather[0].Main
	}
	return []Field{
		{Label: label, Value: temp},
		{Label: labelConditions, Value: conditions},
	}, nil
}

// Sample honors cfg.Config's label/units overrides the same way Poll does,
// and never requires secrets.apiKey (sample mode skips secret resolution
// entirely — see Poller.SampleData).
func (openWeatherMapWidget) Sample(cfg WidgetConfig) []Field {
	var c openWeatherMapConfig
	if len(cfg.Config) > 0 {
		_ = json.Unmarshal(cfg.Config, &c)
	}
	label, tempSuffix := weatherLabelAndSuffix(c.Label, c.Units)
	return []Field{
		{Label: label, Value: "21" + tempSuffix},
		{Label: labelConditions, Value: sampleWeatherCondition},
	}
}
