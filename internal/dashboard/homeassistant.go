package dashboard

import (
	"context"
	"net/http"
)

func init() {
	Register("homeassistant", &homeassistantWidget{})
}

// homeassistantWidget polls Home Assistant's /api/config endpoint for
// version and reachability. Secrets["token"] is a Home Assistant long-lived
// access token, sent as a Bearer token.
type homeassistantWidget struct{}

type homeassistantConfigResponse struct {
	Version      string `json:"version"`
	LocationName string `json:"location_name"`
}

func (homeassistantWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	headers := map[string]string{}
	if token := cfg.Secrets["token"]; token != "" {
		headers["Authorization"] = "Bearer " + token
	}

	var parsed homeassistantConfigResponse
	if fields, err := fetchJSON(ctx, httpClient, cfg, "homeassistant", "/api/config", headers, &parsed); fields != nil || err != nil {
		return fields, err
	}

	return []Field{
		{Label: labelStatus, Value: statusHealthy},
		{Label: labelVersion, Value: parsed.Version},
	}, nil
}

func (homeassistantWidget) Sample(WidgetConfig) []Field {
	return []Field{
		{Label: labelStatus, Value: statusHealthy},
		{Label: labelVersion, Value: "2024.6.0"},
	}
}
