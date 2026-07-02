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
	Register("glances", &glancesWidget{})
}

// glancesWidget is a header InfoWidget that shows host CPU/memory usage from
// a Glances REST API server (https://glances.readthedocs.io/en/latest/api.html).
// Config is an optional JSON object: {"apiVersion": "3"|"4"} — apiVersion
// defaults to "4", matching current Glances releases; older installs still
// serving the v3 API can opt back in. cfg.URL is the Glances base URL (e.g.
// http://host:61208).
type glancesWidget struct{}

type glancesConfig struct {
	APIVersion string `json:"apiVersion"`
}

type glancesAllResponse struct {
	CPU struct {
		Total float64 `json:"total"`
	} `json:"cpu"`
	Mem struct {
		Percent float64 `json:"percent"`
	} `json:"mem"`
}

func (glancesWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	if cfg.URL == "" {
		return nil, errors.New("glances widget: url is required")
	}
	apiVersion := "4"
	if len(cfg.Config) > 0 {
		var c glancesConfig
		if err := json.Unmarshal(cfg.Config, &c); err != nil {
			return nil, fmt.Errorf("decoding widget config: %w", err)
		}
		if c.APIVersion != "" {
			apiVersion = c.APIVersion
		}
	}

	endpoint := strings.TrimRight(cfg.URL, "/") + "/api/" + apiVersion + "/all"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	var parsed glancesAllResponse
	if fields, err := doJSONRequest(httpClient, req, &parsed); fields != nil || err != nil {
		return fields, err
	}

	cpuPct := int(parsed.CPU.Total + 0.5)
	memPct := int(parsed.Mem.Percent + 0.5)
	return []Field{
		{Label: labelCPU, Value: fmt.Sprintf("%d%%", cpuPct), Percent: &cpuPct, Highlight: usageHighlight(&cpuPct)},
		{Label: labelMemory, Value: fmt.Sprintf("%d%%", memPct), Percent: &memPct, Highlight: usageHighlight(&memPct)},
	}, nil
}
