package dashboard

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

func init() {
	Register("linkwarden", &linkwardenWidget{})
}

// linkwardenWidget polls Linkwarden's /api/v1/links endpoint and counts the
// returned links. Secrets["token"] is a Linkwarden API token, sent as a
// Bearer token.
type linkwardenWidget struct{}

type linkwardenLinksResponse struct {
	Response []struct{} `json:"response"`
}

func (linkwardenWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	if cfg.URL == "" {
		return nil, errors.New("linkwarden widget: url is required")
	}

	endpoint := strings.TrimRight(cfg.URL, "/") + "/api/v1/links"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	if token := cfg.Secrets["token"]; token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	var parsed linkwardenLinksResponse
	if fields, err := doJSONRequest(httpClient, req, &parsed); fields != nil || err != nil {
		return fields, err
	}

	return []Field{
		{Label: labelLinks, Value: fmt.Sprintf("%d", len(parsed.Response))},
	}, nil
}
