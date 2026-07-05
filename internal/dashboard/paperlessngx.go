package dashboard

import (
	"context"
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

const (
	labelDocuments = "Documents"
	labelInbox     = "Inbox"
)

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

	var parsed paperlessngxStatisticsResponse
	if fields, err := doJSONRequest(httpClient, req, &parsed); fields != nil || err != nil {
		return fields, err
	}

	return []Field{
		{Label: labelDocuments, Value: fmt.Sprintf("%d", parsed.DocumentsTotal)},
		{Label: labelInbox, Value: fmt.Sprintf("%d", parsed.DocumentsInbox)},
	}, nil
}

func (paperlessngxWidget) Sample(WidgetConfig) []Field {
	return []Field{
		{Label: labelDocuments, Value: "1234"},
		{Label: labelInbox, Value: "12"},
	}
}
