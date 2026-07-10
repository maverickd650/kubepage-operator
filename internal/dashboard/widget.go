// Package dashboard implements the native dashboard subcommand (D11 / Phase
// 6): a Go+htmx renderer that reads a Dashboard's bound CRDs directly and
// polls each ServiceWidget's upstream, replacing the homepage image wrapper.
package dashboard

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"slices"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Highlight severity levels a widget may set on a Field; see Field.Highlight.
const (
	HighlightGood   = "good"
	HighlightWarn   = "warn"
	HighlightDanger = "danger"
)

// Field is one labeled value shown on a widget's card (e.g. "Status" →
// "Healthy"). Widgets return a small ordered slice of these; the renderer
// doesn't interpret them beyond display order, except for Percent/Highlight
// below.
type Field struct {
	Label string
	Value string

	// Percent is an optional 0-100 usage percentage. When set, the renderer
	// draws a usage bar under the value, matching homepage's Resource
	// component's <UsageBar percent={...}>.
	Percent *int

	// Highlight optionally flags this field's stat chip with a severity
	// color: "good", "warn", or "danger". Empty means no highlight. Set
	// either by a widget with its own notion of a threshold (e.g.
	// kubemetrics' CPU/memory percentage) or, after Poll returns, by
	// highlight.go's generic rule engine evaluating the ServiceWidget's
	// configured Highlight rules (keyed by Label) — the latter only runs
	// for fields a widget hasn't already highlighted itself.
	Highlight string
}

// WidgetConfig is everything a Widget needs to poll its upstream, already
// resolved: URL is the widget's configured base URL, Secrets holds every
// entry of the CRD's Secrets map resolved to its plaintext value (the value
// never leaves this process), and Config is the widget's passthrough
// config block verbatim.
type WidgetConfig struct {
	URL     string
	Secrets map[string]string
	Config  json.RawMessage
}

// Widget polls one upstream service and returns the fields to display for
// it. Implementations must be safe for concurrent use: the poller calls Poll
// for every bound widget instance of a given type using the same registered
// Widget value.
type Widget interface {
	Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error)
}

// ClusterWidget is an optional interface a registered Widget may also
// implement when it reads from the Kubernetes API (e.g. metrics.k8s.io)
// rather than polling an HTTP upstream. When a widget implements it, the
// poller calls PollCluster with a cluster-scoped reader instead of Poll, so
// the widget never needs an HTTP client. Widgets still register as a Widget
// (their Poll may be a no-op returning an error) so Lookup keeps working.
type ClusterWidget interface {
	PollCluster(ctx context.Context, reader client.Reader, cfg WidgetConfig) ([]Field, error)
}

// registry maps a ServiceWidget's Type (e.g. "prometheus") to the Widget
// implementation that polls it. Populated via Register, typically from each
// widget implementation's init().
var registry = map[string]Widget{}

// Register associates widgetType with w. Intended to be called from an
// init() function; panics on a duplicate registration since that indicates
// a programming error, not a runtime condition.
func Register(widgetType string, w Widget) {
	if _, exists := registry[widgetType]; exists {
		panic("dashboard: widget type already registered: " + widgetType)
	}
	registry[widgetType] = w
}

// Lookup returns the Widget registered for widgetType, if any.
func Lookup(widgetType string) (Widget, bool) {
	w, ok := registry[widgetType]
	return w, ok
}

// RegisteredTypes returns every widget type currently registered, sorted.
// Used by internal/controller's widget-type policy test
// (widget_type_policy_test.go) to catch a widget added here without also
// adding it to the corresponding CEL enum on the CRD types — see that test
// for why this drift can't otherwise be caught short of a real apiserver
// rejecting a previously-valid type.
func RegisteredTypes() []string {
	return slices.Sorted(maps.Keys(registry))
}

// Sampler is implemented by every registered Widget to provide
// representative placeholder Fields for the preview subcommand's
// --sample-data mode (see Poller.SampleData), so a preview renders populated
// cards without a reachable upstream. Sample must be deterministic — no
// randomness or clock reads — since its output is asserted against directly
// in tests and golden fixtures. cfg is the same WidgetConfig Poll would
// receive except Secrets is always empty: sample mode never resolves
// secrets. A widget whose display shape depends on its own config
// (customapi, prometheusmetric) should read cfg.Config and echo the
// operator's own configured labels back with placeholder values, rather
// than a generic fallback that reveals nothing about their setup.
type Sampler interface {
	Sample(cfg WidgetConfig) []Field
}
