package dashboard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

var pollerLog = ctrl.Log.WithName("dashboard-poller")

// maxConcurrentPolls bounds how many widget/InfoWidget upstream polls run at
// once per poll cycle, so one slow or unreachable service can't make every
// other card lag a full cycle behind while the poller works through a long
// service list sequentially.
const maxConcurrentPolls = 8

// statusStyleDot is the default ServiceCard.Spec.StatusStyle, rendered by
// cards.templ as a bare colored dot; statusStyleBasic renders a colored
// status pill instead: the status word plus latency/ready-count detail.
const (
	statusStyleDot   = "dot"
	statusStyleBasic = "basic"
)

// statusPartial is the pod monitor's three-valued status an HTTP monitor
// probe never reports: some, but not all, of a PodSelector/App's matched
// pods are Ready. Renders amber, between statusUp's green and statusDown's
// red — see cards.templ's status-Partial CSS class.
const statusPartial = "Partial"

// monitorSourceHTTP and monitorSourcePods are the monitorUp gauge's "source"
// label values, distinguishing a ServiceEntry's HTTP monitor
// (Monitor) from its pod monitor (App/PodSelector) — the two can
// now be configured at once (see docs/design/combined-monitor.md), so they
// must not share one label series.
const (
	monitorSourceHTTP = "http"
	monitorSourcePods = "pods"
)

// Poller periodically lists the ServiceCards bound to one Dashboard,
// resolves each widget's secrets and config, polls every widget whose type
// is registered, and writes the results into Store. Polling runs on its own
// interval rather than per browser request, so a slow or unreachable
// upstream never blocks a page load.
type Poller struct {
	// Reader lists CRDs; expected to be a cache-backed (informer) client
	// scoped to Namespace, per D11's "reads its Dashboard's bound CRDs via a
	// controller-runtime cache".
	Reader client.Reader

	// SecretReader resolves Secret values directly, deliberately not
	// cache-backed: secret contents shouldn't sit in an informer's
	// in-memory store for the lifetime of the process.
	SecretReader client.Reader

	// KubeReader serves cluster-scoped reads for ClusterWidget types (e.g.
	// kubemetrics reading nodes and metrics.k8s.io). Deliberately not
	// cache-backed: metrics.k8s.io doesn't support watch, and nodes are
	// cluster-scoped while the CRD cache is namespace-scoped.
	KubeReader client.Reader

	Namespace     string
	DashboardName string
	Interval      time.Duration
	HTTPClient    *http.Client
	Store         *Store

	// Broadcast, when set, is published to once at the end of every poll
	// cycle so any open SSE connection (Server.handleEvents) wakes up and
	// checks whether the fragment/header content it last sent has actually
	// changed — see Broadcaster's doc comment. Optional: nil disables the
	// push path entirely, leaving htmx's interval polling as the only
	// refresh mechanism (e.g. in tests that construct a Poller directly).
	Broadcast *Broadcaster

	// GatewayAPIEnabled reports whether this cluster has Gateway API CRDs
	// installed; see dashboard.Options.GatewayAPIEnabled's doc comment.
	// Gates whether pollOnce ever attempts to List HTTPRoutes for HTTPRoute
	// discovery.
	GatewayAPIEnabled bool

	// warnHTTPRouteUnavailableOnce logs the "Skipping HTTPRoute discovery"
	// message at most once per process, instead of once per poll cycle, for
	// a Dashboard whose spec.discovery.sources includes HTTPRoute on a
	// cluster without Gateway API installed.
	warnHTTPRouteUnavailableOnce sync.Once

	// SampleData, when set, replaces every real upstream poll and monitor
	// probe with a widget's Sampler.Sample output (or a canned "Up" status),
	// so preview mode (internal/preview) can render fully populated cards
	// without a reachable upstream. Set only by dashboard.RunPreview's
	// --sample-data plumbing — never true for the in-cluster dashboard mode.
	// Sample polling skips secret resolution, CA-cert handling, and poll
	// metrics entirely: the data isn't real, so it shouldn't require secret
	// material to be present locally, and it shouldn't pollute Prometheus
	// metrics as if a real poll succeeded.
	SampleData bool

	// Preview, when set, means this Poller is running under
	// dashboard.RunPreview rather than in-cluster: always true for preview
	// mode, independent of SampleData (SampleData is opt-in via
	// --sample-data; Preview is not). A laptop can never dial a
	// cluster-internal URL, so Preview mode ignores InternalURL entirely
	// (both explicit values and the InternalURLAuto sentinel — see
	// resolveBaseURL) and falls back to sample data for exactly the gap that
	// leaves: an HTTP monitor or widget whose only URL was the ignored
	// InternalURL gets a fabricated result instead of a doomed real probe;
	// the pod monitor (App/PodSelector) always gets one too, since preview
	// has no cluster to list pods from at all. Anything with an explicit
	// URL, or a usable Href, still polls for real — see monitor,
	// resolveBaseURL, and pollWidget. When SampleData is also set, SampleData
	// already covers every poll/probe, so Preview's own fabrication paths
	// are never reached.
	Preview bool

	// monitorLabels is the set of monitorUp (service, source) label pairs
	// reported on the previous poll cycle, keyed by
	// "namespace/name/entryName\x1f<source>" (see monitor's doc comment and
	// monitorLabelKey). pollOnce diffs the current cycle's set against this
	// to delete a label series for an entry — or one source of it — that's
	// since been deleted or had that monitor removed — monitorUp has no
	// other pruning path, unlike Store's per-cycle Prune. Only ever read/
	// written from pollOnce, which Run never calls concurrently with itself,
	// so this needs no lock of its own.
	monitorLabels map[string]bool

	// widgetLastPolled tracks the last time a widget with its own
	// PollIntervalSeconds override was actually polled, keyed the same as
	// Store (e.g. "ns/name/0", "header/name"). Widgets without an override
	// poll every cycle and never appear here. Guarded by widgetLastPolledMu
	// since, unlike monitorLabels, it's read and written from the
	// concurrent per-widget goroutines pollOnce fans out via run(), not just
	// from pollOnce itself.
	widgetLastPolledMu sync.Mutex
	widgetLastPolled   map[string]time.Time

	// clampWarned is the set of widget keys duePoll has already logged a
	// "your PollIntervalSeconds override is shorter than the poller interval
	// and has no effect" warning for, so the message is emitted once per
	// widget rather than every cycle. Guarded by widgetLastPolledMu (same
	// concurrency shape as widgetLastPolled) and pruned alongside it.
	clampWarned map[string]bool

	// caKeysUsed is the set of caClientCache keys (see caClient) resolved
	// during the poll cycle currently running, reset at the start of each
	// pollOnce and consulted by pruneCAClientCache after wg.Wait() to evict
	// any cached *http.Client — and its idle-connection pool — for a caCert
	// bundle no widget referenced this cycle (e.g. after a caCert rotation).
	// Guarded by caKeysUsedMu for the same reason as widgetLastPolledMu.
	caKeysUsedMu sync.Mutex
	caKeysUsed   map[string]bool
}

// Run polls once immediately, then on Interval until ctx is done.
func (p *Poller) Run(ctx context.Context) {
	p.pollOnce(ctx)

	ticker := time.NewTicker(p.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.pollOnce(ctx)
		}
	}
}

func (p *Poller) pollOnce(ctx context.Context) {
	p.caKeysUsedMu.Lock()
	p.caKeysUsed = map[string]bool{}
	p.caKeysUsedMu.Unlock()

	var keepMu sync.Mutex
	keep := map[string]bool{}
	markKeep := func(key string) {
		keepMu.Lock()
		keep[key] = true
		keepMu.Unlock()
	}

	var monitorLabelsMu sync.Mutex
	monitorLabels := map[string]bool{}
	recordMonitorLabel := func(label, source string) {
		monitorLabelsMu.Lock()
		monitorLabels[monitorLabelKey(label, source)] = true
		monitorLabelsMu.Unlock()
	}

	// run bounds how many of the closures it's given run concurrently
	// (maxConcurrentPolls), and waits for all of them via wg.Wait below.
	sem := make(chan struct{}, maxConcurrentPolls)
	var wg sync.WaitGroup
	run := func(fn func()) {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			fn()
		})
	}

	defaultStatusStyle, defaultHideErrors := p.siteDefaults(ctx)
	// One Get of the Dashboard covers discovery config, widget defaults, and
	// allowed monitor namespaces — previously a per-field accessor Got it
	// separately for each, three round-trips to the same object every cycle.
	// dashboardSpecForPoll's single-Get result is reused below instead
	// (discoverySpec/widgetDefaults survive as thin wrappers over it, only
	// because unit tests exercise them individually).
	dashboardSpec, dashboardOK := p.dashboardSpecForPoll(ctx)
	widgetDefaults := dashboardSpec.WidgetDefaults
	allowedMonitorNamespaces := dashboardSpec.MonitorNamespaces
	clusterDomain := defaultClusterDomain
	if dashboardSpec.ClusterDomain != nil && *dashboardSpec.ClusterDomain != "" {
		clusterDomain = *dashboardSpec.ClusterDomain
	}

	dashCount, err := namespaceDashboardCount(ctx, p.Reader, p.Namespace)
	if err != nil {
		pollerLog.Error(err, "counting Dashboards")
		return
	}

	var entries pagev1alpha1.ServiceCardList
	if err := p.Reader.List(ctx, &entries, client.InNamespace(p.Namespace)); err != nil {
		pollerLog.Error(err, "listing ServiceCards")
		return
	}
	for _, entry := range entries.Items {
		if !pagev1alpha1.BoundTo(pagev1alpha1.RefName(entry.Spec.DashboardRef), p.DashboardName, dashCount) {
			continue
		}

		for entryIdx, se := range entry.Spec.Entries() {
			namespace, crName := entry.Namespace, entry.Name

			// Each entry's monitor probe (if any) and its widget polls all
			// run as part of the same bounded pool: probing the monitor
			// first, then fanning its widgets out into their own run()
			// slots, rather than probing every entry's monitor sequentially
			// before any widget poll starts. Previously a single slow/
			// unreachable monitor delayed every other card in the cycle by
			// up to its full HTTP timeout.
			run(func() {
				m := p.monitor(ctx, namespace, crName, se, defaultStatusStyle, allowedMonitorNamespaces, clusterDomain, recordMonitorLabel)

				if len(se.Widgets) == 0 {
					// A service with a monitor but no widget still gets one
					// card so its up/down status is visible.
					if m.status == "" && m.podStatus == "" {
						return
					}
					key := fmt.Sprintf("%s/%s/%d/monitor", namespace, crName, entryIdx)
					markKeep(key)
					p.pollWidget(ctx, key, namespace, se, nil, m, defaultHideErrors, widgetDefaults)
					return
				}

				for i := range se.Widgets {
					key := fmt.Sprintf("%s/%s/%d/%d", namespace, crName, entryIdx, i)
					markKeep(key)
					widget := &se.Widgets[i]
					run(func() {
						p.pollWidget(ctx, key, namespace, se, widget, m, defaultHideErrors, widgetDefaults)
					})
				}
			})
		}
	}

	if spec, ok := discoverySpecFromDashboard(dashboardSpec, dashboardOK); ok {
		if spec.HasSource(pagev1alpha1.DiscoverySourceIngress) {
			services, err := discoverServices(ctx, p.Reader, p.Namespace, p.KubeReader, extraDiscoveryNamespaces(spec, p.Namespace), spec)
			if err != nil {
				pollerLog.Error(err, "discovering Ingresses")
			} else {
				for _, svc := range services {
					markKeep(svc.Key)
					run(func() { p.pollDiscoveredService(ctx, svc, recordMonitorLabel) })
				}
			}
		}

		// HTTPRoute discovery (gap-analysis §4.7 fast-follow to Ingress
		// discovery) is opt-in via spec.discovery.sources and additionally
		// requires the cluster to actually have Gateway API installed —
		// attempting to List HTTPRoutes otherwise would fail on a
		// nonexistent Kind, not just missing RBAC. A Dashboard requesting it
		// without Gateway API installed gets a clear Available=False
		// condition from the controller (see reasonDiscoveryHTTPRouteUnavailable
		// in internal/controller); here we just skip the source gracefully
		// and log once per poll cycle rather than erroring repeatedly.
		if spec.HasSource(pagev1alpha1.DiscoverySourceHTTPRoute) {
			if !p.GatewayAPIEnabled {
				p.warnHTTPRouteUnavailableOnce.Do(func() {
					pollerLog.Info("Skipping HTTPRoute discovery: Gateway API CRDs are not installed in this cluster")
				})
			} else {
				routes, err := discoverHTTPRoutes(ctx, p.Reader, p.Namespace, p.KubeReader, extraDiscoveryNamespaces(spec, p.Namespace), spec)
				if err != nil {
					pollerLog.Error(err, "discovering HTTPRoutes")
				} else {
					for _, svc := range routes {
						markKeep(svc.Key)
						run(func() { p.pollDiscoveredService(ctx, svc, recordMonitorLabel) })
					}
				}
			}
		}
	}

	// Header widgets: poll InfoWidgets whose type is a registered (pollable)
	// widget — currently openmeteo. datetime/greeting carry no registered
	// widget, so they're skipped here and rendered statically by LoadSite.
	var infoWidgets pagev1alpha1.InfoWidgetList
	if err := p.Reader.List(ctx, &infoWidgets, client.InNamespace(p.Namespace)); err != nil {
		pollerLog.Error(err, "listing InfoWidgets")
	} else {
		for _, iw := range infoWidgets.Items {
			if !pagev1alpha1.BoundTo(pagev1alpha1.RefName(iw.Spec.DashboardRef), p.DashboardName, dashCount) {
				continue
			}
			for idx, entry := range iw.Spec.Entries() {
				if _, ok := Lookup(entry.Type); !ok {
					continue
				}
				key := fmt.Sprintf("header/%s/%d", iw.Name, idx)
				markKeep(key)
				run(func() { p.pollInfoWidget(ctx, key, iw, entry, widgetDefaults) })
			}
		}
	}

	wg.Wait()
	p.Store.Prune(keep)
	p.pruneMonitorMetrics(monitorLabels)
	p.pruneWidgetLastPolled(keep)
	p.pruneCAClientCache()

	// Skip currentHashes (two full templ renders plus LoadSite) and Publish
	// entirely when nobody's listening — with no open SSE connection there's
	// no one to notify, and this runs every poll cycle regardless of whether
	// the dashboard is even being viewed. Safe even though a client can
	// subscribe in the gap between this check and the next cycle: each new
	// SSE connection computes its own baseline hashes at connect time (see
	// Server.handleEvents), so it just waits for the next cycle's Publish,
	// the same as it would if it had connected moments earlier.
	if p.Broadcast != nil && p.Broadcast.HasSubscribers() {
		// Computed once here rather than once per SSE subscriber
		// (Server.handleEvents used to call this on every broadcast): with N
		// open dashboard tabs, that was N+ full LoadSite + Cards/Header
		// renders per poll cycle instead of one.
		fragment, header := currentHashes(ctx, p.Reader, p.Namespace, p.DashboardName, p.Store)
		p.Broadcast.Publish(fragment, header)
	}
}

// duePoll reports whether the widget at key should be polled this cycle,
// given its optional PollIntervalSeconds override: nil or <=0 means every
// cycle (the common case, tracked nowhere). A set override is floor-clamped
// to the poller's own Interval, since a shorter override would have no
// effect — pollOnce only runs once per Interval regardless. When that clamp
// actually shortens the user's override, it's logged once per widget key
// (not every cycle) so a user setting e.g. 5s against a 15s poller interval
// gets a signal that the value is a no-op, instead of silent floor-clamping.
// When it returns true, it also records now as key's last-polled time.
func (p *Poller) duePoll(key string, overrideSeconds *int32) bool {
	if overrideSeconds == nil || *overrideSeconds <= 0 {
		return true
	}
	requested := time.Duration(*overrideSeconds) * time.Second
	interval := max(requested, p.Interval)

	p.widgetLastPolledMu.Lock()
	defer p.widgetLastPolledMu.Unlock()
	if requested < p.Interval && !p.clampWarned[key] {
		pollerLog.Info("Widget pollIntervalSeconds is shorter than the poller interval and has no effect",
			"key", key, "overrideSeconds", *overrideSeconds, "pollerInterval", p.Interval)
		if p.clampWarned == nil {
			p.clampWarned = map[string]bool{}
		}
		p.clampWarned[key] = true
	}
	if last, ok := p.widgetLastPolled[key]; ok && time.Since(last) < interval {
		return false
	}
	if p.widgetLastPolled == nil {
		p.widgetLastPolled = map[string]time.Time{}
	}
	p.widgetLastPolled[key] = time.Now()
	return true
}

// pruneWidgetLastPolled deletes any widgetLastPolled/clampWarned entry not in
// this cycle's keep set, mirroring Store.Prune, so a deleted (or edited-
// away-from-an-override) widget's bookkeeping doesn't accumulate forever.
func (p *Poller) pruneWidgetLastPolled(keep map[string]bool) {
	p.widgetLastPolledMu.Lock()
	defer p.widgetLastPolledMu.Unlock()
	for key := range p.widgetLastPolled {
		if !keep[key] {
			delete(p.widgetLastPolled, key)
		}
	}
	for key := range p.clampWarned {
		if !keep[key] {
			delete(p.clampWarned, key)
		}
	}
}

// pruneCAClientCache deletes any caClientCache entry not resolved during
// this poll cycle (tracked in caKeysUsed) — otherwise rotating or removing a
// widget's caCert leaves its old *http.Client, and the idle-connection pool
// behind it, cached forever. Unlike pruneWidgetLastPolled's keep set (marked
// for every widget bound this cycle, before its due-poll check), caKeysUsed
// only sees a caCert that was actually resolved this cycle: a widget with a
// PollIntervalSeconds override that isn't due yet won't mark its key, so its
// cached client can get evicted and rebuilt on its next due poll. That's a
// harmless extra TLS handshake, not a correctness issue — the rebuilt client
// still trusts the same CA bundle.
func (p *Poller) pruneCAClientCache() {
	p.caKeysUsedMu.Lock()
	used := p.caKeysUsed
	p.caKeysUsedMu.Unlock()

	caClientCache.mu.Lock()
	defer caClientCache.mu.Unlock()
	for key := range caClientCache.clients {
		if !used[key] {
			delete(caClientCache.clients, key)
		}
	}
}

// pruneMonitorMetrics deletes any monitorUp label series from the previous
// cycle that current (this cycle's labels) no longer reports, so a deleted
// ServiceCard — or one that's had its Monitor/App/PodSelector
// removed — doesn't leave a stale gauge value exported forever.
func (p *Poller) pruneMonitorMetrics(current map[string]bool) {
	for key := range p.monitorLabels {
		if !current[key] {
			label, source := splitMonitorLabelKey(key)
			monitorUp.DeleteLabelValues(label, source)
		}
	}
	p.monitorLabels = current
}

// monitorLabelKeySep separates a monitorUp "service" label from its "source"
// label in monitorLabels' bookkeeping keys — see monitorLabelKey.
const monitorLabelKeySep = "\x1f"

// monitorLabelKey and splitMonitorLabelKey encode/decode a (label, source)
// pair into monitorLabels' single-string map key. \x1f (ASCII unit
// separator) is used rather than "/" since label already contains "/"
// (namespace/crName/entryName).
func monitorLabelKey(label, source string) string {
	return label + monitorLabelKeySep + source
}

func splitMonitorLabelKey(key string) (label, source string) {
	label, source, _ = strings.Cut(key, monitorLabelKeySep)
	return label, source
}

// monitorProbeResult is monitor's combined result: the HTTP monitor
// (Monitor) result in status/statusStyle/latency, and the pod
// monitor (App/PodSelector) result in podStatus/podReadyText — either half
// may be empty when that source isn't configured for the entry. err, when
// non-empty, is a card-facing error message (e.g. a disallowed foreign
// namespace) that the caller should surface as the card's Err field, subject
// to the entry's own HideErrors setting.
type monitorProbeResult struct {
	status      string
	statusStyle string
	latency     string

	podStatus    string
	podReadyText string

	// baseURL is the entry's resolved base URL — InternalURL/Href, with the
	// InternalURLAuto sentinel resolved to an actual in-cluster URL — for
	// pollWidget to use in place of calling ServiceEntry.BaseURL() directly
	// (which can't perform the Service lookup "auto" requires).
	baseURL string

	err string
}

// monitor probes se's configured monitor sources: the HTTP monitor (Monitor,
// a URL or the "self" sentinel resolved to the entry's own base URL — see
// ServiceEntry.MonitorURL) and the pod monitor (App and/or PodSelector,
// freely combinable with the HTTP monitor — see
// docs/design/combined-monitor.md), returning both results at once. record
// is called once per configured source with the monitorUp
// label/source pair this probe reported under, so the caller can track which
// series are still live this cycle (see pruneMonitorMetrics). The label is
// "namespace/crName/entryName", so two entries defined in the same
// ServiceCard don't collide on one label series.
func (p *Poller) monitor(ctx context.Context, namespace, crName string, se pagev1alpha1.ServiceEntry, defaultStatusStyle string, allowedMonitorNamespaces []string, clusterDomain string, record func(label, source string)) monitorProbeResult {
	var result monitorProbeResult

	// Resolved unconditionally (not just when Monitor: self is set): a
	// widget-only entry with no monitor still needs its base URL resolved
	// for pollWidget, and resolveBaseURL is a no-op (no Service lookup) for
	// anything other than the InternalURLAuto sentinel.
	result.baseURL, result.err = p.resolveBaseURL(ctx, namespace, se, allowedMonitorNamespaces, clusterDomain)

	var httpFabricated, podFabricated bool
	if url := se.MonitorURLWithBase(result.baseURL); url != "" {
		result.status, result.latency = p.probeURL(ctx, url)
	} else if p.Preview && !p.SampleData && se.Monitor != nil && *se.Monitor != "" {
		// A monitor is configured (necessarily "monitor: self" — an explicit
		// URL always makes MonitorURLWithBase return non-empty above) but its
		// base URL resolved to "" because Preview mode ignored InternalURL
		// and there's no Href to fall back to either: there's nothing
		// reachable to probe, so report the same fabricated "Up" result
		// SampleData mode would, rather than skipping the monitor (silently
		// dropping the card's status) or probing an empty URL (a guaranteed
		// failure that would misrepresent the real service as Down).
		result.status, result.latency = "Up", sampleMonitorLatency
		httpFabricated = true
	}

	if selector := podMonitorSelector(se); selector != nil {
		if p.Preview && !p.SampleData {
			// Preview mode has no cluster to list pods from at all — unlike
			// the HTTP monitor, this isn't conditional on InternalURL being
			// the pod monitor's only URL; it's simply not resolvable from a
			// laptop under any configuration.
			result.podStatus, result.podReadyText = "Up", sampleMonitorReadyText
			podFabricated = true
		} else {
			var podErr string
			result.podStatus, result.podReadyText, podErr = p.probePodMonitor(ctx, namespace, se, selector, allowedMonitorNamespaces)
			if podErr != "" {
				result.err = podErr
			}
		}
	}

	if result.status == "" && result.podStatus == "" {
		return result
	}

	style := defaultStatusStyle
	if se.StatusStyle != nil {
		style = *se.StatusStyle
	}
	result.statusStyle = style

	// Sample-mode monitor results are fabricated, not observed, so they
	// don't get recorded into the monitorUp Prometheus gauge either — see
	// SampleData's doc comment.
	if p.SampleData {
		return result
	}

	label := namespace + "/" + crName + "/" + se.Name
	if result.status != "" && !httpFabricated {
		up := 0.0
		if result.status == "Up" {
			up = 1
		}
		monitorUp.WithLabelValues(label, monitorSourceHTTP).Set(up)
		record(label, monitorSourceHTTP)
	}
	if result.podStatus != "" && !podFabricated {
		// Partial counts as up in the gauge: the ready fraction is visible
		// on the card (PodReadyText), not in the metric.
		up := 0.0
		if result.podStatus == "Up" || result.podStatus == statusPartial {
			up = 1
		}
		monitorUp.WithLabelValues(label, monitorSourcePods).Set(up)
		record(label, monitorSourcePods)
	}
	return result
}

// sampleMonitorLatency and sampleMonitorReadyText are the canned monitor
// results SampleData mode reports for a configured monitor and
// app/podSelector, respectively — see probeURL/probePodMonitor.
const (
	sampleMonitorLatency   = "12 ms"
	sampleMonitorReadyText = "2/2 ready"
)

// podMonitorLabel is the standard label app.kubernetes.io/name=<app> derives
// its selector from — homepage parity, see ServiceEntry.App's doc comment.
const podMonitorLabel = "app.kubernetes.io/name"

// noMatchedPodsReadyText is the ready-count text podStatus/probePodMonitor
// report when nothing matched the pod monitor's selector at all (namespace
// disallowed, or a selector that simply matches no pods) — "0/0 ready" makes
// that "not found" case legible rather than blank.
const noMatchedPodsReadyText = "0/0 ready"

// podMonitorSelector resolves se's pod monitor selector: se.PodSelector wins
// when set (homepage's documented override semantics); otherwise se.App, if
// set, derives the standard app.kubernetes.io/name=<app> selector; nil when
// neither is set (no pod monitor configured for this entry).
func podMonitorSelector(se pagev1alpha1.ServiceEntry) *metav1.LabelSelector {
	if se.PodSelector != nil {
		return se.PodSelector
	}
	if se.App != nil {
		return &metav1.LabelSelector{MatchLabels: map[string]string{podMonitorLabel: *se.App}}
	}
	return nil
}

// probePodMonitor returns a fabricated "Up" status in SampleData mode
// instead of actually listing pods, so preview mode never needs pod RBAC to
// render a populated status. Otherwise resolves se's pod-list namespace
// (se.Namespace, defaulting to the ServiceCard's own namespace) and lists
// through podStatus.
func (p *Poller) probePodMonitor(ctx context.Context, namespace string, se pagev1alpha1.ServiceEntry, selector *metav1.LabelSelector, allowedMonitorNamespaces []string) (status, text, cardErr string) {
	if p.SampleData {
		return "Up", sampleMonitorReadyText, ""
	}

	podNamespace := namespace
	if se.Namespace != nil && *se.Namespace != "" {
		podNamespace = *se.Namespace
	}

	reader := p.Reader
	if podNamespace != namespace {
		// A foreign namespace isn't served by the namespace-scoped cached
		// Reader — and isn't allowed at all unless the Dashboard's
		// spec.monitorNamespaces explicitly names it (see
		// DashboardSpec.MonitorNamespaces' doc comment). A disallowed
		// namespace short-circuits to Down with a card error naming the
		// fix, rather than surfacing a raw RBAC-forbidden error from an
		// uncached List that was never actually attempted.
		if !slices.Contains(allowedMonitorNamespaces, podNamespace) {
			return statusDown, noMatchedPodsReadyText, fmt.Sprintf(
				"pod monitor namespace %q is not allowed: add it to this Dashboard's spec.monitorNamespaces",
				podNamespace)
		}
		reader = p.KubeReader
	}

	status, text = p.podStatus(ctx, reader, podNamespace, selector, se.Name)
	return status, text, ""
}

// probeURL returns a fabricated "Up" status in SampleData mode instead of
// actually probing url.
func (p *Poller) probeURL(ctx context.Context, url string) (status, latency string) {
	if p.SampleData {
		return "Up", sampleMonitorLatency
	}
	return monitorResult(ctx, p.HTTPClient, url)
}

// defaultClusterDomain is the fallback DashboardSpec.ClusterDomain used to
// build an "internalUrl: auto" entry's FQDN when the field is unset — the
// CRD's own +default marker only applies at object-creation time through the
// API server, so a Dashboard created before this field existed (or a fake
// client in tests) never has it defaulted retroactively.
const defaultClusterDomain = "cluster.local"

// autoInternalURLPortName is the Service port name resolveAutoInternalURL
// prefers when a Service exposes more than one; a Service with no port named
// this falls back to its first port.
const autoInternalURLPortName = "http"

// resolveBaseURL resolves se's base URL for pollWidget/the monitor's "self"
// probe: se.InternalURL when set and not the InternalURLAuto sentinel, else
// se.Href, else "" — mirroring ServiceEntry.BaseURL exactly, except that the
// InternalURLAuto sentinel is resolved here via a Service lookup
// (resolveAutoInternalURL) instead of being returned unresolved. cardErr is
// set only when auto resolution itself fails; it is never set for the
// non-auto cases, which can't fail.
//
// In Preview mode, InternalURL is ignored entirely — both an explicit value
// and the auto sentinel — falling straight through to Href: a laptop can
// never dial a cluster-internal URL, and there's no Service to look up
// "auto" against anyway, so treating auto as unset (rather than attempting,
// and failing, the lookup) avoids a spurious card error on every preview of
// a Dashboard that uses it.
func (p *Poller) resolveBaseURL(ctx context.Context, namespace string, se pagev1alpha1.ServiceEntry, allowedMonitorNamespaces []string, clusterDomain string) (url, cardErr string) {
	if se.InternalURL != nil && *se.InternalURL != "" && !p.Preview {
		if *se.InternalURL == pagev1alpha1.InternalURLAuto {
			return p.resolveAutoInternalURL(ctx, namespace, se, allowedMonitorNamespaces, clusterDomain)
		}
		return *se.InternalURL, ""
	}
	if se.Href != nil {
		return *se.Href, ""
	}
	return "", ""
}

// resolveAutoInternalURL resolves se's "internalUrl: auto" base URL: it looks
// up a Service for se.App (lookupAutoService) in se's pod-monitor namespace
// (se.Namespace, else namespace — the same field and default the pod monitor
// uses, gated by the same cross-namespace allowedMonitorNamespaces allowlist
// as probePodMonitor) and builds
// "http://<service>.<namespace>.svc.<clusterDomain>:<port>" from the result.
// Any failure (no app, disallowed namespace, zero/multiple Service matches,
// no ports) renders as a card error rather than a resolved URL — the same UX
// as a disallowed pod-monitor namespace.
func (p *Poller) resolveAutoInternalURL(ctx context.Context, namespace string, se pagev1alpha1.ServiceEntry, allowedMonitorNamespaces []string, clusterDomain string) (url, cardErr string) {
	if se.App == nil || *se.App == "" {
		return "", "internalUrl: auto requires app to be set"
	}

	svcNamespace := namespace
	if se.Namespace != nil && *se.Namespace != "" {
		svcNamespace = *se.Namespace
	}

	reader := p.Reader
	if svcNamespace != namespace {
		if !slices.Contains(allowedMonitorNamespaces, svcNamespace) {
			return "", fmt.Sprintf(
				"internalUrl: auto namespace %q is not allowed: add it to this Dashboard's spec.monitorNamespaces",
				svcNamespace)
		}
		reader = p.KubeReader
	}

	svc, cardErr := p.lookupAutoService(ctx, reader, svcNamespace, *se.App)
	if cardErr != "" {
		return "", cardErr
	}

	port := autoInternalURLPort(svc)
	if port == 0 {
		return "", fmt.Sprintf("internalUrl: auto found Service %q in namespace %q but it has no ports", svc.Name, svcNamespace)
	}

	domain := clusterDomain
	if domain == "" {
		domain = defaultClusterDomain
	}
	return fmt.Sprintf("http://%s.%s.svc.%s:%d", svc.Name, svcNamespace, domain, port), ""
}

// lookupAutoService resolves the Service "internalUrl: auto" derives its URL
// from: a Service named app, falling back to the standard
// app.kubernetes.io/name=app label selector (podMonitorLabel, the same label
// the App-derived pod monitor selector uses) when no Service has that name.
// The label-selector fallback requires exactly one match; zero or multiple
// renders a card error naming the candidates (multiple) or nothing found
// (zero) instead of guessing.
func (p *Poller) lookupAutoService(ctx context.Context, reader client.Reader, namespace, app string) (corev1.Service, string) {
	var svc corev1.Service
	err := reader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: app}, &svc)
	switch {
	case err == nil:
		return svc, ""
	case !apierrors.IsNotFound(err):
		pollerLog.Error(err, "getting Service for internalUrl: auto", "namespace", namespace, "app", app)
		return corev1.Service{}, fmt.Sprintf("internalUrl: auto failed to look up Service %q in namespace %q: %v", app, namespace, err)
	}

	var svcs corev1.ServiceList
	if err := reader.List(ctx, &svcs, client.InNamespace(namespace), client.MatchingLabels{podMonitorLabel: app}); err != nil {
		pollerLog.Error(err, "listing Services for internalUrl: auto", "namespace", namespace, "app", app)
		return corev1.Service{}, fmt.Sprintf("internalUrl: auto failed to list Services labeled %s=%q in namespace %q: %v", podMonitorLabel, app, namespace, err)
	}
	switch len(svcs.Items) {
	case 1:
		return svcs.Items[0], ""
	case 0:
		return corev1.Service{}, fmt.Sprintf(
			"internalUrl: auto found no Service named %q or labeled %s=%q in namespace %q",
			app, podMonitorLabel, app, namespace)
	default:
		names := make([]string, len(svcs.Items))
		for i, s := range svcs.Items {
			names[i] = s.Name
		}
		return corev1.Service{}, fmt.Sprintf(
			"internalUrl: auto found multiple Services labeled %s=%q in namespace %q: %s",
			podMonitorLabel, app, namespace, strings.Join(names, ", "))
	}
}

// autoInternalURLPort picks the Service port "internalUrl: auto" builds its
// URL from: the port named autoInternalURLPortName, else the Service's first
// port, else 0 (no ports at all).
func autoInternalURLPort(svc corev1.Service) int32 {
	if len(svc.Spec.Ports) == 0 {
		return 0
	}
	for _, p := range svc.Spec.Ports {
		if p.Name == autoInternalURLPortName {
			return p.Port
		}
	}
	return svc.Spec.Ports[0].Port
}

// podStatus computes a three-valued pod monitor status from selector: pods
// matching it are listed in namespace through reader (the namespace-scoped
// cached Reader for the ServiceCard's own namespace, RBAC granted
// unconditionally by internal/controller/dashboard_rbac.go's
// dashboardPodsRule; the cluster-scoped, uncached KubeReader for an allowed
// foreign namespace, RBAC granted by reconcileMonitorRBAC — see
// probePodMonitor). All matched pods Ready renders "Up"; some (but not all)
// renders "Partial" (homepage parity, see docs/design/combined-monitor.md);
// none Ready, or no pods matched, renders "Down". The status's ready text
// slot shows "<ready>/<total> ready" ("0/0 ready" when nothing matched).
func (p *Poller) podStatus(ctx context.Context, reader client.Reader, namespace string, selector *metav1.LabelSelector, entryName string) (status, readyText string) {
	sel, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		pollerLog.Error(err, "invalid pod monitor selector", "serviceEntry", entryName)
		return statusDown, ""
	}

	var pods corev1.PodList
	if err := reader.List(ctx, &pods, client.InNamespace(namespace), client.MatchingLabelsSelector{Selector: sel}); err != nil {
		pollerLog.Error(err, "listing pods for pod monitor", "serviceEntry", entryName)
		return statusDown, ""
	}

	ready := 0
	for _, pod := range pods.Items {
		if podReady(&pod) {
			ready++
		}
	}
	switch {
	case len(pods.Items) == 0:
		status = statusDown
	case ready == len(pods.Items):
		status = "Up"
	case ready > 0:
		status = statusPartial
	default:
		status = statusDown
	}
	return status, fmt.Sprintf("%d/%d ready", ready, len(pods.Items))
}

// podReady reports whether pod's Ready condition is True — stricter than
// phase=Running, which a pod failing its readiness probe still reports.
func podReady(pod *corev1.Pod) bool {
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// pollWidget builds and stores the Card for one of an entry's widgets, with
// the entry's already-probed monitor result attached. A nil widget means the
// entry has a monitor but no widget: the card shows only title/icon/monitor.
// When widget sets its own PollIntervalSeconds and this cycle isn't due yet,
// pollWidget doesn't re-run the widget poll — the entry's monitor is still
// probed every cycle regardless of the widget's own interval, so the fresh
// monitor result is merged into the previously stored card (see
// mergeMonitorIntoStoredCard) instead of being discarded, keeping the
// card's Up/Down status current even between widget polls.
func (p *Poller) pollWidget(ctx context.Context, key string, namespace string, se pagev1alpha1.ServiceEntry, widget *pagev1alpha1.ServiceWidget, m monitorProbeResult, defaultHideErrors bool, widgetDefaults map[string]pagev1alpha1.WidgetDefaultsEntry) {
	if widget != nil && !p.duePoll(key, widget.PollIntervalSeconds) {
		p.mergeMonitorIntoStoredCard(key, se, m, defaultHideErrors)
		return
	}

	hideErrors := defaultHideErrors
	if se.ErrorDisplay != nil {
		hideErrors = !*se.ErrorDisplay
	}
	card := Card{
		Key:          key,
		Group:        se.Group,
		ServiceName:  se.Name,
		Order:        se.Order,
		IconURL:      IconURL(se.Icon),
		ShowStats:    se.ShowStats == nil || *se.ShowStats,
		HideErrors:   hideErrors,
		Status:       m.status,
		StatusStyle:  m.statusStyle,
		Latency:      m.latency,
		PodStatus:    m.podStatus,
		PodReadyText: m.podReadyText,
		UpdatedAt:    time.Now(),
	}
	if m.err != "" && !hideErrors {
		card.Err = m.err
	}
	if se.Description != nil {
		card.Description = *se.Description
	}
	if se.Href != nil {
		card.Href = *se.Href
	}
	if se.Target != nil {
		card.Target = *se.Target
	}

	if widget == nil {
		p.Store.Set(card)
		return
	}
	card.WidgetType = widget.Type

	impl, ok := Lookup(widget.Type)
	if !ok {
		if !card.HideErrors {
			card.Err = fmt.Sprintf("unsupported widget type %q", widget.Type)
		}
		p.Store.Set(card)
		return
	}

	cfg := WidgetConfig{Secrets: map[string]string{}}
	// An explicit widget url wins; otherwise the widget inherits the entry's
	// already-resolved base URL (m.baseURL, computed once per entry by
	// monitor's call to resolveBaseURL — internalUrl, with the
	// InternalURLAuto sentinel resolved to an actual URL, else href), so the
	// common one-URL entry never spells the same URL twice.
	if widget.URL != nil {
		cfg.URL = *widget.URL
	} else {
		cfg.URL = m.baseURL
	}
	if widget.Config != nil {
		cfg.Config = widget.Config.Raw
	}

	// In Preview mode, a widget with no explicit url whose entry base URL
	// resolved to "" (its only URL was an InternalURL that Preview mode
	// ignores, and it has no Href to fall back to either) has nothing
	// reachable to poll — fall back to sample data for just that widget
	// instead of a doomed real request. A widget with an explicit url, or
	// whose entry has a usable Href, still polls for real.
	if p.SampleData || (p.Preview && widget.URL == nil && cfg.URL == "") {
		p.pollWidgetSample(card, impl, cfg, widget)
		return
	}

	caCert, err := p.resolveWidgetSecrets(ctx, namespace, widget.Type, widget.Secrets, widget.SecretRef, widget.CACert, widgetDefaults, cfg.Secrets)
	if err != nil {
		if !card.HideErrors {
			card.Err = err.Error()
		}
		p.Store.Set(card)
		return
	}

	httpClient, err := p.httpClientForCACert(ctx, namespace, caCert, p.HTTPClient)
	if err != nil {
		if !card.HideErrors {
			card.Err = err.Error()
		}
		p.Store.Set(card)
		return
	}

	start := time.Now()
	fields, err := impl.Poll(ctx, httpClient, cfg)
	observePoll(widget.Type, metricErr(err, fields), time.Since(start).Seconds())
	switch {
	case err != nil && !card.HideErrors:
		card.Err = err.Error()
	case err == nil && card.ShowStats:
		card.Fields = applyHighlights(filterFields(fields, widget.Fields), widget.Highlight)
	}
	p.Store.Set(card)
}

// mergeMonitorIntoStoredCard updates the fresh-this-cycle monitor fields
// (Status, StatusStyle, Latency, PodStatus, PodReadyText, plus a
// monitor-sourced Err when hideErrors allows one) on the card already in
// Store for key, leaving everything the widget poll produced (Fields,
// WidgetType, and any widget-sourced Err) untouched. Used when a widget's
// own PollIntervalSeconds override skips this cycle's poll: without this,
// the card would show the widget's last fields next to a stale monitor
// status for up to the override interval. If no card is stored yet (the
// widget's very first cycle), a new card is stored with the monitor result
// and no fields — the next due poll fills those in.
func (p *Poller) mergeMonitorIntoStoredCard(key string, se pagev1alpha1.ServiceEntry, m monitorProbeResult, defaultHideErrors bool) {
	hideErrors := defaultHideErrors
	if se.ErrorDisplay != nil {
		hideErrors = !*se.ErrorDisplay
	}

	card, ok := p.Store.Get(key)
	if !ok {
		card = Card{
			Key:         key,
			Group:       se.Group,
			ServiceName: se.Name,
			Order:       se.Order,
			IconURL:     IconURL(se.Icon),
			ShowStats:   se.ShowStats == nil || *se.ShowStats,
		}
		if se.Description != nil {
			card.Description = *se.Description
		}
		if se.Href != nil {
			card.Href = *se.Href
		}
		if se.Target != nil {
			card.Target = *se.Target
		}
	}

	card.HideErrors = hideErrors
	card.Status = m.status
	card.StatusStyle = m.statusStyle
	card.Latency = m.latency
	card.PodStatus = m.podStatus
	card.PodReadyText = m.podReadyText
	card.UpdatedAt = time.Now()
	if m.err != "" && !hideErrors {
		card.Err = m.err
	}
	p.Store.Set(card)
}

// pollWidgetSample fills card from impl's Sampler.Sample instead of polling
// the real upstream: no secret resolution, no CA-cert/HTTP client, and no
// poll metrics recorded (see SampleData's doc comment). Every registered
// Widget is required to implement Sampler (enforced by
// TestEveryRegisteredWidgetHasASample in widget_test.go), so the "no sample"
// branch below should be unreachable outside a broken future registration.
func (p *Poller) pollWidgetSample(card Card, impl Widget, cfg WidgetConfig, widget *pagev1alpha1.ServiceWidget) {
	sampler, ok := impl.(Sampler)
	if !ok {
		if !card.HideErrors {
			card.Err = fmt.Sprintf("no sample data for widget type %q", widget.Type)
		}
		p.Store.Set(card)
		return
	}
	if card.ShowStats {
		card.Fields = applyHighlights(filterFields(sampler.Sample(cfg), widget.Fields), widget.Highlight)
	}
	p.Store.Set(card)
}

// caClientCache caches one *http.Client per CA bundle (keyed by its SHA-256),
// so a widget referencing the same caCert doesn't rebuild a TLS transport —
// and lose keep-alive connections — every poll cycle. Mirrors unifi.go's
// unifiInsecureClientCache pattern for the same reason.
var caClientCache = struct {
	mu      sync.Mutex
	clients map[string]*http.Client
}{clients: map[string]*http.Client{}}

// caCacheKey returns caClientCache's key for a CA PEM bundle: its SHA-256
// content hash, hex-encoded.
func caCacheKey(caPEM string) string {
	sum := sha256.Sum256([]byte(caPEM))
	return hex.EncodeToString(sum[:])
}

// caClient returns an *http.Client trusting caPEM in addition to the system
// trust store, built from base's Timeout, caching the result by the PEM
// bundle's content hash (see caClientCache).
func caClient(base *http.Client, caPEM string) (*http.Client, error) {
	key := caCacheKey(caPEM)

	caClientCache.mu.Lock()
	defer caClientCache.mu.Unlock()
	if c, ok := caClientCache.clients[key]; ok {
		return c, nil
	}

	transport, err := newGuardedTransportWithCA([]byte(caPEM))
	if err != nil {
		return nil, err
	}
	c := &http.Client{Timeout: base.Timeout, Transport: transport}
	caClientCache.clients[key] = c
	return c, nil
}

// httpClientForCACert returns base unchanged when caCert is nil, or an
// *http.Client trusting the resolved CA bundle otherwise (see caClient). A
// non-nil error means the CA cert couldn't be resolved or parsed and the
// caller should surface it as a card error rather than falling back to base
// silently — a widget that opted into caCert wants pinned verification, not
// a quiet downgrade to the system trust store alone.
func (p *Poller) httpClientForCACert(ctx context.Context, namespace string, caCert *pagev1alpha1.SecretValueSource, base *http.Client) (*http.Client, error) {
	if caCert == nil {
		return base, nil
	}
	caPEM, err := p.resolveSecret(ctx, namespace, *caCert)
	if err != nil {
		return nil, fmt.Errorf("resolving caCert: %w", err)
	}
	p.markCAKeyUsed(caCacheKey(caPEM))
	return caClient(base, caPEM)
}

// markCAKeyUsed records key (a caClientCache key) as resolved during the
// current poll cycle, so pruneCAClientCache doesn't evict it.
func (p *Poller) markCAKeyUsed(key string) {
	p.caKeysUsedMu.Lock()
	if p.caKeysUsed == nil {
		p.caKeysUsed = map[string]bool{}
	}
	p.caKeysUsed[key] = true
	p.caKeysUsedMu.Unlock()
}

// metricErr returns the error to record for a poll metric, treating an
// "Unreachable"/"HTTP <code>" Status field the same as a returned error: by
// convention (see e.g. grafana.go), a widget reports a transport failure or
// non-2xx upstream response via this Field rather than a Go error, so that
// the card still renders a status instead of falling back to card.Err — but
// that means a real outage would otherwise be recorded as poll metric
// "success".
func metricErr(err error, fields []Field) error {
	if err != nil {
		return err
	}
	for _, f := range fields {
		if f.Label != labelStatus {
			continue
		}
		if f.Value == statusUnreach || strings.HasPrefix(f.Value, "HTTP ") {
			return fmt.Errorf("widget reported status %q", f.Value)
		}
	}
	return nil
}

// pollInfoWidget builds and stores the header Card for one InfoWidgetEntry
// whose type is a registered widget (e.g. openmeteo). key is the composite
// store key (header/<iw.Name>/<entry index>) — it must match the key
// site.go's headerWidgets assigns the corresponding HeaderWidget.Key, since
// server.go's buildHeader correlates the two by this key rather than by
// object name (a multi-widget InfoWidget yields multiple entries sharing one
// name). Config becomes the widget's Config; Secrets are resolved in-process
// like service widgets. When entry sets its own PollIntervalSeconds and this
// cycle isn't due yet, it returns immediately, leaving the previous cycle's
// card in place (see pollWidget's doc comment for the same pattern).
func (p *Poller) pollInfoWidget(ctx context.Context, key string, iw pagev1alpha1.InfoWidget, entry pagev1alpha1.InfoWidgetEntry, widgetDefaults map[string]pagev1alpha1.WidgetDefaultsEntry) {
	if !p.duePoll(key, entry.PollIntervalSeconds) {
		return
	}

	card := Card{
		Key:         key,
		ServiceName: iw.Name,
		WidgetType:  entry.Type,
		Order:       entry.Order,
		Header:      true,
		ShowStats:   true,
		UpdatedAt:   time.Now(),
	}

	impl, _ := Lookup(entry.Type) // presence already checked by caller

	cfg := WidgetConfig{Secrets: map[string]string{}}
	if entry.Config != nil {
		cfg.Config = entry.Config.Raw
	}
	if entry.URL != nil {
		cfg.URL = *entry.URL
	}

	if p.SampleData {
		p.pollInfoWidgetSample(card, impl, cfg)
		return
	}

	caCert, err := p.resolveWidgetSecrets(ctx, iw.Namespace, entry.Type, entry.Secrets, entry.SecretRef, entry.CACert, widgetDefaults, cfg.Secrets)
	if err != nil {
		card.Err = err.Error()
		p.Store.Set(card)
		return
	}

	var fields []Field
	start := time.Now()
	if cw, ok := impl.(ClusterWidget); ok {
		// Cluster-backed widget (e.g. kubemetrics): read the Kubernetes API
		// via KubeReader instead of polling an HTTP upstream.
		fields, err = cw.PollCluster(ctx, p.KubeReader, cfg)
	} else {
		var httpClient *http.Client
		httpClient, err = p.httpClientForCACert(ctx, iw.Namespace, caCert, p.HTTPClient)
		if err == nil {
			fields, err = impl.Poll(ctx, httpClient, cfg)
		}
	}
	observePoll(entry.Type, metricErr(err, fields), time.Since(start).Seconds())
	if err != nil {
		card.Err = err.Error()
	} else {
		card.Fields = fields
	}
	p.Store.Set(card)
}

// pollInfoWidgetSample is pollInfoWidget's SampleData counterpart: it never
// calls PollCluster or Poll, so it needs neither KubeReader nor an HTTP
// client, and it records no poll metrics. impl is guaranteed non-nil by
// pollOnce's caller-side Lookup check.
func (p *Poller) pollInfoWidgetSample(card Card, impl Widget, cfg WidgetConfig) {
	sampler, ok := impl.(Sampler)
	if !ok {
		card.Err = fmt.Sprintf("no sample data for widget type %q", card.WidgetType)
		p.Store.Set(card)
		return
	}
	card.Fields = sampler.Sample(cfg)
	p.Store.Set(card)
}

// resolveSecret returns src's literal value, or the plaintext content of
// the Secret key it references — unlike the homepage-wrapper's
// secretProjection (internal/controller/secret_resolver.go), this never
// produces a file-projection placeholder: the dashboard backend uses the
// value directly and it never leaves this process.
func (p *Poller) resolveSecret(ctx context.Context, namespace string, src pagev1alpha1.SecretValueSource) (string, error) {
	if src.SecretKeyRef == nil {
		if src.Value != nil {
			return *src.Value, nil
		}
		return "", fmt.Errorf("neither value nor secretKeyRef set")
	}

	ref := src.SecretKeyRef
	secret := &corev1.Secret{}
	if err := p.SecretReader.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: namespace}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return "", fmt.Errorf("secret %q does not exist in namespace %q", ref.Name, namespace)
		}
		return "", fmt.Errorf("getting Secret %q: %w", ref.Name, err)
	}

	data, ok := secret.Data[ref.Key]
	if !ok {
		return "", fmt.Errorf("key %q does not exist in Secret %q", ref.Key, ref.Name)
	}
	return string(data), nil
}

// resolveSecretRefFields returns the plaintext content of every key in the
// Secret named name, keyed by key name — the expansion a widget's SecretRef
// performs (see ServiceWidget.SecretRef's doc comment): every key becomes a
// resolved secret field, as if listed under Secrets with secretKeyRef.key
// equal to that key name. Read through the same RBAC-scoped SecretReader as
// resolveSecret, so secretPolicy: Labeled enforcement (via
// internal/controller/dashboard_rbac.go's referencedSecretNames) applies
// identically: the dashboard pod can only Get a Secret its Role permits.
func (p *Poller) resolveSecretRefFields(ctx context.Context, namespace, name string) (map[string]string, error) {
	secret := &corev1.Secret{}
	if err := p.SecretReader.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("secret %q does not exist in namespace %q", name, namespace)
		}
		return nil, fmt.Errorf("getting Secret %q: %w", name, err)
	}

	fields := make(map[string]string, len(secret.Data))
	for key, data := range secret.Data {
		fields[key] = string(data)
	}
	return fields, nil
}

// resolveWidgetSecrets resolves a widget's secret-bearing fields into
// cfgSecrets (a ServiceWidget/InfoWidgetEntry's own WidgetConfig.Secrets, or
// pollWidgetSample-adjacent caller's equivalent) in increasing precedence,
// each stage free to overwrite the last: widgetDefaults (dashboard-wide) <
// secretRef (widget-level, whole-Secret shorthand) < secrets (widget-level,
// explicit per key) — see ServiceWidget.SecretRef's doc comment. Returns the
// caCert to use (widget's own, else widgetDefaults', see mergeWidgetSecrets)
// and the first resolution error encountered, if any — shared by pollWidget
// and pollInfoWidget purely to keep each of their own cyclomatic complexity
// down; see reconcileDeployment's doc comment for the project's general
// stance on not collapsing call sites beyond this.
func (p *Poller) resolveWidgetSecrets(
	ctx context.Context,
	namespace string,
	widgetType string,
	secrets map[string]pagev1alpha1.SecretValueSource,
	secretRef *string,
	caCertSrc *pagev1alpha1.SecretValueSource,
	widgetDefaults map[string]pagev1alpha1.WidgetDefaultsEntry,
	cfgSecrets map[string]string,
) (*pagev1alpha1.SecretValueSource, error) {
	defaults, caCert := mergeWidgetSecrets(widgetType, nil, caCertSrc, widgetDefaults)
	for field, src := range defaults {
		value, err := p.resolveSecret(ctx, namespace, src)
		if err != nil {
			return nil, fmt.Errorf("resolving secret field %q: %w", field, err)
		}
		cfgSecrets[field] = value
	}

	if secretRef != nil {
		refFields, err := p.resolveSecretRefFields(ctx, namespace, *secretRef)
		if err != nil {
			return nil, fmt.Errorf("resolving secretRef %q: %w", *secretRef, err)
		}
		maps.Copy(cfgSecrets, refFields)
	}

	for field, src := range secrets {
		value, err := p.resolveSecret(ctx, namespace, src)
		if err != nil {
			return nil, fmt.Errorf("resolving secret field %q: %w", field, err)
		}
		cfgSecrets[field] = value
	}

	return caCert, nil
}

// siteDefaults returns the site-wide StatusStyle/HideErrors defaults from
// the Dashboard's spec.style (falling back to statusStyleDot/false when
// unset), the same resolution LoadSite uses for the HTTP-serving side.
func (p *Poller) siteDefaults(ctx context.Context) (statusStyle string, hideErrors bool) {
	statusStyle = statusStyleDot

	spec, err := boundStyleSpec(ctx, p.Reader, p.Namespace, p.DashboardName)
	if err != nil {
		pollerLog.Error(err, "loading Dashboard style for site-wide defaults")
		return statusStyle, hideErrors
	}
	if spec == nil {
		return statusStyle, hideErrors
	}
	if spec.StatusStyle != nil {
		statusStyle = *spec.StatusStyle
	}
	if spec.ErrorDisplay != nil {
		hideErrors = !*spec.ErrorDisplay
	}
	return statusStyle, hideErrors
}

// dashboardSpecForPoll Gets the Dashboard once and returns its Spec, or the
// zero value and ok=false when the Get fails — a transient error here
// should not fail the whole poll cycle, only make every field this backs
// (discovery config, widget defaults, allowed monitor namespaces) fall back
// to "unset" for this cycle — e.g. a foreign-namespace pod monitor simply
// renders Down with a card error for the cycle (MonitorNamespaces falls back
// to nil) rather than failing every other card too. pollOnce calls this once
// per cycle and derives all three from the single result;
// discoverySpec/widgetDefaults below wrap it for callers (currently only
// tests) that want just one field.
func (p *Poller) dashboardSpecForPoll(ctx context.Context) (pagev1alpha1.DashboardSpec, bool) {
	var instance pagev1alpha1.Dashboard
	if err := p.Reader.Get(ctx, types.NamespacedName{Name: p.DashboardName, Namespace: p.Namespace}, &instance); err != nil {
		pollerLog.Error(err, "getting Dashboard for poll cycle config")
		return pagev1alpha1.DashboardSpec{}, false
	}
	return instance.Spec, true
}

// discoverySpecFromDashboard extracts the DiscoverySpec from an
// already-fetched DashboardSpec (spec, ok as returned by
// dashboardSpecForPoll), returning (zero value, false) when the Get failed
// or Ingress annotation discovery isn't enabled.
func discoverySpecFromDashboard(spec pagev1alpha1.DashboardSpec, ok bool) (pagev1alpha1.DiscoverySpec, bool) {
	if !ok || spec.Discovery == nil || !spec.Discovery.Enabled {
		return pagev1alpha1.DiscoverySpec{}, false
	}
	return *spec.Discovery, true
}

// discoverySpec returns the Dashboard's DiscoverySpec when Ingress annotation
// discovery is enabled, or (zero value, false) otherwise (including when the
// Dashboard can't be read — a transient error here should not make every
// discovered card vanish from the log at more than Error level, but it must
// not panic the poll cycle either).
func (p *Poller) discoverySpec(ctx context.Context) (pagev1alpha1.DiscoverySpec, bool) {
	spec, ok := p.dashboardSpecForPoll(ctx)
	return discoverySpecFromDashboard(spec, ok)
}

// widgetDefaults returns the Dashboard's Spec.WidgetDefaults (the per-widget-
// type shared secret defaults — see DashboardSpec.WidgetDefaults' doc
// comment), or nil when unset or the Dashboard can't be read — mirroring
// discoverySpec's tolerance of a transient Get failure: it should not fail
// every widget's poll for the cycle, only fall back to "no defaults" (same
// behavior as today, before this field existed).
func (p *Poller) widgetDefaults(ctx context.Context) map[string]pagev1alpha1.WidgetDefaultsEntry {
	spec, ok := p.dashboardSpecForPoll(ctx)
	if !ok {
		return nil
	}
	return spec.WidgetDefaults
}

// pollDiscoveredService builds and stores the Card for one Ingress-derived
// discoveredService: title/icon/description/href only, plus an optional
// monitor probe of its href — never a polled widget, since annotations are world-readable to
// anyone who can read the Ingress and so can't safely carry secrets (see
// DiscoverySpec's doc comment).
func (p *Poller) pollDiscoveredService(ctx context.Context, svc discoveredService, record func(label, source string)) {
	card := Card{
		Key:         svc.Key,
		Group:       svc.Group,
		ServiceName: svc.Name,
		IconURL:     svc.IconURL,
		Description: svc.Description,
		Href:        svc.Href,
		UpdatedAt:   time.Now(),
	}
	if svc.Monitor && svc.Href != "" {
		card.Status, card.Latency = p.probeURL(ctx, svc.Href)
		card.StatusStyle = statusStyleDot
		// Sample-mode monitor results are fabricated, not observed, so they
		// don't get recorded into the monitorUp Prometheus gauge either —
		// see SampleData's doc comment and monitor()'s identical gate.
		if !p.SampleData {
			label := "discovery/" + svc.Key
			up := 0.0
			if card.Status == "Up" {
				up = 1
			}
			monitorUp.WithLabelValues(label, monitorSourceHTTP).Set(up)
			record(label, monitorSourceHTTP)
		}
	}
	p.Store.Set(card)
}
