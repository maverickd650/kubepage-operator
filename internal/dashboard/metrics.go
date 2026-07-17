package dashboard

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Poll metrics for the dashboard process. Each dashboard Deployment is its
// own OS process (the manager image run with the "dashboard" subcommand), so
// registering on the default registerer here never collides with the
// manager's own controller-runtime metrics.
var (
	widgetPollTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "kubepage_widget_poll_total",
		Help: "Total widget upstream polls, by widget type and result (success/error).",
	}, []string{"type", "result"})

	widgetPollDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "kubepage_widget_poll_duration_seconds",
		Help:    "Duration of widget upstream polls, by widget type.",
		Buckets: prometheus.DefBuckets,
	}, []string{"type"})

	monitorUp = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "kubepage_monitor_up",
		Help: "Whether a ServiceCard's monitor probe last succeeded (1) or failed (0), by source (\"http\" for monitor, \"pods\" for app/podSelector; a pod monitor's Partial status still counts as up).",
	}, []string{"service", "source"})
)

func observePoll(widgetType string, err error, seconds float64) {
	widgetPollDuration.WithLabelValues(widgetType).Observe(seconds)
	result := "success"
	if err != nil {
		result = "error"
	}
	widgetPollTotal.WithLabelValues(widgetType, result).Inc()
}
