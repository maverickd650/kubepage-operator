package dashboard

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const widgetTypeLonghorn = "longhorn"

func init() {
	Register(widgetTypeLonghorn, &longhornWidget{})
}

// longhornWidget is a header InfoWidget that shows aggregate cluster storage
// usage from a Longhorn Manager's REST API (https://longhorn.io/docs/latest/references/api-reference/),
// summed across every node's disk status. cfg.URL is the Longhorn Manager
// base URL (e.g. http://longhorn-frontend.longhorn-system:80), reachable
// from the dashboard pod like any other widget's URL — no cluster RBAC
// needed, unlike kubemetrics. Required to be set via spec.widgets[].url (see
// pollInfoWidget).
type longhornWidget struct{}

type longhornNodesResponse struct {
	Data []struct {
		DiskStatus map[string]struct {
			StorageMaximum   int64 `json:"storageMaximum"`
			StorageAvailable int64 `json:"storageAvailable"`
		} `json:"diskStatus"`
	} `json:"data"`
}

func (longhornWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	if cfg.URL == "" {
		return nil, errors.New("longhorn widget: url is required")
	}

	endpoint := strings.TrimRight(cfg.URL, "/") + "/v1/nodes"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	var parsed longhornNodesResponse
	if fields, err := doJSONRequest(httpClient, req, &parsed); fields != nil || err != nil {
		return fields, err
	}

	var maxBytes, availBytes int64
	for _, node := range parsed.Data {
		for _, disk := range node.DiskStatus {
			maxBytes += disk.StorageMaximum
			availBytes += disk.StorageAvailable
		}
	}
	if maxBytes == 0 {
		return []Field{{Label: labelStatus, Value: statusUnknown}}, nil
	}

	usedGiB := float64(maxBytes-availBytes) / bytesPerGiB
	totalGiB := float64(maxBytes) / bytesPerGiB
	pct := int(usedGiB/totalGiB*100 + 0.5)
	return []Field{
		{Label: labelStorage, Value: fmt.Sprintf("%s / %s GiB (%d%%)", trimFloat(usedGiB), trimFloat(totalGiB), pct), Percent: &pct, Highlight: usageHighlight(&pct)},
	}, nil
}

func (longhornWidget) Sample(WidgetConfig) []Field {
	usedGiB, totalGiB := 750.0, 1000.0
	pct := int(usedGiB/totalGiB*100 + 0.5)
	return []Field{
		{Label: labelStorage, Value: fmt.Sprintf("%s / %s GiB (%d%%)", trimFloat(usedGiB), trimFloat(totalGiB), pct), Percent: &pct, Highlight: usageHighlight(&pct)},
	}
}
