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

// grafanaWidget polls Grafana's /api/admin/stats endpoint for dashboard/
// datasource/alert counts, mirroring gethomepage's grafana widget (same
// endpoint, same four fields). admin/stats requires server-admin
// credentials: Secrets["username"]/Secrets["password"] are sent as HTTP
// Basic auth (homepage's documented setup — an admin user's login), or
// Secrets["token"] as a Bearer token for a server-admin service account.
//
// The "Alerts Triggered" count follows homepage's default (version 1)
// resolution: legacy /api/alerts filtered to state == "alerting", falling
// back to the Grafana-managed alertmanager's active-alert list on clusters
// where the legacy endpoint is gone (removed in Grafana 11) or empty.
type grafanaWidget struct{}

const (
	labelDashboards      = "Dashboards"
	labelDatasources     = "Data Sources"
	labelTotalAlerts     = "Total Alerts"
	labelAlertsTriggered = "Alerts Triggered"

	grafanaStatsPath        = "/api/admin/stats"
	grafanaLegacyAlertsPath = "/api/alerts"
	grafanaAlertmanagerPath = "/api/alertmanager/grafana/api/v2/alerts"
	grafanaStateAlerting    = "alerting"
)

type grafanaStatsResponse struct {
	Dashboards  int `json:"dashboards"`
	Datasources int `json:"datasources"`
	Alerts      int `json:"alerts"`
}

type grafanaLegacyAlert struct {
	State string `json:"state"`
}

func (w grafanaWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	if cfg.URL == "" {
		return nil, errors.New("grafana widget: url is required")
	}

	statsReq, err := grafanaRequest(ctx, cfg, grafanaStatsPath)
	if err != nil {
		return nil, err
	}
	var stats grafanaStatsResponse
	if fields, err := doJSONRequest(httpClient, statsReq, &stats); fields != nil || err != nil {
		return fields, err
	}

	triggered, fields, err := w.triggeredAlerts(ctx, httpClient, cfg)
	if fields != nil || err != nil {
		return fields, err
	}

	return []Field{
		{Label: labelDashboards, Value: fmt.Sprintf("%d", stats.Dashboards)},
		{Label: labelDatasources, Value: fmt.Sprintf("%d", stats.Datasources)},
		{Label: labelTotalAlerts, Value: fmt.Sprintf("%d", stats.Alerts)},
		{Label: labelAlertsTriggered, Value: fmt.Sprintf("%d", triggered)},
	}, nil
}

// triggeredAlerts resolves the "Alerts Triggered" count the way homepage's
// grafana widget does at its default version 1: count legacy /api/alerts
// entries in the "alerting" state; if that endpoint fails or returns no
// alerts, fall back to the number of active alerts the Grafana-managed
// alertmanager reports. Only when both endpoints fail is the failure
// surfaced (as the legacy endpoint's status Field, matching homepage
// preferring the primary error).
func (grafanaWidget) triggeredAlerts(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) (int, []Field, error) {
	legacyReq, err := grafanaRequest(ctx, cfg, grafanaLegacyAlertsPath)
	if err != nil {
		return 0, nil, err
	}
	var legacy []grafanaLegacyAlert
	legacyFields, legacyErr := doJSONRequest(httpClient, legacyReq, &legacy)
	legacyOK := legacyFields == nil && legacyErr == nil
	if legacyOK && len(legacy) > 0 {
		triggered := 0
		for _, a := range legacy {
			if a.State == grafanaStateAlerting {
				triggered++
			}
		}
		return triggered, nil, nil
	}

	amReq, err := grafanaRequest(ctx, cfg, grafanaAlertmanagerPath)
	if err != nil {
		return 0, nil, err
	}
	var active []json.RawMessage
	if fields, err := doJSONRequest(httpClient, amReq, &active); fields == nil && err == nil {
		return len(active), nil, nil
	}
	if legacyOK {
		// Legacy endpoint answered with zero alerts and the alertmanager
		// fallback failed: homepage shows 0 rather than an error here.
		return 0, nil, nil
	}
	return 0, legacyFields, legacyErr
}

// grafanaRequest builds an authenticated GET against cfg.URL+path:
// username/password secrets become HTTP Basic auth, else a token secret
// becomes a Bearer token.
func grafanaRequest(ctx context.Context, cfg WidgetConfig, path string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(cfg.URL, "/")+path, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	if username := cfg.Secrets[secretUsername]; username != "" {
		req.SetBasicAuth(username, cfg.Secrets[secretPassword])
	} else if token := cfg.Secrets["token"]; token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req, nil
}

func (grafanaWidget) Sample(WidgetConfig) []Field {
	return []Field{
		{Label: labelDashboards, Value: "24"},
		{Label: labelDatasources, Value: "6"},
		{Label: labelTotalAlerts, Value: "18"},
		{Label: labelAlertsTriggered, Value: "2"},
	}
}
