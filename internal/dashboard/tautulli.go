package dashboard

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

func init() {
	Register("tautulli", &tautulliWidget{})
}

// tautulliWidget polls Tautulli's single-endpoint API
// (GET /api/v2?apikey=...&cmd=get_activity) for current Plex stream count
// and bandwidth. Unlike every other widget here, Tautulli's API key is a
// query parameter, not a header — Secrets["apiKey"] is appended to the URL
// rather than set on the request.
type tautulliWidget struct{}

const labelBandwidth = "Bandwidth"

type tautulliActivityResponse struct {
	Response struct {
		Data struct {
			StreamCount    string `json:"stream_count"`
			TotalBandwidth int    `json:"total_bandwidth"`
		} `json:"data"`
	} `json:"response"`
}

func (tautulliWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	if cfg.URL == "" {
		return nil, errors.New("tautulli widget: url is required")
	}

	query := url.Values{
		"apikey": {cfg.Secrets["apiKey"]},
		"cmd":    {"get_activity"},
	}
	endpoint := strings.TrimRight(cfg.URL, "/") + "/api/v2?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	var parsed tautulliActivityResponse
	if fields, err := doJSONRequest(httpClient, req, &parsed); fields != nil || err != nil {
		return fields, err
	}

	streams := cmp.Or(parsed.Response.Data.StreamCount, "0")

	return []Field{
		{Label: labelStreams, Value: streams},
		{Label: labelBandwidth, Value: fmt.Sprintf("%.1f Mbps", float64(parsed.Response.Data.TotalBandwidth)/1000)},
	}, nil
}

func (tautulliWidget) Sample(WidgetConfig) []Field {
	return []Field{
		{Label: labelStreams, Value: "2"},
		{Label: labelBandwidth, Value: "12.3 Mbps"},
	}
}
