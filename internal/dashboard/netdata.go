package dashboard

import (
	"context"
	"fmt"
	"net/http"
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
	var parsed netdataInfoResponse
	if fields, err := fetchJSON(ctx, httpClient, cfg, "netdata", "/api/v1/info", nil, &parsed); fields != nil || err != nil {
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
