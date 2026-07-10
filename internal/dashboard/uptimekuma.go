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
	Register("uptime-kuma", &uptimeKumaWidget{})
}

// uptimeKumaWidget polls one Uptime Kuma public status page for its
// monitors' latest up/down state. Uptime Kuma has no API-key auth for this
// data — status pages are meant to be public — so there are no Secrets;
// config.slug names which published status page to read.
//
// Two requests are made: /api/status-page/<slug> to enumerate every monitor
// on the page (so a monitor with no heartbeat yet is still counted, as
// Down), and /api/status-page/heartbeat/<slug> for each monitor's most
// recent heartbeat status.
type uptimeKumaWidget struct{}

const labelDown = "Down"

type uptimeKumaConfig struct {
	Slug string `json:"slug"`
}

type uptimeKumaStatusPageResponse struct {
	PublicGroupList []struct {
		MonitorList []struct {
			ID int `json:"id"`
		} `json:"monitorList"`
	} `json:"publicGroupList"`
}

// uptimeKumaHeartbeat is one entry in the heartbeat endpoint's per-monitor
// list; Status follows Uptime Kuma's convention of 1 = up, 0 = down (2 =
// pending, 3 = maintenance also occur but are treated as Down here — see
// Poll).
type uptimeKumaHeartbeat struct {
	Status int `json:"status"`
}

type uptimeKumaHeartbeatResponse struct {
	HeartbeatList map[string][]uptimeKumaHeartbeat `json:"heartbeatList"`
}

func (uptimeKumaWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	if cfg.URL == "" {
		return nil, errors.New("uptime-kuma widget: url is required")
	}

	var kumaCfg uptimeKumaConfig
	if len(cfg.Config) > 0 {
		if err := json.Unmarshal(cfg.Config, &kumaCfg); err != nil {
			return nil, fmt.Errorf("decoding widget config: %w", err)
		}
	}
	if kumaCfg.Slug == "" {
		return nil, errors.New("uptime-kuma widget: config.slug is required")
	}

	base := strings.TrimRight(cfg.URL, "/")

	pageReq, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/status-page/"+kumaCfg.Slug, nil)
	if err != nil {
		return nil, fmt.Errorf("building status page request: %w", err)
	}

	var page uptimeKumaStatusPageResponse
	if fields, err := doJSONRequest(httpClient, pageReq, &page); fields != nil || err != nil {
		return fields, err
	}

	heartbeatReq, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/status-page/heartbeat/"+kumaCfg.Slug, nil)
	if err != nil {
		return nil, fmt.Errorf("building heartbeat request: %w", err)
	}

	var heartbeats uptimeKumaHeartbeatResponse
	if fields, err := doJSONRequest(httpClient, heartbeatReq, &heartbeats); fields != nil || err != nil {
		return fields, err
	}

	up, down := 0, 0
	for _, group := range page.PublicGroupList {
		for _, monitor := range group.MonitorList {
			list := heartbeats.HeartbeatList[strconv.Itoa(monitor.ID)]
			if len(list) > 0 && list[len(list)-1].Status == 1 {
				up++
			} else {
				down++
			}
		}
	}

	return []Field{
		{Label: labelUp, Value: fmt.Sprintf("%d", up)},
		{Label: labelDown, Value: fmt.Sprintf("%d", down)},
	}, nil
}

func (uptimeKumaWidget) Sample(WidgetConfig) []Field {
	return []Field{
		{Label: labelUp, Value: "11"},
		{Label: labelDown, Value: "1"},
	}
}
