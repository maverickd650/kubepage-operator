package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

// Poller periodically lists the ServiceEntries bound to one Instance,
// resolves each widget's secrets and config, polls every widget whose type
// is registered, and writes the results into Store. Polling runs on its own
// interval rather than per browser request, so a slow or unreachable
// upstream never blocks a page load.
type Poller struct {
	// Reader lists CRDs; expected to be a cache-backed (informer) client
	// scoped to Namespace, per D11's "reads its Instance's bound CRDs via a
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

	Namespace    string
	InstanceName string
	Interval     time.Duration
	HTTPClient   *http.Client
	Store        *Store
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
	keep := map[string]bool{}

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

	var entries pagev1alpha1.ServiceEntryList
	if err := p.Reader.List(ctx, &entries, client.InNamespace(p.Namespace)); err != nil {
		pollerLog.Error(err, "listing ServiceEntries")
		return
	}
	for _, entry := range entries.Items {
		if entry.Spec.InstanceRef.Name != p.InstanceName {
			continue
		}

		// Probe the entry's monitor (ping/siteMonitor) at most once,
		// sequentially — it's a single cheap HTTP HEAD/GET — then attach the
		// result to every card built for the entry. The potentially slow
		// part, each widget's upstream API poll, runs concurrently below.
		status, statusStyle, latency := p.monitor(ctx, entry)

		if len(entry.Spec.Widgets) == 0 {
			// A service with a monitor but no widget still gets one card so
			// its up/down status is visible.
			if status != "" {
				key := fmt.Sprintf("%s/%s/monitor", entry.Namespace, entry.Name)
				keep[key] = true
				run(func() { p.pollWidget(ctx, key, entry, nil, status, statusStyle, latency) })
			}
			continue
		}

		for i := range entry.Spec.Widgets {
			key := fmt.Sprintf("%s/%s/%d", entry.Namespace, entry.Name, i)
			keep[key] = true
			widget := &entry.Spec.Widgets[i]
			run(func() { p.pollWidget(ctx, key, entry, widget, status, statusStyle, latency) })
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
			if iw.Spec.InstanceRef.Name != p.InstanceName {
				continue
			}
			if _, ok := Lookup(iw.Spec.Type); !ok {
				continue
			}
			key := fmt.Sprintf("header/%s", iw.Name)
			keep[key] = true
			run(func() { p.pollInfoWidget(ctx, key, iw) })
		}
	}

	wg.Wait()
	p.Store.Prune(keep)
}

// monitor probes the entry's Ping/SiteMonitor (SiteMonitor preferred when both
// set) over HTTP, returning the resolved status/style/latency, or empty
// strings when neither is configured.
func (p *Poller) monitor(ctx context.Context, entry pagev1alpha1.ServiceEntry) (status, statusStyle, latency string) {
	url := ""
	switch {
	case entry.Spec.SiteMonitor != nil && *entry.Spec.SiteMonitor != "":
		url = *entry.Spec.SiteMonitor
	case entry.Spec.Ping != nil && *entry.Spec.Ping != "":
		url = *entry.Spec.Ping
	default:
		return "", "", ""
	}

	style := "dot"
	if entry.Spec.StatusStyle != nil {
		style = *entry.Spec.StatusStyle
	}
	status, latency = monitorResult(ctx, p.HTTPClient, url)
	up := 0.0
	if status == "Up" {
		up = 1
	}
	monitorUp.WithLabelValues(entry.Namespace + "/" + entry.Name).Set(up)
	return status, style, latency
}

// pollWidget builds and stores the Card for one of an entry's widgets, with
// the entry's already-probed monitor status attached. A nil widget means the
// entry has a monitor but no widget: the card shows only title/icon/monitor.
func (p *Poller) pollWidget(ctx context.Context, key string, entry pagev1alpha1.ServiceEntry, widget *pagev1alpha1.ServiceWidget, status, statusStyle, latency string) {
	card := Card{
		Key:         key,
		Group:       entry.Spec.Group,
		ServiceName: entry.Spec.Name,
		Order:       entry.Spec.Order,
		IconURL:     IconURL(entry.Spec.Icon),
		ShowStats:   entry.Spec.ShowStats == nil || *entry.Spec.ShowStats,
		HideErrors:  entry.Spec.HideErrors != nil && *entry.Spec.HideErrors,
		Status:      status,
		StatusStyle: statusStyle,
		Latency:     latency,
		UpdatedAt:   time.Now(),
	}
	if entry.Spec.Description != nil {
		card.Description = *entry.Spec.Description
	}
	if entry.Spec.Href != nil {
		card.Href = *entry.Spec.Href
	}
	if entry.Spec.Target != nil {
		card.Target = *entry.Spec.Target
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
	for field, src := range widget.Secrets {
		value, err := p.resolveSecret(ctx, entry.Namespace, src)
		if err != nil {
			if !card.HideErrors {
				card.Err = fmt.Sprintf("resolving secret field %q: %v", field, err)
			}
			p.Store.Set(card)
			return
		}
		cfg.Secrets[field] = value
	}

	start := time.Now()
	fields, err := impl.Poll(ctx, p.HTTPClient, cfg)
	observePoll(widget.Type, metricErr(err, fields), time.Since(start).Seconds())
	switch {
	case err != nil && !card.HideErrors:
		card.Err = err.Error()
	case err == nil && card.ShowStats:
		card.Fields = fields
	}
	p.Store.Set(card)
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

// pollInfoWidget builds and stores the header Card for one InfoWidget whose
// type is a registered widget (e.g. openmeteo). Options become the widget's
// Config; Secrets are resolved in-process like service widgets.
func (p *Poller) pollInfoWidget(ctx context.Context, key string, iw pagev1alpha1.InfoWidget) {
	card := Card{
		Key:         key,
		ServiceName: iw.Name,
		WidgetType:  iw.Spec.Type,
		Order:       iw.Spec.Order,
		Header:      true,
		ShowStats:   true,
		UpdatedAt:   time.Now(),
	}

	impl, _ := Lookup(iw.Spec.Type) // presence already checked by caller

	cfg := WidgetConfig{Secrets: map[string]string{}}
	if iw.Spec.Options != nil {
		cfg.Config = iw.Spec.Options.Raw
		// A header widget has no dedicated URL field; let it set the widget's
		// base URL via an Options "url" key (widgets ignore the key in their
		// own config decode).
		var opts struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(iw.Spec.Options.Raw, &opts); err == nil {
			cfg.URL = opts.URL
		}
	}
	for field, src := range iw.Spec.Secrets {
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
		fields, err = impl.Poll(ctx, p.HTTPClient, cfg)
	}
	observePoll(iw.Spec.Type, metricErr(err, fields), time.Since(start).Seconds())
	if err != nil {
		card.Err = err.Error()
	} else {
		card.Fields = fields
	}
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
