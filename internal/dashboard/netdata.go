package dashboard

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

func init() {
	Register("netdata", &netdataWidget{})
}

const (
	labelWarnings  = "Warnings"
	labelCriticals = "Criticals"
)

// netdataWidget polls a Netdata agent's /api/v1/info endpoint for its
// active alarm counts, matching gethomepage/homepage's netdata widget.
// Netdata's info endpoint requires no authentication by default.
type netdataWidget struct{}

type netdataInfoResponse struct {
	Alarms struct {
		Warning  int `json:"warning"`
		Critical int `json:"critical"`
	} `json:"alarms"`
}

func (netdataWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	if cfg.URL == "" {
		return nil, errors.New("netdata widget: url is required")
	}

	endpoint := strings.TrimRight(cfg.URL, "/") + "/api/v1/info"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	var parsed netdataInfoResponse
	if fields, err := doJSONRequest(httpClient, req, &parsed); fields != nil || err != nil {
		return fields, err
	}

	return []Field{
		{Label: labelWarnings, Value: fmt.Sprintf("%d", parsed.Alarms.Warning)},
		{Label: labelCriticals, Value: fmt.Sprintf("%d", parsed.Alarms.Critical)},
	}, nil
}

func (netdataWidget) Sample(WidgetConfig) []Field {
	return []Field{
		{Label: labelWarnings, Value: "2"},
		{Label: labelCriticals, Value: "0"},
	}
}
