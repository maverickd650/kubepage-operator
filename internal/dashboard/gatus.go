package dashboard

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

func init() {
	Register("gatus", &gatusWidget{})
}

// gatusWidget polls a Gatus instance's public endpoint-statuses API for a
// count of endpoints whose most recent check succeeded vs. failed, matching
// gethomepage/homepage's gatus widget. No auth: Gatus's
// /api/v1/endpoints/statuses endpoint is unauthenticated by default.
type gatusWidget struct{}

// gatusEndpointStatus is one entry of the statuses response — a map keyed
// by "<group>_<name>" (or just "<name>") to this shape; only the latest
// result's success flag is used here.
type gatusEndpointStatus struct {
	Results []struct {
		Success bool `json:"success"`
	} `json:"results"`
}

func (gatusWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	if cfg.URL == "" {
		return nil, errors.New("gatus widget: url is required")
	}

	endpoint := strings.TrimRight(cfg.URL, "/") + "/api/v1/endpoints/statuses"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	var parsed map[string]gatusEndpointStatus
	if fields, err := doJSONRequest(httpClient, req, &parsed); fields != nil || err != nil {
		return fields, err
	}

	var up, down int
	for _, status := range parsed {
		if len(status.Results) == 0 {
			continue
		}
		if status.Results[len(status.Results)-1].Success {
			up++
		} else {
			down++
		}
	}

	return []Field{
		{Label: labelUp, Value: fmt.Sprintf("%d", up)},
		{Label: labelDown, Value: fmt.Sprintf("%d", down)},
	}, nil
}

func (gatusWidget) Sample(WidgetConfig) []Field {
	return []Field{
		{Label: labelUp, Value: "11"},
		{Label: labelDown, Value: "1"},
	}
}
