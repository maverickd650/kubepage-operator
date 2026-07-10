package dashboard

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const widgetTypeOpenMeteo = "openmeteo"

func init() {
	Register(widgetTypeOpenMeteo, &openMeteoWidget{})
}

const (
	openMeteoDefaultBase = "https://api.open-meteo.com"
	condClear            = "Clear"
	condPartlyCloudy     = "Partly cloudy"
	condFog              = "Fog"
	condDrizzle          = "Drizzle"
	condRain             = "Rain"
	condRainShowers      = "Rain showers"
	condSnow             = "Snow"
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

	label, tempSuffix := weatherLabelAndSuffix(c.Label, c.Units)
	tempUnit := "celsius"
	if c.Units == unitsImperial {
		tempUnit = "fahrenheit"
	}

	base := cmp.Or(cfg.URL, openMeteoDefaultBase)
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
	var parsed openMeteoResponse
	if fields, err := doJSONRequest(httpClient, req, &parsed); fields != nil || err != nil {
		return fields, err
	}

	temp := strconv.FormatFloat(parsed.CurrentWeather.Temperature, 'f', -1, 64) + tempSuffix
	return []Field{
		{Label: label, Value: temp},
		{Label: labelConditions, Value: weatherCondition(parsed.CurrentWeather.WeatherCode)},
	}, nil
}

// Sample honors cfg.Config's label/units overrides the same way Poll does.
func (openMeteoWidget) Sample(cfg WidgetConfig) []Field {
	var c openMeteoConfig
	if len(cfg.Config) > 0 {
		_ = json.Unmarshal(cfg.Config, &c)
	}
	label, tempSuffix := weatherLabelAndSuffix(c.Label, c.Units)
	return []Field{
		{Label: label, Value: "18" + tempSuffix},
		{Label: labelConditions, Value: condClear},
	}
}

// weatherCondition maps a WMO weather-interpretation code (as returned by
// Open-Meteo) to a short human-readable condition.
func weatherCondition(code int) string {
	switch {
	case code == 0:
		return condClear
	case code <= 3:
		return condPartlyCloudy
	case code <= 48:
		return condFog
	case code <= 57:
		return condDrizzle
	case code <= 67:
		return condRain
	case code <= 77:
		return condSnow
	case code <= 82:
		return condRainShowers
	case code <= 86:
		return "Snow showers"
	case code <= 99:
		return condThunderstorm
	default:
		return statusUnknown
	}
}
