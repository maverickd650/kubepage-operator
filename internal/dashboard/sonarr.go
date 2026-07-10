package dashboard

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
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
	if cfg.URL == "" {
		return nil, errors.New("sonarr widget: url is required")
	}
	apiKey := cfg.Secrets[secretAPIKey]

	base := strings.TrimRight(cfg.URL, "/")

	seriesReq, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v3/series", nil)
	if err != nil {
		return nil, fmt.Errorf("building series request: %w", err)
	}
	seriesReq.Header.Set(headerXAPIKey, apiKey)

	var series []struct{}
	if fields, err := doJSONRequest(httpClient, seriesReq, &series); fields != nil || err != nil {
		return fields, err
	}

	queueReq, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v3/queue", nil)
	if err != nil {
		return nil, fmt.Errorf("building queue request: %w", err)
	}
	queueReq.Header.Set(headerXAPIKey, apiKey)

	var queue sonarrQueueResponse
	if fields, err := doJSONRequest(httpClient, queueReq, &queue); fields != nil || err != nil {
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
