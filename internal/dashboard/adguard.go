package dashboard

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

func init() {
	Register("adguard", &adguardWidget{})
}

// adguardWidget polls AdGuard Home's /control/stats endpoint for DNS query
// volume. Unlike every other widget in this file family, AdGuard Home's
// control API authenticates with HTTP Basic auth, not a static API key or
// header token: Secrets["username"]/Secrets["password"] are the AdGuard Home
// web-UI credentials, sent via the standard Authorization: Basic header.
type adguardWidget struct{}

const labelBlocked = "Blocked"

type adguardStatsResponse struct {
	NumDNSQueries       int `json:"num_dns_queries"`
	NumBlockedFiltering int `json:"num_blocked_filtering"`
}

func (adguardWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	if cfg.URL == "" {
		return nil, errors.New("adguard widget: url is required")
	}

	endpoint := strings.TrimRight(cfg.URL, "/") + "/control/stats"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	if username := cfg.Secrets["username"]; username != "" {
		req.SetBasicAuth(username, cfg.Secrets[secretPassword])
	}

	var parsed adguardStatsResponse
	if fields, err := doJSONRequest(httpClient, req, &parsed); fields != nil || err != nil {
		return fields, err
	}

	blockedPct := 0.0
	if parsed.NumDNSQueries > 0 {
		blockedPct = float64(parsed.NumBlockedFiltering) / float64(parsed.NumDNSQueries) * 100
	}

	return []Field{
		{Label: labelQueries, Value: fmt.Sprintf("%d", parsed.NumDNSQueries)},
		{Label: labelBlocked, Value: fmt.Sprintf("%d (%.1f%%)", parsed.NumBlockedFiltering, blockedPct)},
	}, nil
}

func (adguardWidget) Sample(WidgetConfig) []Field {
	return []Field{
		{Label: labelQueries, Value: "48213"},
		{Label: labelBlocked, Value: "9127 (18.9%)"},
	}
}
