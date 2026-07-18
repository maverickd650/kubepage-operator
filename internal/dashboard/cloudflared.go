package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
// scoped to read Tunnel status, sent as a Bearer token. The tunnel's own
// status ("healthy"/"degraded"/"down"/"inactive") is shown verbatim
// (capitalized) rather than folded into statusUnreach: a down or inactive
// tunnel is a fact reported by a successful poll, not a poll failure.
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

	if cfg.URL == "" {
		cfg.URL = cloudflaredAPIBase
	}
	path := fmt.Sprintf("/accounts/%s/cfd_tunnel/%s", tunnelCfg.AccountID, tunnelCfg.TunnelID)

	headers := map[string]string{}
	if token := cfg.Secrets["token"]; token != "" {
		headers["Authorization"] = "Bearer " + token
	}

	var parsed cloudflaredTunnelResponse
	if fields, err := fetchJSON(ctx, httpClient, cfg, "cloudflared", path, headers, &parsed); fields != nil || err != nil {
		return fields, err
	}

	status := statusUnknown
	switch parsed.Result.Status {
	case "healthy":
		status = statusHealthy
	case "degraded":
		status = statusDegraded
	case "down":
		status = statusDown
	case "inactive":
		status = statusInactive
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
