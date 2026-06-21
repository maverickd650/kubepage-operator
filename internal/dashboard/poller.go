package dashboard

import (
	"context"
	"fmt"
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

var pollerLog = ctrl.Log.WithName("dashboard-poller")

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
	var entries pagev1alpha1.ServiceEntryList
	if err := p.Reader.List(ctx, &entries, client.InNamespace(p.Namespace)); err != nil {
		pollerLog.Error(err, "listing ServiceEntries")
		return
	}

	keep := map[string]bool{}
	for _, entry := range entries.Items {
		if entry.Spec.InstanceRef.Name != p.InstanceName {
			continue
		}

		for i, widget := range entry.Spec.Widgets {
			key := fmt.Sprintf("%s/%s/%d", entry.Namespace, entry.Name, i)
			keep[key] = true
			p.pollWidget(ctx, key, entry, widget)
		}
	}

	p.Store.Prune(keep)
}

func (p *Poller) pollWidget(ctx context.Context, key string, entry pagev1alpha1.ServiceEntry, widget pagev1alpha1.ServiceWidget) {
	card := Card{
		Key:         key,
		Group:       entry.Spec.Group,
		ServiceName: entry.Spec.Name,
		WidgetType:  widget.Type,
		Order:       entry.Spec.Order,
		IconURL:     IconURL(entry.Spec.Icon),
		UpdatedAt:   time.Now(),
	}
	if entry.Spec.Description != nil {
		card.Description = *entry.Spec.Description
	}

	impl, ok := Lookup(widget.Type)
	if !ok {
		card.Err = fmt.Sprintf("unsupported widget type %q", widget.Type)
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
			card.Err = fmt.Sprintf("resolving secret field %q: %v", field, err)
			p.Store.Set(card)
			return
		}
		cfg.Secrets[field] = value
	}

	fields, err := impl.Poll(ctx, p.HTTPClient, cfg)
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
