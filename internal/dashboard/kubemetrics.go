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
	cpuLabel, memoryLabel := labelCPU, labelMemory
	if len(cfg.Config) > 0 {
		var c kubeMetricsConfig
		if err := json.Unmarshal(cfg.Config, &c); err != nil {
			return nil, fmt.Errorf("decoding widget config: %w", err)
		}
		if c.CPULabel != "" {
			cpuLabel = c.CPULabel
		}
		if c.MemoryLabel != "" {
			memoryLabel = c.MemoryLabel
		}
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

	return []Field{
		{Label: cpuLabel, Value: formatCPU(cpuUsed, cpuTotal)},
		{Label: memoryLabel, Value: formatMemory(memUsed, memTotal)},
	}, nil
}

// formatCPU renders CPU as "used / total cores (pct%)", using millicores for
// precision and omitting the percentage when total capacity is unknown.
func formatCPU(used, total resource.Quantity) string {
	usedCores := float64(used.MilliValue()) / 1000
	totalCores := float64(total.MilliValue()) / 1000
	if totalCores == 0 {
		return fmt.Sprintf("%s cores", trimFloat(usedCores))
	}
	pct := usedCores / totalCores * 100
	return fmt.Sprintf("%s / %s cores (%d%%)", trimFloat(usedCores), trimFloat(totalCores), int(pct+0.5))
}

// formatMemory renders memory as "used / total GiB (pct%)", omitting the
// percentage when total capacity is unknown.
func formatMemory(used, total resource.Quantity) string {
	usedGiB := float64(used.Value()) / bytesPerGiB
	totalGiB := float64(total.Value()) / bytesPerGiB
	if totalGiB == 0 {
		return fmt.Sprintf("%s GiB", trimFloat(usedGiB))
	}
	pct := usedGiB / totalGiB * 100
	return fmt.Sprintf("%s / %s GiB (%d%%)", trimFloat(usedGiB), trimFloat(totalGiB), int(pct+0.5))
}

// trimFloat formats f with one decimal place, dropping a trailing ".0".
func trimFloat(f float64) string {
	s := strconv.FormatFloat(f, 'f', 1, 64)
	if len(s) > 2 && s[len(s)-2:] == ".0" {
		return s[:len(s)-2]
	}
	return s
}
