package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func init() {
	Register("prometheusmetric", &prometheusMetricWidget{})
}

// prometheusMetricWidget runs a single config-driven PromQL instant query
// against /api/v1/query and shows its scalar result, unlike the fixed
// /api/v1/targets summary the plain "prometheus" widget shows. Config is a
// JSON object: {"query": "<promql>", "label": "<display label>"}. label
// defaults to "Value" if unset.
type prometheusMetricWidget struct{}

type prometheusMetricConfig struct {
	Query string `json:"query"`
	Label string `json:"label"`
}

type prometheusQueryResponse struct {
	Status string `json:"status"`
	Data   struct {
		Result []struct {
			Value [2]any `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

func (prometheusMetricWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	if cfg.URL == "" {
		return nil, errors.New("prometheusmetric widget: url is required")
	}
	if len(cfg.Config) == 0 {
		return nil, errors.New("prometheusmetric widget: config.query is required")
	}

	var metricCfg prometheusMetricConfig
	if err := json.Unmarshal(cfg.Config, &metricCfg); err != nil {
		return nil, fmt.Errorf("decoding widget config: %w", err)
	}
	if metricCfg.Query == "" {
		return nil, errors.New("prometheusmetric widget: config.query is required")
	}
	label := metricCfg.Label
	if label == "" {
		label = labelValue
	}

	endpoint := strings.TrimRight(cfg.URL, "/") + "/api/v1/query?query=" + url.QueryEscape(metricCfg.Query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	var parsed prometheusQueryResponse
	if fields, err := doJSONRequest(httpClient, req, &parsed); fields != nil || err != nil {
		return fields, err
	}

	if len(parsed.Data.Result) == 0 {
		return []Field{{Label: label, Value: statusUnknown}}, nil
	}

	raw, ok := parsed.Data.Result[0].Value[1].(string)
	if !ok {
		return []Field{{Label: label, Value: statusUnknown}}, nil
	}
	num, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return []Field{{Label: label, Value: raw}}, nil
	}

	return []Field{{Label: label, Value: strconv.FormatFloat(num, 'f', -1, 64)}}, nil
}

// Sample echoes the operator's own configured label back with a placeholder
// numeric value, same reasoning as customapi's Sample.
func (prometheusMetricWidget) Sample(cfg WidgetConfig) []Field {
	label := labelValue
	if len(cfg.Config) > 0 {
		var metricCfg prometheusMetricConfig
		if err := json.Unmarshal(cfg.Config, &metricCfg); err == nil && metricCfg.Label != "" {
			label = metricCfg.Label
		}
	}
	return []Field{{Label: label, Value: "42"}}
}
