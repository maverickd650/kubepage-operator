package dashboard

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
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
	if cfg.URL == "" {
		return nil, errors.New("plex widget: url is required")
	}

	endpoint := strings.TrimRight(cfg.URL, "/") + "/status/sessions"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if token := cfg.Secrets["token"]; token != "" {
		req.Header.Set("X-Plex-Token", token)
	}

	var parsed plexSessionsResponse
	if fields, err := doJSONRequest(httpClient, req, &parsed); fields != nil || err != nil {
		return fields, err
	}

	return []Field{
		{Label: labelStatus, Value: statusHealthy},
		{Label: labelStreams, Value: fmt.Sprintf("%d", parsed.MediaContainer.Size)},
	}, nil
}
