package dashboard

import (
	"context"
	"encoding/json"
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

	resp, err := httpClient.Do(req)
	if err != nil {
		return []Field{{Label: labelStatus, Value: statusUnreach}}, nil
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return []Field{{Label: labelStatus, Value: fmt.Sprintf("HTTP %d", resp.StatusCode)}}, nil
	}

	var parsed linkwardenLinksResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decoding links response: %w", err)
	}

	return []Field{
		{Label: labelLinks, Value: fmt.Sprintf("%d", len(parsed.Response))},
	}, nil
}
