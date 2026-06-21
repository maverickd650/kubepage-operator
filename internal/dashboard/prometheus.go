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
	Register("prometheus", &prometheusWidget{})
}

const (
	labelStatus    = "Status"
	labelTargetsUp = "Targets Up"
	statusHealthy  = "Healthy"
	statusDegraded = "Degraded"
	statusUnknown  = "Unknown"
	statusUnreach  = "Unreachable"
)

// prometheusWidget polls a Prometheus server's /api/v1/targets endpoint and
// summarizes target health. Chosen as the spine's first (and only, for 6.0)
// widget because its API is open (no auth) and stable.
type prometheusWidget struct{}

type prometheusTargetsResponse struct {
	Status string `json:"status"`
	Data   struct {
		ActiveTargets []struct {
			Health string `json:"health"`
		} `json:"activeTargets"`
	} `json:"data"`
}

func (prometheusWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	if cfg.URL == "" {
		return nil, errors.New("prometheus widget: url is required")
	}

	endpoint := strings.TrimRight(cfg.URL, "/") + "/api/v1/targets?state=active"
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

	var parsed prometheusTargetsResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decoding targets response: %w", err)
	}

	total := len(parsed.Data.ActiveTargets)
	up := 0
	for _, t := range parsed.Data.ActiveTargets {
		if t.Health == "up" {
			up++
		}
	}

	status := statusHealthy
	switch {
	case total == 0:
		status = statusUnknown
	case up < total:
		status = statusDegraded
	}

	return []Field{
		{Label: labelStatus, Value: status},
		{Label: labelTargetsUp, Value: fmt.Sprintf("%d / %d", up, total)},
	}, nil
}
