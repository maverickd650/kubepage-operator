package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
	"github.com/maverickd650/kubepage-operator/internal/render"
)

const (
	// secretsVolumeName/secretsMountPath mount the aggregated projected
	// Secret volume backing every widget's secretKeyRef into the homepage
	// container (file-based secret delivery per D8 — not exposed in pod env).
	secretsVolumeName = "secrets"
	secretsMountPath  = "/app/secrets"
)

// buildServiceInputs converts entries into render-ready ServiceInputs,
// resolving every widget's Secrets into a {{HOMEPAGE_FILE_*}} Config
// placeholder via projection. Resolution validates that each referenced
// Secret and key actually exist, so a typo surfaces as a clear reconcile
// error/status condition instead of a silent kubelet-level mount failure.
func (r *InstanceReconciler) buildServiceInputs(ctx context.Context, namespace string, entries []pagev1alpha1.ServiceEntry, projection *secretProjection) ([]render.ServiceInput, error) {
	inputs := make([]render.ServiceInput, 0, len(entries))
	for _, e := range entries {
		widgets := make([]render.ServiceWidgetInput, 0, len(e.Spec.Widgets))
		for wi, w := range e.Spec.Widgets {
			cfg := map[string]any{}
			if w.Config != nil && len(w.Config.Raw) > 0 {
				if err := json.Unmarshal(w.Config.Raw, &cfg); err != nil {
					return nil, fmt.Errorf("decoding config for ServiceEntry %s/%s widget %d: %w", e.Namespace, e.Name, wi, err)
				}
			}

			fields := make([]string, 0, len(w.Secrets))
			for field := range w.Secrets {
				fields = append(fields, field)
			}
			slices.Sort(fields)

			for _, field := range fields {
				hash := strings.ToUpper(shortHash(fmt.Sprintf("%s/%s/%d/%s", namespace, e.Name, wi, field)))
				describe := func() string { return fmt.Sprintf("ServiceEntry %s widget %d field %q", e.Name, wi, field) }
				val, err := projection.resolve(ctx, r.Client, namespace, hash, w.Secrets[field], describe)
				if err != nil {
					return nil, err
				}
				cfg[field] = val
			}

			widgets = append(widgets, render.ServiceWidgetInput{Type: w.Type, URL: w.URL, Config: cfg})
		}

		inputs = append(inputs, render.ServiceInput{
			Group:       e.Spec.Group,
			Name:        e.Spec.Name,
			Order:       e.Spec.Order,
			Href:        e.Spec.Href,
			Icon:        e.Spec.Icon,
			Description: e.Spec.Description,
			Ping:        e.Spec.Ping,
			SiteMonitor: e.Spec.SiteMonitor,
			Target:      e.Spec.Target,
			StatusStyle: e.Spec.StatusStyle,
			ShowStats:   e.Spec.ShowStats,
			HideErrors:  e.Spec.HideErrors,
			Widgets:     widgets,
		})
	}

	return inputs, nil
}

// shortHash returns a short, stable hex digest of s.
func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:12]
}
