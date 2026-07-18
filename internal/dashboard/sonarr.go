package dashboard

import (
	"context"
	"fmt"
	"net/http"
)

func init() {
	Register("sonarr", &sonarrWidget{})
}

// sonarrWidget polls Sonarr's /api/v3/series and /api/v3/queue endpoints for
// library size and download-queue depth. Secrets["apiKey"] is a Sonarr API
// key, sent as the "X-Api-Key" header (Sonarr's own auth scheme, not
// Bearer/Basic).
type sonarrWidget struct{}

type sonarrQueueResponse struct {
	TotalRecords int `json:"totalRecords"`
}

func (sonarrWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	headers := map[string]string{headerXAPIKey: cfg.Secrets[secretAPIKey]}

	var series []struct{}
	if fields, err := fetchJSON(ctx, httpClient, cfg, "sonarr", "/api/v3/series", headers, &series); fields != nil || err != nil {
		return fields, err
	}

	var queue sonarrQueueResponse
	if fields, err := fetchJSON(ctx, httpClient, cfg, "sonarr", "/api/v3/queue", headers, &queue); fields != nil || err != nil {
		return fields, err
	}

	return []Field{
		{Label: labelSeries, Value: fmt.Sprintf("%d", len(series))},
		{Label: labelQueue, Value: fmt.Sprintf("%d", queue.TotalRecords)},
	}, nil
}

func (sonarrWidget) Sample(WidgetConfig) []Field {
	return []Field{
		{Label: labelSeries, Value: "142"},
		{Label: labelQueue, Value: "2"},
	}
}
