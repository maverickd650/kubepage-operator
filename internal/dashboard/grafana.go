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
	Register("grafana", &grafanaWidget{})
}

// grafanaWidget polls Grafana's unauthenticated /api/health endpoint for
// database/version status. Secrets["token"], if set, is sent as a Bearer
// token (useful behind an auth proxy); /api/health itself needs none.
type grafanaWidget struct{}

type grafanaHealthResponse struct {
	Database string `json:"database"`
	Version  string `json:"version"`
}

func (grafanaWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	if cfg.URL == "" {
		return nil, errors.New("grafana widget: url is required")
	}

	endpoint := strings.TrimRight(cfg.URL, "/") + "/api/health"
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

	var parsed grafanaHealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decoding health response: %w", err)
	}

	status := statusHealthy
	if parsed.Database != "ok" {
		status = statusDegraded
	}

	return []Field{
		{Label: labelStatus, Value: status},
		{Label: labelVersion, Value: parsed.Version},
	}, nil
}
