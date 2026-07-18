package dashboard

import (
	"context"
	"fmt"
	"net/http"
)

func init() {
	Register("prometheus", &prometheusWidget{})
}

const (
	labelStatus       = "Status"
	labelTargetsUp    = "Targets Up"
	labelTargetsDown  = "Targets Down"
	labelTargetsTotal = "Targets Total"
	statusHealthy     = "Healthy"
	statusDegraded    = "Degraded"
	statusUnknown     = "Unknown"
	statusUnreach     = "Unreachable"
	// statusInactive is cloudflared.go's own tunnel-status mapping (alongside
	// monitor.go's statusDown, reused here) — distinct from statusUnreach so
	// that a legitimately down/inactive tunnel (a fact reported by a
	// successful poll) isn't conflated with a failed poll: poller.go's
	// metricErr treats a Status field of statusUnreach as a poll failure,
	// which a down tunnel is not.
	statusInactive = "Inactive"
)

// prometheusWidget polls a Prometheus server's /api/v1/targets endpoint and
// summarizes target health. Chosen as the spine's first (and only, for 6.0)
// widget because its API is open (no auth) and stable.
type prometheusWidget struct{}

type prometheusTargetsResponse struct {
	Status string `json:"status"`
	Data   struct {
		ActiveTargets []struct {
			Health string `json:"health"`
		} `json:"activeTargets"`
	} `json:"data"`
}

func (prometheusWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	var parsed prometheusTargetsResponse
	if fields, err := fetchJSON(ctx, httpClient, cfg, "prometheus", "/api/v1/targets?state=active", nil, &parsed); fields != nil || err != nil {
		return fields, err
	}

	total := len(parsed.Data.ActiveTargets)
	up, down := 0, 0
	for _, t := range parsed.Data.ActiveTargets {
		switch t.Health {
		case "up":
			up++
		case apiHealthDown:
			down++
		}
	}

	status := statusHealthy
	switch {
	case total == 0:
		status = statusUnknown
	case up < total:
		status = statusDegraded
	}

	return []Field{
		{Label: labelStatus, Value: status},
		{Label: labelTargetsUp, Value: fmt.Sprintf("%d", up)},
		{Label: labelTargetsDown, Value: fmt.Sprintf("%d", down)},
		{Label: labelTargetsTotal, Value: fmt.Sprintf("%d", total)},
	}, nil
}

func (prometheusWidget) Sample(WidgetConfig) []Field {
	return []Field{
		{Label: labelStatus, Value: statusHealthy},
		{Label: labelTargetsUp, Value: "8"},
		{Label: labelTargetsDown, Value: "0"},
		{Label: labelTargetsTotal, Value: "8"},
	}
}
