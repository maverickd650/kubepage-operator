package dashboard

import (
	"context"
	"fmt"
	"net/http"
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
	var parsed adguardStatsResponse
	if fields, err := fetchJSONBasicAuth(ctx, httpClient, cfg, "adguard", "/control/stats", cfg.Secrets[secretUsername], cfg.Secrets[secretPassword], &parsed); fields != nil || err != nil {
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
