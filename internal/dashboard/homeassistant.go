package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
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
	if cfg.URL == "" {
		return nil, errors.New("homeassistant widget: url is required")
	}

	endpoint := strings.TrimRight(cfg.URL, "/") + "/api/config"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	if token := cfg.Secrets["token"]; token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return []Field{{Label: labelStatus, Value: statusUnreach}}, nil
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return []Field{{Label: labelStatus, Value: fmt.Sprintf("HTTP %d", resp.StatusCode)}}, nil
	}

	var parsed homeassistantConfigResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decoding config response: %w", err)
	}

	return []Field{
		{Label: labelStatus, Value: statusHealthy},
		{Label: labelVersion, Value: parsed.Version},
	}, nil
}
