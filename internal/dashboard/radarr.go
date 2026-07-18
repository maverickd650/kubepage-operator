package dashboard

import (
	"context"
	"fmt"
	"net/http"
)

func init() {
	Register("radarr", &radarrWidget{})
}

// radarrWidget polls Radarr's /api/v3/movie and /api/v3/queue endpoints for
// library size and download-queue depth. Secrets["apiKey"] is a Radarr API
// key, sent as the "X-Api-Key" header (same scheme as Sonarr, see sonarr.go).
type radarrWidget struct{}

type radarrQueueResponse struct {
	TotalRecords int `json:"totalRecords"`
}

func (radarrWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	headers := map[string]string{headerXAPIKey: cfg.Secrets[secretAPIKey]}

	var movies []struct{}
	if fields, err := fetchJSON(ctx, httpClient, cfg, "radarr", "/api/v3/movie", headers, &movies); fields != nil || err != nil {
		return fields, err
	}

	var queue radarrQueueResponse
	if fields, err := fetchJSON(ctx, httpClient, cfg, "radarr", "/api/v3/queue", headers, &queue); fields != nil || err != nil {
		return fields, err
	}

	return []Field{
		{Label: labelMovies, Value: fmt.Sprintf("%d", len(movies))},
		{Label: labelQueue, Value: fmt.Sprintf("%d", queue.TotalRecords)},
	}, nil
}

func (radarrWidget) Sample(WidgetConfig) []Field {
	return []Field{
		{Label: labelMovies, Value: "318"},
		{Label: labelQueue, Value: "1"},
	}
}
