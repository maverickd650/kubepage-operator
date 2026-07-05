package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func init() {
	Register("kubemetrics", &kubeMetricsWidget{})
}

// kubeMetricsWidget is a header InfoWidget that shows live cluster-wide CPU
// and memory usage, sourced from metrics-server (metrics.k8s.io) for usage and
// the core Node objects for capacity. Unlike the HTTP-polled widgets it reads
// the Kubernetes API, so it implements ClusterWidget and the poller calls
// PollCluster rather than Poll. Config is an optional JSON object:
// {"cpuLabel": "<label>", "memoryLabel": "<label>"} overriding the defaults.
type kubeMetricsWidget struct{}

type kubeMetricsConfig struct {
	CPULabel    string `json:"cpuLabel"`
	MemoryLabel string `json:"memoryLabel"`
}

const bytesPerGiB = 1024 * 1024 * 1024

// Poll satisfies the Widget interface so the type can live in the registry,
// but kubemetrics reads the cluster API: the poller always prefers
// PollCluster, so this is never reached.
func (kubeMetricsWidget) Poll(context.Context, *http.Client, WidgetConfig) ([]Field, error) {
	return nil, errors.New("kubemetrics: cluster-only widget")
}

func (kubeMetricsWidget) PollCluster(ctx context.Context, reader client.Reader, cfg WidgetConfig) ([]Field, error) {
	cpuLabel, memoryLabel, err := resolveKubeMetricsLabels(cfg)
	if err != nil {
		return nil, err
	}

	var nodeMetrics metricsv1beta1.NodeMetricsList
	if err := reader.List(ctx, &nodeMetrics); err != nil {
		return []Field{{Label: labelStatus, Value: statusUnreach}}, nil
	}

	var cpuUsed, memUsed resource.Quantity
	for i := range nodeMetrics.Items {
		usage := nodeMetrics.Items[i].Usage
		cpuUsed.Add(usage[corev1.ResourceCPU])
		memUsed.Add(usage[corev1.ResourceMemory])
	}

	var nodes corev1.NodeList
	if err := reader.List(ctx, &nodes); err != nil {
		return nil, fmt.Errorf("listing nodes: %w", err)
	}

	var cpuTotal, memTotal resource.Quantity
	for i := range nodes.Items {
		alloc := nodes.Items[i].Status.Allocatable
		cpuTotal.Add(alloc[corev1.ResourceCPU])
		memTotal.Add(alloc[corev1.ResourceMemory])
	}

	cpuValue, cpuPct := formatCPU(cpuUsed, cpuTotal)
	memValue, memPct := formatMemory(memUsed, memTotal)
	return []Field{
		{Label: cpuLabel, Value: cpuValue, Percent: cpuPct, Highlight: usageHighlight(cpuPct)},
		{Label: memoryLabel, Value: memValue, Percent: memPct, Highlight: usageHighlight(memPct)},
	}, nil
}

// resolveKubeMetricsLabels decodes cfg.Config's cpuLabel/memoryLabel
// overrides, falling back to labelCPU/labelMemory when unset — the one
// piece of config-decode logic PollCluster and Sample share, kept here so
// they can't drift out of sync with each other.
func resolveKubeMetricsLabels(cfg WidgetConfig) (cpuLabel, memoryLabel string, err error) {
	cpuLabel, memoryLabel = labelCPU, labelMemory
	if len(cfg.Config) == 0 {
		return cpuLabel, memoryLabel, nil
	}
	var c kubeMetricsConfig
	if err := json.Unmarshal(cfg.Config, &c); err != nil {
		return cpuLabel, memoryLabel, fmt.Errorf("decoding widget config: %w", err)
	}
	if c.CPULabel != "" {
		cpuLabel = c.CPULabel
	}
	if c.MemoryLabel != "" {
		memoryLabel = c.MemoryLabel
	}
	return cpuLabel, memoryLabel, nil
}

// Sample honors cfg.Config's cpuLabel/memoryLabel overrides the same way
// PollCluster does, so a preview reflects the operator's own configured
// labels rather than a generic fallback. A decode error is ignored — unlike
// PollCluster, Sample has no error return, so malformed config just falls
// back to the default labels rather than producing an error card.
func (kubeMetricsWidget) Sample(cfg WidgetConfig) []Field {
	cpuLabel, memoryLabel, _ := resolveKubeMetricsLabels(cfg)

	cpuPct, memPct := 65, 92
	return []Field{
		{Label: cpuLabel, Value: "2.6 / 4 cores (65%)", Percent: &cpuPct, Highlight: usageHighlight(&cpuPct)},
		{Label: memoryLabel, Value: "11 / 12 GiB (92%)", Percent: &memPct, Highlight: usageHighlight(&memPct)},
	}
}

// usageHighlight flags a usage percentage as "warn" (>=75%) or "danger"
// (>=90%), or "" below that or when pct is unknown (nil, no capacity data).
func usageHighlight(pct *int) string {
	switch {
	case pct == nil:
		return ""
	case *pct >= 90:
		return HighlightDanger
	case *pct >= 75:
		return HighlightWarn
	default:
		return ""
	}
}

// formatCPU renders CPU as "used / total cores (pct%)", using millicores for
// precision and omitting the percentage when total capacity is unknown. The
// returned *int is the same percentage, for the usage bar; nil when omitted.
func formatCPU(used, total resource.Quantity) (string, *int) {
	usedCores := float64(used.MilliValue()) / 1000
	totalCores := float64(total.MilliValue()) / 1000
	if totalCores == 0 {
		return fmt.Sprintf("%s cores", trimFloat(usedCores)), nil
	}
	pct := int(usedCores/totalCores*100 + 0.5)
	return fmt.Sprintf("%s / %s cores (%d%%)", trimFloat(usedCores), trimFloat(totalCores), pct), &pct
}

// formatMemory renders memory as "used / total GiB (pct%)", omitting the
// percentage when total capacity is unknown. The returned *int is the same
// percentage, for the usage bar; nil when omitted.
func formatMemory(used, total resource.Quantity) (string, *int) {
	usedGiB := float64(used.Value()) / bytesPerGiB
	totalGiB := float64(total.Value()) / bytesPerGiB
	if totalGiB == 0 {
		return fmt.Sprintf("%s GiB", trimFloat(usedGiB)), nil
	}
	pct := int(usedGiB/totalGiB*100 + 0.5)
	return fmt.Sprintf("%s / %s GiB (%d%%)", trimFloat(usedGiB), trimFloat(totalGiB), pct), &pct
}

// trimFloat formats f with one decimal place, dropping a trailing ".0".
func trimFloat(f float64) string {
	s := strconv.FormatFloat(f, 'f', 1, 64)
	if len(s) > 2 && s[len(s)-2:] == ".0" {
		return s[:len(s)-2]
	}
	return s
}
