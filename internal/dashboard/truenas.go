package dashboard

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

func init() {
	Register("truenas", &truenasWidget{})
}

// truenasWidget polls TrueNAS's REST API (/api/v2.0/system/info) for
// version and uptime. Secrets["token"] is a TrueNAS API key, sent as
// "Authorization: Bearer <key>" per TrueNAS's v2.0 API auth scheme.
type truenasWidget struct{}

type truenasSystemInfoResponse struct {
	Version       string `json:"version"`
	UptimeSeconds int64  `json:"uptime_seconds"`
}

func (truenasWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	if cfg.URL == "" {
		return nil, errors.New("truenas widget: url is required")
	}

	endpoint := strings.TrimRight(cfg.URL, "/") + "/api/v2.0/system/info"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	if token := cfg.Secrets["token"]; token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	var parsed truenasSystemInfoResponse
	if fields, err := doJSONRequest(httpClient, req, &parsed); fields != nil || err != nil {
		return fields, err
	}

	return []Field{
		{Label: labelVersion, Value: parsed.Version},
		{Label: labelUptime, Value: formatUptime(parsed.UptimeSeconds)},
	}, nil
}

func (truenasWidget) Sample(WidgetConfig) []Field {
	return []Field{
		{Label: labelVersion, Value: "TrueNAS-24.04.0"},
		{Label: labelUptime, Value: formatUptime(370000)},
	}
}

func formatUptime(seconds int64) string {
	days := seconds / 86400
	hours := (seconds % 86400) / 3600
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	minutes := (seconds % 3600) / 60
	return fmt.Sprintf("%dh %dm", hours, minutes)
}
