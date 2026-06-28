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

func init() {
	Register("openmeteo", &openMeteoWidget{})
}

const (
	openMeteoDefaultBase = "https://api.open-meteo.com"
	condClear            = "Clear"
	condRain             = "Rain"
	condThunderstorm     = "Thunderstorm"
)

// openMeteoWidget is a header InfoWidget that shows current weather from the
// keyless Open-Meteo forecast API. Config is a JSON object:
// {"latitude": <num>, "longitude": <num>, "units": "metric"|"imperial",
// "label": "<display label>"}. units defaults to metric; label defaults to
// "Weather". cfg.URL optionally overrides the API base (used by tests).
type openMeteoWidget struct{}

type openMeteoConfig struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Units     string  `json:"units"`
	Label     string  `json:"label"`
}

type openMeteoResponse struct {
	CurrentWeather struct {
		Temperature float64 `json:"temperature"`
		WeatherCode int     `json:"weathercode"`
	} `json:"current_weather"`
}

func (openMeteoWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	if len(cfg.Config) == 0 {
		return nil, errors.New("openmeteo widget: config.latitude/longitude are required")
	}
	var c openMeteoConfig
	if err := json.Unmarshal(cfg.Config, &c); err != nil {
		return nil, fmt.Errorf("decoding widget config: %w", err)
	}
	if c.Latitude == 0 && c.Longitude == 0 {
		return nil, errors.New("openmeteo widget: config.latitude/longitude are required")
	}

	label := c.Label
	if label == "" {
		label = labelWeather
	}
	tempUnit := "celsius"
	tempSuffix := "°C"
	if c.Units == "imperial" {
		tempUnit = "fahrenheit"
		tempSuffix = "°F"
	}

	base := cfg.URL
	if base == "" {
		base = openMeteoDefaultBase
	}
	q := url.Values{}
	q.Set("latitude", strconv.FormatFloat(c.Latitude, 'f', -1, 64))
	q.Set("longitude", strconv.FormatFloat(c.Longitude, 'f', -1, 64))
	q.Set("current_weather", "true")
	q.Set("temperature_unit", tempUnit)
	endpoint := strings.TrimRight(base, "/") + "/v1/forecast?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return []Field{{Label: labelStatus, Value: statusUnreach}}, nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return []Field{{Label: labelStatus, Value: fmt.Sprintf("HTTP %d", resp.StatusCode)}}, nil
	}

	var parsed openMeteoResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decoding forecast response: %w", err)
	}

	temp := strconv.FormatFloat(parsed.CurrentWeather.Temperature, 'f', -1, 64) + tempSuffix
	return []Field{
		{Label: label, Value: temp},
		{Label: labelConditions, Value: weatherCondition(parsed.CurrentWeather.WeatherCode)},
	}, nil
}

// weatherCondition maps a WMO weather-interpretation code (as returned by
// Open-Meteo) to a short human-readable condition.
func weatherCondition(code int) string {
	switch {
	case code == 0:
		return condClear
	case code <= 3:
		return "Partly cloudy"
	case code <= 48:
		return "Fog"
	case code <= 57:
		return "Drizzle"
	case code <= 67:
		return condRain
	case code <= 77:
		return "Snow"
	case code <= 82:
		return "Rain showers"
	case code <= 86:
		return "Snow showers"
	case code <= 99:
		return condThunderstorm
	default:
		return statusUnknown
	}
}
