package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

func init() {
	Register("speedtest", &speedtestWidget{})
}

const (
	labelDownload = "Download"
	labelUpload   = "Upload"
	labelPing     = "Ping"

	// Sample's placeholder values, pulled into constants since they also
	// appear verbatim in speedtest_test.go's Poll assertions.
	speedtestSampleDownload = "94.3 Mbps"
	speedtestSampleUpload   = "11.2 Mbps"
	speedtestSamplePing     = "8 ms"
)

// speedtestWidget polls a Speedtest Tracker instance's latest-result
// endpoint for download/upload throughput and ping, matching
// gethomepage/homepage's speedtest widget. Speedtest Tracker changed its API
// shape between major versions; config: {"version": 2} selects the v2 API
// ("/api/v1/results/latest", results in bytes/sec, requiring
// Secrets["key"] as a Bearer token) — version unset or 1 uses the older
// "/api/speedtest/latest" (results already in Mbps, no auth required).
type speedtestWidget struct{}

type speedtestConfig struct {
	Version int `json:"version"`
}

type speedtestLatestResponse struct {
	Data struct {
		Download float64 `json:"download"`
		Upload   float64 `json:"upload"`
		Ping     float64 `json:"ping"`
	} `json:"data"`
}

func (speedtestWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	if cfg.URL == "" {
		return nil, errors.New("speedtest widget: url is required")
	}

	var stCfg speedtestConfig
	if len(cfg.Config) > 0 {
		if err := json.Unmarshal(cfg.Config, &stCfg); err != nil {
			return nil, fmt.Errorf("decoding widget config: %w", err)
		}
	}

	path := "/api/speedtest/latest"
	if stCfg.Version == 2 {
		path = "/api/v1/results/latest"
	}

	headers := map[string]string{}
	if key := cfg.Secrets[secretAPIKey]; key != "" {
		headers["Authorization"] = "Bearer " + key
	}

	var parsed speedtestLatestResponse
	if fields, err := fetchJSON(ctx, httpClient, cfg, "speedtest", path, headers, &parsed); fields != nil || err != nil {
		return fields, err
	}

	downloadMbps, uploadMbps := parsed.Data.Download, parsed.Data.Upload
	if stCfg.Version == 2 {
		downloadMbps = parsed.Data.Download * 8 / 1_000_000
		uploadMbps = parsed.Data.Upload * 8 / 1_000_000
	}

	return []Field{
		{Label: labelDownload, Value: fmt.Sprintf("%.1f Mbps", downloadMbps)},
		{Label: labelUpload, Value: fmt.Sprintf("%.1f Mbps", uploadMbps)},
		{Label: labelPing, Value: fmt.Sprintf("%.0f ms", parsed.Data.Ping)},
	}, nil
}

func (speedtestWidget) Sample(WidgetConfig) []Field {
	return []Field{
		{Label: labelDownload, Value: speedtestSampleDownload},
		{Label: labelUpload, Value: speedtestSampleUpload},
		{Label: labelPing, Value: speedtestSamplePing},
	}
}
