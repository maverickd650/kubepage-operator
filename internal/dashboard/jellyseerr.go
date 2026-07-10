package dashboard

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

func init() {
	Register("jellyseerr", &jellyseerrWidget{})
}

// jellyseerrWidget polls Jellyseerr's /api/v1/status (version) and
// /api/v1/request/count (pending request count) endpoints.
// Secrets["apiKey"] is sent as the "X-Api-Key" header.
type jellyseerrWidget struct{}

const labelPending = "Pending"

type jellyseerrStatusResponse struct {
	Version string `json:"version"`
}

type jellyseerrRequestCountResponse struct {
	Pending int `json:"pending"`
}

func (jellyseerrWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	if cfg.URL == "" {
		return nil, errors.New("jellyseerr widget: url is required")
	}
	apiKey := cfg.Secrets[secretAPIKey]

	base := strings.TrimRight(cfg.URL, "/")

	statusReq, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/status", nil)
	if err != nil {
		return nil, fmt.Errorf("building status request: %w", err)
	}
	statusReq.Header.Set(headerXAPIKey, apiKey)

	var status jellyseerrStatusResponse
	if fields, err := doJSONRequest(httpClient, statusReq, &status); fields != nil || err != nil {
		return fields, err
	}

	countReq, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/request/count", nil)
	if err != nil {
		return nil, fmt.Errorf("building request-count request: %w", err)
	}
	countReq.Header.Set(headerXAPIKey, apiKey)

	var count jellyseerrRequestCountResponse
	if fields, err := doJSONRequest(httpClient, countReq, &count); fields != nil || err != nil {
		return fields, err
	}

	return []Field{
		{Label: labelVersion, Value: status.Version},
		{Label: labelPending, Value: fmt.Sprintf("%d", count.Pending)},
	}, nil
}

func (jellyseerrWidget) Sample(WidgetConfig) []Field {
	return []Field{
		{Label: labelVersion, Value: "2.1.0"},
		{Label: labelPending, Value: "3"},
	}
}
