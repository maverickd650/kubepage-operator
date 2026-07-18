package dashboard

import (
	"context"
	"fmt"
	"net/http"
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
	headers := map[string]string{}
	if token := cfg.Secrets["token"]; token != "" {
		headers["Authorization"] = "Token " + token
	}

	var parsed paperlessngxStatisticsResponse
	if fields, err := fetchJSON(ctx, httpClient, cfg, "paperlessngx", "/api/statistics/", headers, &parsed); fields != nil || err != nil {
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
