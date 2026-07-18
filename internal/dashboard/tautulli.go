package dashboard

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"net/url"
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
	query := url.Values{
		"apikey": {cfg.Secrets["apiKey"]},
		"cmd":    {"get_activity"},
	}
	path := "/api/v2?" + query.Encode()

	var parsed tautulliActivityResponse
	if fields, err := fetchJSON(ctx, httpClient, cfg, "tautulli", path, nil, &parsed); fields != nil || err != nil {
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
