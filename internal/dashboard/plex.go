package dashboard

import (
	"context"
	"fmt"
	"net/http"
)

func init() {
	Register("plex", &plexWidget{})
}

// plexWidget polls Plex's /status/sessions endpoint for the current stream
// count. Secrets["token"] is Plex's X-Plex-Token, sent as a header (not a
// query param, so it never lands in server access logs).
type plexWidget struct{}

type plexSessionsResponse struct {
	MediaContainer struct {
		Size int `json:"size"`
	} `json:"MediaContainer"`
}

func (plexWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	headers := map[string]string{"Accept": "application/json"}
	if token := cfg.Secrets["token"]; token != "" {
		headers["X-Plex-Token"] = token
	}

	var parsed plexSessionsResponse
	if fields, err := fetchJSON(ctx, httpClient, cfg, "plex", "/status/sessions", headers, &parsed); fields != nil || err != nil {
		return fields, err
	}

	return []Field{
		{Label: labelStatus, Value: statusHealthy},
		{Label: labelStreams, Value: fmt.Sprintf("%d", parsed.MediaContainer.Size)},
	}, nil
}

func (plexWidget) Sample(WidgetConfig) []Field {
	return []Field{
		{Label: labelStatus, Value: statusHealthy},
		{Label: labelStreams, Value: "3"},
	}
}
