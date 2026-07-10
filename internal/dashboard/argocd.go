package dashboard

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

func init() {
	Register("argocd", &argocdWidget{})
}

// argocdWidget polls Argo CD's /api/v1/applications endpoint and summarizes
// every application's sync/health status. Secrets["token"] is an Argo CD
// API token (a project or account token, not a session JWT from the login
// endpoint), sent as a Bearer token.
type argocdWidget struct{}

const (
	labelApps    = "Apps"
	labelSynced  = "Synced"
	labelHealthy = "Healthy"
)

type argocdApplicationsResponse struct {
	Items []struct {
		Status struct {
			Sync struct {
				Status string `json:"status"`
			} `json:"sync"`
			Health struct {
				Status string `json:"status"`
			} `json:"health"`
		} `json:"status"`
	} `json:"items"`
}

func (argocdWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	if cfg.URL == "" {
		return nil, errors.New("argocd widget: url is required")
	}

	endpoint := strings.TrimRight(cfg.URL, "/") + "/api/v1/applications"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	if token := cfg.Secrets["token"]; token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	var parsed argocdApplicationsResponse
	if fields, err := doJSONRequest(httpClient, req, &parsed); fields != nil || err != nil {
		return fields, err
	}

	synced, healthy := 0, 0
	for _, app := range parsed.Items {
		if app.Status.Sync.Status == "Synced" {
			synced++
		}
		if app.Status.Health.Status == "Healthy" {
			healthy++
		}
	}

	return []Field{
		{Label: labelApps, Value: fmt.Sprintf("%d", len(parsed.Items))},
		{Label: labelSynced, Value: fmt.Sprintf("%d", synced)},
		{Label: labelHealthy, Value: fmt.Sprintf("%d", healthy)},
	}, nil
}

func (argocdWidget) Sample(WidgetConfig) []Field {
	return []Field{
		{Label: labelApps, Value: "22"},
		{Label: labelSynced, Value: "21"},
		{Label: labelHealthy, Value: "20"},
	}
}
