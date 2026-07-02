package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func init() {
	Register("customapi", &customAPIWidget{})
}

// customAPIWidget polls an arbitrary JSON HTTP endpoint and maps configured
// JSON-path expressions onto display Fields, homepage's "customapi" widget
// (https://gethomepage.dev/widgets/services/customapi/): the generic answer
// for "any service with a JSON status endpoint" that doesn't warrant its own
// hand-written Go widget. Secrets["token"], if set, is sent as a Bearer
// token; a widget with no auth need just omits it.
type customAPIWidget struct{}

// customAPIFieldConfig is one entry in ServiceWidget.Config's "mappings"
// array: jsonpath is a dot-separated path into the response body (array
// indices as plain integers, e.g. "data.0.value"); suffix is appended to a
// numeric value verbatim (e.g. "%", " ms").
type customAPIFieldConfig struct {
	Label    string `json:"label"`
	JSONPath string `json:"jsonpath"`
	Suffix   string `json:"suffix"`
}

type customAPIConfig struct {
	Mappings []customAPIFieldConfig `json:"mappings"`
}

func (customAPIWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	if cfg.URL == "" {
		return nil, errors.New("customapi widget: url is required")
	}
	if len(cfg.Config) == 0 {
		return nil, errors.New("customapi widget: config.mappings is required")
	}

	var apiCfg customAPIConfig
	if err := json.Unmarshal(cfg.Config, &apiCfg); err != nil {
		return nil, fmt.Errorf("decoding widget config: %w", err)
	}
	if len(apiCfg.Mappings) == 0 {
		return nil, errors.New("customapi widget: config.mappings must not be empty")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	if token := cfg.Secrets["token"]; token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	var body any
	if fields, err := doJSONRequest(httpClient, req, &body); fields != nil || err != nil {
		return fields, err
	}

	fields := make([]Field, 0, len(apiCfg.Mappings))
	for _, m := range apiCfg.Mappings {
		if m.Label == "" || m.JSONPath == "" {
			continue
		}
		value, ok := jsonPathLookup(body, m.JSONPath)
		if !ok {
			fields = append(fields, Field{Label: m.Label, Value: statusUnknown})
			continue
		}
		fields = append(fields, Field{Label: m.Label, Value: formatJSONValue(value) + m.Suffix})
	}
	return fields, nil
}

// jsonPathLookup walks a decoded JSON value (map[string]any/[]any/scalars)
// following path's dot-separated segments, treating a segment that parses as
// a non-negative integer as an array index. Returns ok=false for any missing
// key, out-of-range index, or a path that indexes through a scalar.
func jsonPathLookup(value any, path string) (any, bool) {
	for segment := range strings.SplitSeq(path, ".") {
		if segment == "" {
			continue
		}
		switch v := value.(type) {
		case map[string]any:
			next, ok := v[segment]
			if !ok {
				return nil, false
			}
			value = next
		case []any:
			idx, err := strconv.Atoi(segment)
			if err != nil || idx < 0 || idx >= len(v) {
				return nil, false
			}
			value = v[idx]
		default:
			return nil, false
		}
	}
	return value, true
}

// formatJSONValue renders a jsonPathLookup result for display: a float
// without a trailing ".0" for whole numbers, a bool as "true"/"false", a
// string verbatim, and anything else (an object/array the path resolved to
// rather than a scalar) via a compact JSON dump so it's still visible rather
// than silently blank.
func formatJSONValue(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(val)
	case nil:
		return ""
	default:
		b, err := json.Marshal(val)
		if err != nil {
			return statusUnknown
		}
		return string(b)
	}
}
