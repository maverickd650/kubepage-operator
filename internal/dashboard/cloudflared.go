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
	Register("cloudflared", &cloudflaredWidget{})
}

// cloudflaredAPIBase is the default Cloudflare API root. Unlike every other
// widget here, cloudflared has no local URL to poll: tunnel status lives in
// Cloudflare's own API. cfg.URL, if set, overrides this base (used by tests
// and by anyone proxying the Cloudflare API).
const cloudflaredAPIBase = "https://api.cloudflare.com/client/v4"

// cloudflaredWidget polls the Cloudflare API for one named tunnel's status.
// Config: {"accountId": "...", "tunnelId": "..."} (not secret — these are
// identifiers, not credentials). Secrets["token"] is a Cloudflare API token
// scoped to read Tunnel status, sent as a Bearer token.
type cloudflaredWidget struct{}

type cloudflaredConfig struct {
	AccountID string `json:"accountId"`
	TunnelID  string `json:"tunnelId"`
}

type cloudflaredTunnelResponse struct {
	Result struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	} `json:"result"`
}

func (cloudflaredWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	if len(cfg.Config) == 0 {
		return nil, errors.New("cloudflared widget: config.accountId and config.tunnelId are required")
	}

	var tunnelCfg cloudflaredConfig
	if err := json.Unmarshal(cfg.Config, &tunnelCfg); err != nil {
		return nil, fmt.Errorf("decoding widget config: %w", err)
	}
	if tunnelCfg.AccountID == "" || tunnelCfg.TunnelID == "" {
		return nil, errors.New("cloudflared widget: config.accountId and config.tunnelId are required")
	}

	base := cloudflaredAPIBase
	if cfg.URL != "" {
		base = cfg.URL
	}

	endpoint := fmt.Sprintf("%s/accounts/%s/cfd_tunnel/%s", strings.TrimRight(base, "/"), tunnelCfg.AccountID, tunnelCfg.TunnelID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	if token := cfg.Secrets["token"]; token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	var parsed cloudflaredTunnelResponse
	if fields, err := doJSONRequest(httpClient, req, &parsed); fields != nil || err != nil {
		return fields, err
	}

	status := statusUnknown
	switch parsed.Result.Status {
	case "healthy":
		status = statusHealthy
	case "degraded":
		status = statusDegraded
	case "down", "inactive":
		status = statusUnreach
	}

	return []Field{
		{Label: labelStatus, Value: status},
		{Label: labelTunnel, Value: parsed.Result.Name},
	}, nil
}

func (cloudflaredWidget) Sample(WidgetConfig) []Field {
	return []Field{
		{Label: labelStatus, Value: statusHealthy},
		{Label: labelTunnel, Value: "sample-tunnel"},
	}
}
