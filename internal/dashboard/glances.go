package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

func init() {
	Register("glances", &glancesWidget{})
}

// glancesWidget is a header InfoWidget that shows host CPU/memory usage from
// a Glances REST API server's /quicklook endpoint
// (https://glances.readthedocs.io/en/latest/api.html) — a small summary
// object, rather than /all's full stats dump (processlist and everything
// else), which is expensive to compute server-side and unused here. Config
// is an optional JSON object: {"apiVersion": "3"|"4"} — apiVersion defaults
// to "4", matching current Glances releases; older installs still serving
// the v3 API can opt back in. cfg.URL is the Glances base URL (e.g.
// http://host:61208).
type glancesWidget struct{}

type glancesConfig struct {
	APIVersion string `json:"apiVersion"`
}

type glancesQuicklookResponse struct {
	CPU float64 `json:"cpu"`
	Mem float64 `json:"mem"`
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

	path := "/api/" + apiVersion + "/quicklook"

	var parsed glancesQuicklookResponse
	if fields, err := fetchJSON(ctx, httpClient, cfg, "glances", path, nil, &parsed); fields != nil || err != nil {
		return fields, err
	}

	cpuPct := int(parsed.CPU + 0.5)
	memPct := int(parsed.Mem + 0.5)
	return []Field{
		{Label: labelCPU, Value: fmt.Sprintf("%d%%", cpuPct), Percent: &cpuPct, Highlight: usageHighlight(&cpuPct)},
		{Label: labelMemory, Value: fmt.Sprintf("%d%%", memPct), Percent: &memPct, Highlight: usageHighlight(&memPct)},
	}, nil
}

func (glancesWidget) Sample(WidgetConfig) []Field {
	cpuPct, memPct := 45, 82
	return []Field{
		{Label: labelCPU, Value: fmt.Sprintf("%d%%", cpuPct), Percent: &cpuPct, Highlight: usageHighlight(&cpuPct)},
		{Label: labelMemory, Value: fmt.Sprintf("%d%%", memPct), Percent: &memPct, Highlight: usageHighlight(&memPct)},
	}
}
