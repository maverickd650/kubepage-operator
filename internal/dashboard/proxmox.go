package dashboard

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

func init() {
	Register("proxmox", &proxmoxWidget{})
}

// proxmoxWidget polls a Proxmox VE cluster's /api2/json/cluster/resources
// endpoint for VM/LXC counts and aggregate node CPU/memory usage, matching
// gethomepage/homepage's proxmox widget.
//
// Auth is Proxmox's API-token scheme, sent as a single "Authorization:
// PVEAPIToken=<username>=<password>" header (not Bearer/Basic) —
// Secrets["username"] is "<user>!<tokenid>" (e.g. "root@pam!homepage") and
// Secrets["password"] is the token's secret value, matching homepage's own
// field naming. Config: {"node": "pve1", "insecureTLS": false} — node
// restricts the VM/LXC/CPU/memory counts to a single cluster node (unset
// aggregates the whole cluster); insecureTLS is the same self-signed-cert
// opt-in unifi.go's InsecureTLS provides, since Proxmox VE commonly runs
// with a self-signed certificate out of the box.
type proxmoxWidget struct{}

type proxmoxConfig struct {
	Node        string `json:"node"`
	InsecureTLS bool   `json:"insecureTLS"`
}

type proxmoxResource struct {
	Type     string  `json:"type"`
	Status   string  `json:"status"`
	Node     string  `json:"node"`
	Template int     `json:"template"`
	MaxMem   float64 `json:"maxmem"`
	Mem      float64 `json:"mem"`
	MaxCPU   float64 `json:"maxcpu"`
	CPU      float64 `json:"cpu"`
}

type proxmoxClusterResourcesResponse struct {
	Data []proxmoxResource `json:"data"`
}

const (
	labelVMs = "VMs"
	labelLXC = "LXC"
)

func (proxmoxWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	if cfg.URL == "" {
		return nil, errors.New("proxmox widget: url is required")
	}
	username := cfg.Secrets[secretUsername]
	password := cfg.Secrets[secretPassword]
	if username == "" || password == "" {
		return nil, errors.New("proxmox widget: secrets.username and secrets.password are required")
	}

	var proxCfg proxmoxConfig
	if len(cfg.Config) > 0 {
		if err := json.Unmarshal(cfg.Config, &proxCfg); err != nil {
			return nil, fmt.Errorf("decoding widget config: %w", err)
		}
	}

	endpoint := strings.TrimRight(cfg.URL, "/") + "/api2/json/cluster/resources"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s=%s", username, password))

	client := proxmoxHTTPClient(httpClient, cfg.URL, proxCfg.InsecureTLS)
	var parsed proxmoxClusterResourcesResponse
	if fields, err := doJSONRequest(client, req, &parsed); fields != nil || err != nil {
		return fields, err
	}

	var vmsTotal, vmsRunning, lxcTotal, lxcRunning int
	var maxMem, mem, maxCPU, cpu float64
	var nodes int
	for _, r := range parsed.Data {
		if proxCfg.Node != "" && r.Node != proxCfg.Node {
			continue
		}
		switch r.Type {
		case "qemu":
			if r.Template != 0 {
				continue
			}
			vmsTotal++
			if r.Status == apiStatusRunning {
				vmsRunning++
			}
		case "lxc":
			if r.Template != 0 {
				continue
			}
			lxcTotal++
			if r.Status == apiStatusRunning {
				lxcRunning++
			}
		case "node":
			if r.Status != "online" {
				continue
			}
			nodes++
			maxMem += r.MaxMem
			mem += r.Mem
			maxCPU += r.MaxCPU
			cpu += r.CPU * r.MaxCPU
		}
	}

	fields := make([]Field, 0, 4)
	fields = append(fields,
		Field{Label: labelVMs, Value: fmt.Sprintf("%d / %d", vmsRunning, vmsTotal)},
		Field{Label: labelLXC, Value: fmt.Sprintf("%d / %d", lxcRunning, lxcTotal)},
	)
	if nodes == 0 || maxCPU == 0 || maxMem == 0 {
		return fields, nil
	}

	cpuPct := int(cpu/maxCPU*100 + 0.5)
	memPct := int(mem/maxMem*100 + 0.5)
	fields = append(fields,
		Field{Label: labelCPU, Value: fmt.Sprintf("%d%%", cpuPct), Percent: &cpuPct, Highlight: usageHighlight(&cpuPct)},
		Field{Label: labelMemory, Value: fmt.Sprintf("%d%%", memPct), Percent: &memPct, Highlight: usageHighlight(&memPct)},
	)
	return fields, nil
}

func (proxmoxWidget) Sample(WidgetConfig) []Field {
	cpuPct, memPct := 32, 54
	return []Field{
		{Label: labelVMs, Value: "6 / 8"},
		{Label: labelLXC, Value: "4 / 5"},
		{Label: labelCPU, Value: fmt.Sprintf("%d%%", cpuPct), Percent: &cpuPct, Highlight: usageHighlight(&cpuPct)},
		{Label: labelMemory, Value: fmt.Sprintf("%d%%", memPct), Percent: &memPct, Highlight: usageHighlight(&memPct)},
	}
}

// proxmoxInsecureClientCache holds one *http.Client per baseURL for
// insecureTLS controllers — same rationale as unifi.go's
// unifiInsecureClientCache, kept separate since each widget's cache is only
// ever read/written by that widget's own Poll. Bounded (see
// boundedClientCache) for the same reason unifiInsecureClientCache is.
var proxmoxInsecureClientCache = newBoundedClientCache()

// proxmoxHTTPClient returns client unchanged unless insecureTLS is set, in
// which case it returns a cached (or newly built and cached) client for
// baseURL with certificate verification disabled, mirroring
// unifi.go's unifiHTTPClient.
func proxmoxHTTPClient(client *http.Client, baseURL string, insecureTLS bool) *http.Client {
	if !insecureTLS {
		return client
	}

	return proxmoxInsecureClientCache.getOrCreate(baseURL, func() *http.Client {
		transport := newGuardedTransport(nil)
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit per-widget opt-in, see proxmoxConfig.InsecureTLS
		return &http.Client{Timeout: client.Timeout, Transport: transport}
	})
}
