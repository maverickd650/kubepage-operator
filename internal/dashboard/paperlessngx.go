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
	Register("paperlessngx", &paperlessngxWidget{})
}

// paperlessngxWidget polls Paperless-ngx's /api/statistics/ endpoint.
// Secrets["token"] is a Paperless API token, sent as "Authorization: Token
// <token>" (Paperless's own auth scheme, distinct from Bearer).
type paperlessngxWidget struct{}

type paperlessngxStatisticsResponse struct {
	DocumentsTotal int `json:"documents_total"`
	DocumentsInbox int `json:"documents_inbox"`
}

func (paperlessngxWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	if cfg.URL == "" {
		return nil, errors.New("paperlessngx widget: url is required")
	}

	endpoint := strings.TrimRight(cfg.URL, "/") + "/api/statistics/"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	if token := cfg.Secrets["token"]; token != "" {
		req.Header.Set("Authorization", "Token "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return []Field{{Label: labelStatus, Value: statusUnreach}}, nil
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return []Field{{Label: labelStatus, Value: fmt.Sprintf("HTTP %d", resp.StatusCode)}}, nil
	}

	var parsed paperlessngxStatisticsResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decoding statistics response: %w", err)
	}

	return []Field{
		{Label: "Documents", Value: fmt.Sprintf("%d", parsed.DocumentsTotal)},
		{Label: "Inbox", Value: fmt.Sprintf("%d", parsed.DocumentsInbox)},
	}, nil
}
