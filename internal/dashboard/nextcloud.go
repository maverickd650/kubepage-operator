package dashboard

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

func init() {
	Register("nextcloud", &nextcloudWidget{})
}

const (
	labelCPULoad     = "CPU Load"
	labelMemoryUsage = "Memory Usage"
	labelFreeSpace   = "Free Space"
	labelActiveUsers = "Active Users"
	headerNCToken    = "NC-Token"
	secretKeyNCToken = "key"
)

// nextcloudWidget polls a Nextcloud instance's serverinfo API
// (/ocs/v2.php/apps/serverinfo/api/v1/info?format=json) for CPU load,
// memory usage, free storage, and active-user count, matching
// gethomepage/homepage's nextcloud widget.
//
// Homepage supports two auth modes for this widget: Secrets["key"] sent as
// the "NC-Token" header (an app password / API token generated in Nextcloud
// Settings > Security), or Secrets["username"]/Secrets["password"] HTTP
// Basic auth — key wins when both are set, matching homepage's own
// credentialedProxyHandler precedence for this widget type.
type nextcloudWidget struct{}

type nextcloudInfoResponse struct {
	OCS struct {
		Data struct {
			Nextcloud struct {
				System struct {
					CPULoad   []float64          `json:"cpuload"`
					MemTotal  nextcloudFlexFloat `json:"mem_total"`
					MemFree   nextcloudFlexFloat `json:"mem_free"`
					FreeSpace float64            `json:"freespace"`
				} `json:"system"`
			} `json:"nextcloud"`
			ActiveUsers struct {
				Last24Hours int `json:"last24hours"`
			} `json:"activeUsers"`
		} `json:"data"`
	} `json:"ocs"`
}

// nextcloudFlexFloat unmarshals a JSON number that some Nextcloud versions
// encode as a quoted string (mem_total/mem_free in the serverinfo API) —
// homepage's own component applies parseFloat() to these two fields for the
// same reason, unlike freespace, which it uses as a bare number.
type nextcloudFlexFloat float64

func (f *nextcloudFlexFloat) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	var v float64
	if _, err := fmt.Sscanf(s, "%g", &v); err != nil {
		return fmt.Errorf("parsing numeric field %q: %w", s, err)
	}
	*f = nextcloudFlexFloat(v)
	return nil
}

func (nextcloudWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	req, err := buildJSONRequest(ctx, cfg, "nextcloud", "/ocs/v2.php/apps/serverinfo/api/v1/info?format=json")
	if err != nil {
		return nil, err
	}
	if key := cfg.Secrets[secretKeyNCToken]; key != "" {
		req.Header.Set(headerNCToken, key)
	} else if username := cfg.Secrets[secretUsername]; username != "" {
		req.SetBasicAuth(username, cfg.Secrets[secretPassword])
	}

	var parsed nextcloudInfoResponse
	if fields, err := doJSONRequest(httpClient, req, &parsed); fields != nil || err != nil {
		return fields, err
	}

	system := parsed.OCS.Data.Nextcloud.System
	cpuLoad := 0.0
	if len(system.CPULoad) > 0 {
		cpuLoad = system.CPULoad[0]
	}
	memTotal, memFree := float64(system.MemTotal), float64(system.MemFree)
	memPct := 0
	if memTotal > 0 {
		memPct = int((memTotal-memFree)/memTotal*100 + 0.5)
	}
	freeGiB := system.FreeSpace / bytesPerGiB

	return []Field{
		{Label: labelCPULoad, Value: fmt.Sprintf("%.2f%%", cpuLoad)},
		{Label: labelMemoryUsage, Value: fmt.Sprintf("%d%%", memPct), Percent: &memPct, Highlight: usageHighlight(&memPct)},
		{Label: labelFreeSpace, Value: fmt.Sprintf("%s GiB", trimFloat(freeGiB))},
		{Label: labelActiveUsers, Value: fmt.Sprintf("%d", parsed.OCS.Data.ActiveUsers.Last24Hours)},
	}, nil
}

func (nextcloudWidget) Sample(WidgetConfig) []Field {
	memPct := 61
	return []Field{
		{Label: labelCPULoad, Value: "0.42%"},
		{Label: labelMemoryUsage, Value: fmt.Sprintf("%d%%", memPct), Percent: &memPct, Highlight: usageHighlight(&memPct)},
		{Label: labelFreeSpace, Value: "812.5 GiB"},
		{Label: labelActiveUsers, Value: "17"},
	}
}
