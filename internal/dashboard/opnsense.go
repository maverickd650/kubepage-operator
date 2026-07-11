package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

func init() {
	Register("opnsense", &opnsenseWidget{})
}

const (
	labelWANUpload   = "WAN Upload"
	labelWANDownload = "WAN Download"

	// opnsenseSampleWANUpload is Sample's placeholder WAN-upload value,
	// pulled into a constant since it also appears in opnsense_test.go's
	// formatBytesHumanized table test for the same byte count.
	opnsenseSampleWANUpload = "12.4 GB"
)

// opnsenseWidget polls an OPNsense firewall's diagnostics API for CPU/memory
// usage and WAN interface traffic counters, matching gethomepage/homepage's
// opnsense widget. Auth is HTTP Basic using an OPNsense API key/secret pair
// (Settings > Access > Users > API Keys): Secrets["username"] is the key,
// Secrets["password"] is the secret, matching homepage's own field naming
// for this widget (an API key/secret pair sent as Basic auth, not an
// OPNsense user's actual login password).
//
// Config: {"wan": "wan"} — the interface name (as reported by
// /api/diagnostics/traffic/interface) whose byte counters are shown;
// defaults to "wan".
type opnsenseWidget struct{}

type opnsenseConfig struct {
	WAN string `json:"wan"`
}

type opnsenseActivityResponse struct {
	Headers []string `json:"headers"`
}

type opnsenseInterfaceStats struct {
	BytesTransmitted json.Number `json:"bytes transmitted"`
	BytesReceived    json.Number `json:"bytes received"`
}

type opnsenseInterfaceResponse struct {
	Interfaces map[string]opnsenseInterfaceStats `json:"interfaces"`
}

// opnsenseCPUIdleRe matches the "N.NN% idle" fragment in the activity
// endpoint's headers[2] top-style CPU summary line.
var opnsenseCPUIdleRe = regexp.MustCompile(`([0-9.]+)% idle`)

// opnsenseMemActiveRe matches the "Mem: <value> Active," fragment in the
// activity endpoint's headers[3] top-style memory summary line.
var opnsenseMemActiveRe = regexp.MustCompile(`Mem: (.+) Active,`)

func (opnsenseWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	if cfg.URL == "" {
		return nil, errors.New("opnsense widget: url is required")
	}
	username := cfg.Secrets[secretUsername]
	password := cfg.Secrets[secretPassword]

	var opnCfg opnsenseConfig
	if len(cfg.Config) > 0 {
		if err := json.Unmarshal(cfg.Config, &opnCfg); err != nil {
			return nil, fmt.Errorf("decoding widget config: %w", err)
		}
	}
	wan := opnCfg.WAN
	if wan == "" {
		wan = "wan"
	}

	base := strings.TrimRight(cfg.URL, "/")

	activityReq, err := opnsenseRequest(ctx, base+"/api/diagnostics/activity/getActivity", username, password)
	if err != nil {
		return nil, err
	}
	var activity opnsenseActivityResponse
	if fields, err := doJSONRequest(httpClient, activityReq, &activity); fields != nil || err != nil {
		return fields, err
	}

	interfaceReq, err := opnsenseRequest(ctx, base+"/api/diagnostics/traffic/interface", username, password)
	if err != nil {
		return nil, err
	}
	var iface opnsenseInterfaceResponse
	if fields, err := doJSONRequest(httpClient, interfaceReq, &iface); fields != nil || err != nil {
		return fields, err
	}

	fields := []Field{}
	if len(activity.Headers) > 3 {
		if m := opnsenseCPUIdleRe.FindStringSubmatch(activity.Headers[2]); m != nil {
			if idle, err := strconv.ParseFloat(m[1], 64); err == nil {
				cpuPct := int(100 - idle + 0.5)
				fields = append(fields, Field{Label: labelCPU, Value: fmt.Sprintf("%d%%", cpuPct), Percent: &cpuPct, Highlight: usageHighlight(&cpuPct)})
			}
		}
		if m := opnsenseMemActiveRe.FindStringSubmatch(activity.Headers[3]); m != nil {
			fields = append(fields, Field{Label: labelMemory, Value: strings.TrimSpace(m[1])})
		}
	}

	if stats, ok := iface.Interfaces[wan]; ok {
		if tx, err := stats.BytesTransmitted.Float64(); err == nil {
			fields = append(fields, Field{Label: labelWANUpload, Value: formatBytesHumanized(tx)})
		}
		if rx, err := stats.BytesReceived.Float64(); err == nil {
			fields = append(fields, Field{Label: labelWANDownload, Value: formatBytesHumanized(rx)})
		}
	}

	return fields, nil
}

func (opnsenseWidget) Sample(WidgetConfig) []Field {
	cpuPct := 12
	return []Field{
		{Label: labelCPU, Value: fmt.Sprintf("%d%%", cpuPct), Percent: &cpuPct, Highlight: usageHighlight(&cpuPct)},
		{Label: labelMemory, Value: "1.8G"},
		{Label: labelWANUpload, Value: opnsenseSampleWANUpload},
		{Label: labelWANDownload, Value: "84.7 GB"},
	}
}

// opnsenseRequest builds a GET request against the diagnostics API with
// Basic auth set when username is non-empty — OPNsense's own API key/secret
// scheme is presented as a Basic-auth pair, matching homepage's
// genericProxyHandler behavior for widgets that set both username and
// password.
func opnsenseRequest(ctx context.Context, endpoint, username, password string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	if username != "" {
		req.SetBasicAuth(username, password)
	}
	return req, nil
}

// formatBytesHumanized renders a byte count as a human-readable string
// (B/KB/MB/GB/TB, 1000-based to match homepage's t("common.bytes", ...)
// formatting rather than binary KiB/MiB units).
func formatBytesHumanized(bytes float64) string {
	const unit = 1000.0
	if bytes < unit {
		return fmt.Sprintf("%.0f B", bytes)
	}
	div, exp := unit, 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", bytes/div, "KMGT"[exp])
}
