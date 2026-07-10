package dashboard

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
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
	if cfg.URL == "" {
		return nil, errors.New("radarr widget: url is required")
	}
	apiKey := cfg.Secrets[secretAPIKey]

	base := strings.TrimRight(cfg.URL, "/")

	movieReq, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v3/movie", nil)
	if err != nil {
		return nil, fmt.Errorf("building movie request: %w", err)
	}
	movieReq.Header.Set(headerXAPIKey, apiKey)

	var movies []struct{}
	if fields, err := doJSONRequest(httpClient, movieReq, &movies); fields != nil || err != nil {
		return fields, err
	}

	queueReq, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v3/queue", nil)
	if err != nil {
		return nil, fmt.Errorf("building queue request: %w", err)
	}
	queueReq.Header.Set(headerXAPIKey, apiKey)

	var queue radarrQueueResponse
	if fields, err := doJSONRequest(httpClient, queueReq, &queue); fields != nil || err != nil {
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
