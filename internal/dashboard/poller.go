package dashboard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
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
// cards.templ as a colored dot rather than a text pill.
const statusStyleDot = "dot"

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

	// monitorLabels is the set of monitorUp labels reported on the previous
	// poll cycle — "namespace/name" for the single-card form, or
	// "namespace/name/entryName" for a multi-card-form services entry (see
	// monitor's doc comment). pollOnce diffs the
	// current cycle's set against this to delete a label series for an
	// entry that's since been deleted or had its monitor removed —
	// monitorUp has no other pruning path, unlike Store's per-cycle Prune.
	// Only ever read/written from pollOnce, which Run never calls
	// concurrently with itself, so this needs no lock of its own.
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
	var keepMu sync.Mutex
	keep := map[string]bool{}
	markKeep := func(key string) {
		keepMu.Lock()
		keep[key] = true
		keepMu.Unlock()
	}

	var monitorLabelsMu sync.Mutex
	monitorLabels := map[string]bool{}
	recordMonitorLabel := func(label string) {
		monitorLabelsMu.Lock()
		monitorLabels[label] = true
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
	widgetDefaults := p.widgetDefaults(ctx)

	var entries pagev1alpha1.ServiceCardList
	if err := p.Reader.List(ctx, &entries, client.InNamespace(p.Namespace)); err != nil {
		pollerLog.Error(err, "listing ServiceCards")
		return
	}
	for _, entry := range entries.Items {
		if entry.Spec.DashboardRef.Name != p.DashboardName {
			continue
		}

		// multiForm is true for a ServiceCard using the multi-card
		// (services) form; used only to pick monitorUp's label suffix (see
		// monitor's doc comment) — Entries() itself already normalizes both
		// forms into the same []ServiceEntry shape for everything else.
		multiForm := len(entry.Spec.Services) > 0

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
				status, statusStyle, latency := p.monitor(ctx, namespace, crName, se, multiForm, defaultStatusStyle, recordMonitorLabel)

				if len(se.Widgets) == 0 {
					// A service with a monitor but no widget still gets one
					// card so its up/down status is visible.
					if status == "" {
						return
					}
					key := fmt.Sprintf("%s/%s/%d/monitor", namespace, crName, entryIdx)
					markKeep(key)
					p.pollWidget(ctx, key, namespace, se, nil, status, statusStyle, latency, defaultHideErrors, widgetDefaults)
					return
				}

				for i := range se.Widgets {
					key := fmt.Sprintf("%s/%s/%d/%d", namespace, crName, entryIdx, i)
					markKeep(key)
					widget := &se.Widgets[i]
					run(func() {
						p.pollWidget(ctx, key, namespace, se, widget, status, statusStyle, latency, defaultHideErrors, widgetDefaults)
					})
				}
			})
		}
	}

	if spec, ok := p.discoverySpec(ctx); ok {
		if spec.HasSource(pagev1alpha1.DiscoverySourceIngress) {
			services, err := discoverServices(ctx, p.Reader, p.Namespace, spec)
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
				routes, err := discoverHTTPRoutes(ctx, p.Reader, p.Namespace, spec)
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
			if iw.Spec.DashboardRef.Name != p.DashboardName {
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
}

// duePoll reports whether the widget at key should be polled this cycle,
// given its optional PollIntervalSeconds override: nil or <=0 means every
// cycle (the common case, tracked nowhere). A set override is floor-clamped
// to the poller's own Interval, since a shorter override would have no
// effect — pollOnce only runs once per Interval regardless. When it returns
// true, it also records now as key's last-polled time.
func (p *Poller) duePoll(key string, overrideSeconds *int32) bool {
	if overrideSeconds == nil || *overrideSeconds <= 0 {
		return true
	}
	interval := max(time.Duration(*overrideSeconds)*time.Second, p.Interval)

	p.widgetLastPolledMu.Lock()
	defer p.widgetLastPolledMu.Unlock()
	if last, ok := p.widgetLastPolled[key]; ok && time.Since(last) < interval {
		return false
	}
	if p.widgetLastPolled == nil {
		p.widgetLastPolled = map[string]time.Time{}
	}
	p.widgetLastPolled[key] = time.Now()
	return true
}

// pruneWidgetLastPolled deletes any widgetLastPolled entry not in this
// cycle's keep set, mirroring Store.Prune, so a deleted (or edited-away-
// from-an-override) widget's bookkeeping doesn't accumulate forever.
func (p *Poller) pruneWidgetLastPolled(keep map[string]bool) {
	p.widgetLastPolledMu.Lock()
	defer p.widgetLastPolledMu.Unlock()
	for key := range p.widgetLastPolled {
		if !keep[key] {
			delete(p.widgetLastPolled, key)
		}
	}
}

// pruneMonitorMetrics deletes any monitorUp label series from the previous
// cycle that current (this cycle's labels) no longer reports, so a deleted
// ServiceCard — or one that's had its Ping/SiteMonitor/PodSelector removed —
// doesn't leave a stale gauge value exported forever.
func (p *Poller) pruneMonitorMetrics(current map[string]bool) {
	for label := range p.monitorLabels {
		if !current[label] {
			monitorUp.DeleteLabelValues(label)
		}
	}
	p.monitorLabels = current
}

// monitor probes se's monitor source — Ping, SiteMonitor, or PodSelector,
// mutually exclusive by ServiceEntry's own CEL validation — returning the
// resolved status/style/latency, or empty strings when none is configured.
// record is called with the monitorUp label this probe reported under, so
// the caller can track which labels are still live this cycle (see
// pruneMonitorMetrics). The label is "namespace/crName" for a ServiceCard
// using the inline single-card form (unchanged from earlier versions of this
// API), or "namespace/crName/entryName" for a services entry of the
// multi-card form — multiForm distinguishes the two so two entries defined
// in the same multi-card ServiceCard don't collide on one label series.
func (p *Poller) monitor(ctx context.Context, namespace, crName string, se pagev1alpha1.ServiceEntry, multiForm bool, defaultStatusStyle string, record func(label string)) (status, statusStyle, latency string) {
	switch {
	case se.PodSelector != nil:
		status, latency = p.probePodSelector(ctx, namespace, se)
	case se.SiteMonitor != nil && *se.SiteMonitor != "":
		status, latency = p.probeURL(ctx, *se.SiteMonitor)
	case se.Ping != nil && *se.Ping != "":
		status, latency = p.probeURL(ctx, *se.Ping)
	default:
		return "", "", ""
	}

	style := defaultStatusStyle
	if se.StatusStyle != nil {
		style = *se.StatusStyle
	}
	// Sample-mode monitor results are fabricated, not observed, so they
	// don't get recorded into the monitorUp Prometheus gauge either — see
	// SampleData's doc comment.
	if p.SampleData {
		return status, style, latency
	}
	up := 0.0
	if status == "Up" {
		up = 1
	}
	label := namespace + "/" + crName
	if multiForm {
		label += "/" + se.Name
	}
	monitorUp.WithLabelValues(label).Set(up)
	record(label)
	return status, style, latency
}

// sampleMonitorLatency and sampleMonitorReadyText are the canned monitor
// results SampleData mode reports for a configured ping/siteMonitor/
// podSelector, respectively — see probeURL/probePodSelector.
const (
	sampleMonitorLatency   = "12 ms"
	sampleMonitorReadyText = "2/2 ready"
)

// probePodSelector returns a fabricated "Up" status in SampleData mode
// instead of actually listing pods, so preview mode never needs pod RBAC to
// render a populated status.
func (p *Poller) probePodSelector(ctx context.Context, namespace string, se pagev1alpha1.ServiceEntry) (status, text string) {
	if p.SampleData {
		return "Up", sampleMonitorReadyText
	}
	return p.podStatus(ctx, namespace, se)
}

// probeURL returns a fabricated "Up" status in SampleData mode instead of
// actually probing url.
func (p *Poller) probeURL(ctx context.Context, url string) (status, latency string) {
	if p.SampleData {
		return "Up", sampleMonitorLatency
	}
	return monitorResult(ctx, p.HTTPClient, url)
}

// podStatus computes an up/down status from se's PodSelector: pods are
// listed in namespace through the same namespace-scoped, cache-backed Reader
// as ServiceCard itself (RBAC granted by
// internal/controller/instance_rbac.go's dashboardPodsRule — namespaced and
// owner-referenced, unlike the cluster-scoped KubeReader used by
// ClusterWidget types). Any matching pod with a Ready condition of True
// renders "Up"; the monitor's latency slot is repurposed to show
// "<ready>/<total> ready" in place of a network latency figure.
func (p *Poller) podStatus(ctx context.Context, namespace string, se pagev1alpha1.ServiceEntry) (status, readyText string) {
	selector, err := metav1.LabelSelectorAsSelector(se.PodSelector)
	if err != nil {
		pollerLog.Error(err, "invalid PodSelector", "serviceEntry", se.Name)
		return statusDown, ""
	}

	var pods corev1.PodList
	if err := p.Reader.List(ctx, &pods, client.InNamespace(namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
		pollerLog.Error(err, "listing pods for PodSelector", "serviceEntry", se.Name)
		return statusDown, ""
	}

	ready := 0
	for _, pod := range pods.Items {
		if podReady(&pod) {
			ready++
		}
	}
	status = statusDown
	if ready > 0 {
		status = "Up"
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
// the entry's already-probed monitor status attached. A nil widget means the
// entry has a monitor but no widget: the card shows only title/icon/monitor.
// When widget sets its own PollIntervalSeconds and this cycle isn't due yet,
// pollWidget returns immediately without touching Store, leaving the
// previous cycle's card (monitor status included) in place — key is already
// in this cycle's keep set from the caller, so it survives Store.Prune.
func (p *Poller) pollWidget(ctx context.Context, key string, namespace string, se pagev1alpha1.ServiceEntry, widget *pagev1alpha1.ServiceWidget, status, statusStyle, latency string, defaultHideErrors bool, widgetDefaults map[string]pagev1alpha1.WidgetDefaultsEntry) {
	if widget != nil && !p.duePoll(key, widget.PollIntervalSeconds) {
		return
	}

	hideErrors := defaultHideErrors
	if se.ErrorDisplay != nil {
		hideErrors = *se.ErrorDisplay == pagev1alpha1.ErrorDisplayHidden
	}
	card := Card{
		Key:         key,
		Group:       se.Group,
		ServiceName: se.Name,
		Order:       se.Order,
		IconURL:     IconURL(se.Icon),
		ShowStats:   se.ShowStats == nil || *se.ShowStats != pagev1alpha1.StatsHide,
		HideErrors:  hideErrors,
		Status:      status,
		StatusStyle: statusStyle,
		Latency:     latency,
		UpdatedAt:   time.Now(),
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
	if widget.URL != nil {
		cfg.URL = *widget.URL
	}
	if widget.Config != nil {
		cfg.Config = widget.Config.Raw
	}

	if p.SampleData {
		p.pollWidgetSample(card, impl, cfg, widget)
		return
	}

	secrets, caCert := mergeWidgetSecrets(widget.Type, widget.Secrets, widget.CACert, widgetDefaults)
	for field, src := range secrets {
		value, err := p.resolveSecret(ctx, namespace, src)
		if err != nil {
			if !card.HideErrors {
				card.Err = fmt.Sprintf("resolving secret field %q: %v", field, err)
			}
			p.Store.Set(card)
			return
		}
		cfg.Secrets[field] = value
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

// caClient returns an *http.Client trusting caPEM in addition to the system
// trust store, built from base's Timeout, caching the result by the PEM
// bundle's content hash (see caClientCache).
func caClient(base *http.Client, caPEM string) (*http.Client, error) {
	sum := sha256.Sum256([]byte(caPEM))
	key := hex.EncodeToString(sum[:])

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
	return caClient(base, caPEM)
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
// name). Options become the widget's Config; Secrets are resolved in-process
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
	if entry.Options != nil {
		cfg.Config = entry.Options.Raw
		// Options' "url" key remains supported for backwards compatibility
		// (widgets ignore the key in their own config decode); entry.URL,
		// when set, takes precedence over it.
		var opts struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(entry.Options.Raw, &opts); err == nil {
			cfg.URL = opts.URL
		}
	}
	if entry.URL != nil {
		cfg.URL = *entry.URL
	}

	if p.SampleData {
		p.pollInfoWidgetSample(card, impl, cfg)
		return
	}

	secrets, caCert := mergeWidgetSecrets(entry.Type, entry.Secrets, entry.CACert, widgetDefaults)
	for field, src := range secrets {
		value, err := p.resolveSecret(ctx, iw.Namespace, src)
		if err != nil {
			card.Err = fmt.Sprintf("resolving secret field %q: %v", field, err)
			p.Store.Set(card)
			return
		}
		cfg.Secrets[field] = value
	}

	var fields []Field
	var err error
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

// siteDefaults returns the site-wide StatusStyle/HideErrors defaults from
// the Dashboard's bound DashboardStyle (falling back to statusStyleDot/false
// when none is bound), the same "which DashboardStyle wins" resolution
// LoadSite uses for the HTTP-serving side.
func (p *Poller) siteDefaults(ctx context.Context) (statusStyle string, hideErrors bool) {
	statusStyle = statusStyleDot

	spec, err := boundDashboardStyleSpec(ctx, p.Reader, p.Namespace, p.DashboardName)
	if err != nil {
		pollerLog.Error(err, "loading DashboardStyle for site-wide defaults")
		return statusStyle, hideErrors
	}
	if spec == nil {
		return statusStyle, hideErrors
	}
	if spec.StatusStyle != nil {
		statusStyle = *spec.StatusStyle
	}
	if spec.ErrorDisplay != nil {
		hideErrors = *spec.ErrorDisplay == pagev1alpha1.ErrorDisplayHidden
	}
	return statusStyle, hideErrors
}

// discoverySpec returns the Dashboard's DiscoverySpec when Ingress annotation
// discovery is enabled, or (zero value, false) otherwise (including when the
// Dashboard can't be read — a transient error here should not make every
// discovered card vanish from the log at more than Error level, but it must
// not panic the poll cycle either).
func (p *Poller) discoverySpec(ctx context.Context) (pagev1alpha1.DiscoverySpec, bool) {
	var instance pagev1alpha1.Dashboard
	if err := p.Reader.Get(ctx, types.NamespacedName{Name: p.DashboardName, Namespace: p.Namespace}, &instance); err != nil {
		pollerLog.Error(err, "getting Dashboard for discovery config")
		return pagev1alpha1.DiscoverySpec{}, false
	}
	if instance.Spec.Discovery == nil || instance.Spec.Discovery.Enabled != pagev1alpha1.Enabled {
		return pagev1alpha1.DiscoverySpec{}, false
	}
	return *instance.Spec.Discovery, true
}

// widgetDefaults returns the Dashboard's Spec.WidgetDefaults (the per-widget-
// type shared secret defaults — see DashboardSpec.WidgetDefaults' doc
// comment), or nil when unset or the Dashboard can't be read — mirroring
// discoverySpec's tolerance of a transient Get failure: it should not fail
// every widget's poll for the cycle, only fall back to "no defaults" (same
// behavior as today, before this field existed).
func (p *Poller) widgetDefaults(ctx context.Context) map[string]pagev1alpha1.WidgetDefaultsEntry {
	var instance pagev1alpha1.Dashboard
	if err := p.Reader.Get(ctx, types.NamespacedName{Name: p.DashboardName, Namespace: p.Namespace}, &instance); err != nil {
		pollerLog.Error(err, "getting Dashboard for widgetDefaults")
		return nil
	}
	return instance.Spec.WidgetDefaults
}

// pollDiscoveredService builds and stores the Card for one Ingress-derived
// discoveredService: title/icon/description/href only, plus an optional Ping
// probe — never a polled widget, since annotations are world-readable to
// anyone who can read the Ingress and so can't safely carry secrets (see
// DiscoverySpec's doc comment).
func (p *Poller) pollDiscoveredService(ctx context.Context, svc discoveredService, record func(label string)) {
	card := Card{
		Key:         svc.Key,
		Group:       svc.Group,
		ServiceName: svc.Name,
		IconURL:     svc.IconURL,
		Description: svc.Description,
		Href:        svc.Href,
		UpdatedAt:   time.Now(),
	}
	if svc.Ping && svc.Href != "" {
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
			monitorUp.WithLabelValues(label).Set(up)
			record(label)
		}
	}
	p.Store.Set(card)
}
