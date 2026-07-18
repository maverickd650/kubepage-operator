package dashboard

import (
	"context"
	"fmt"
	"net/http"
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
	headers := map[string]string{headerXAPIKey: cfg.Secrets[secretAPIKey]}

	var status jellyseerrStatusResponse
	if fields, err := fetchJSON(ctx, httpClient, cfg, "jellyseerr", "/api/v1/status", headers, &status); fields != nil || err != nil {
		return fields, err
	}

	var count jellyseerrRequestCountResponse
	if fields, err := fetchJSON(ctx, httpClient, cfg, "jellyseerr", "/api/v1/request/count", headers, &count); fields != nil || err != nil {
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
