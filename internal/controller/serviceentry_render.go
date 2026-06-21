package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

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
// placeholder, and returns the projected-volume sources and env vars needed
// to back those placeholders. Resolution validates that each referenced
// Secret and key actually exist, so a typo surfaces as a clear reconcile
// error/status condition instead of a silent kubelet-level mount failure.
func (r *InstanceReconciler) buildServiceInputs(ctx context.Context, namespace string, entries []pagev1alpha1.ServiceEntry) ([]render.ServiceInput, []corev1.VolumeProjection, []corev1.EnvVar, error) {
	bySecret := map[string][]corev1.KeyToPath{}
	seenHash := map[string]bool{}
	var env []corev1.EnvVar

	inputs := make([]render.ServiceInput, 0, len(entries))
	for _, e := range entries {
		widgets := make([]render.ServiceWidgetInput, 0, len(e.Spec.Widgets))
		for wi, w := range e.Spec.Widgets {
			cfg := map[string]any{}
			if w.Config != nil && len(w.Config.Raw) > 0 {
				if err := json.Unmarshal(w.Config.Raw, &cfg); err != nil {
					return nil, nil, nil, fmt.Errorf("decoding config for ServiceEntry %s/%s widget %d: %w", e.Namespace, e.Name, wi, err)
				}
			}

			fields := make([]string, 0, len(w.Secrets))
			for field := range w.Secrets {
				fields = append(fields, field)
			}
			slices.Sort(fields)

			for _, field := range fields {
				val, err := r.resolveSecretValue(ctx, namespace, e.Name, wi, field, w.Secrets[field], bySecret, seenHash, &env)
				if err != nil {
					return nil, nil, nil, err
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

	secretNames := make([]string, 0, len(bySecret))
	for name := range bySecret {
		secretNames = append(secretNames, name)
	}
	slices.Sort(secretNames)

	sources := make([]corev1.VolumeProjection, 0, len(secretNames))
	for _, name := range secretNames {
		items := bySecret[name]
		slices.SortFunc(items, func(a, b corev1.KeyToPath) int { return strings.Compare(a.Path, b.Path) })
		sources = append(sources, corev1.VolumeProjection{
			Secret: &corev1.SecretProjection{
				LocalObjectReference: corev1.LocalObjectReference{Name: name},
				Items:                items,
			},
		})
	}
	slices.SortFunc(env, func(a, b corev1.EnvVar) int { return strings.Compare(a.Name, b.Name) })

	return inputs, sources, env, nil
}

// resolveSecretValue returns the value to put in a widget's Config for one
// secret-bearing field: the literal Value if set, otherwise a
// {{HOMEPAGE_FILE_<hash>}} placeholder backed by a newly-registered
// projection item + env var for src.SecretKeyRef. The hash is derived from
// (namespace, ServiceEntry name, widget index, field) so it's stable across
// reconciles without needing to persist any state.
func (r *InstanceReconciler) resolveSecretValue(
	ctx context.Context,
	namespace, entryName string,
	widgetIdx int,
	field string,
	src pagev1alpha1.SecretValueSource,
	bySecret map[string][]corev1.KeyToPath,
	seenHash map[string]bool,
	env *[]corev1.EnvVar,
) (string, error) {
	if src.SecretKeyRef == nil {
		if src.Value != nil {
			return *src.Value, nil
		}
		return "", fmt.Errorf("ServiceEntry %s widget %d field %q has neither value nor secretKeyRef set", entryName, widgetIdx, field)
	}

	ref := src.SecretKeyRef
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: namespace}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return "", fmt.Errorf("ServiceEntry %s widget %d field %q references Secret %q which does not exist in namespace %q",
				entryName, widgetIdx, field, ref.Name, namespace)
		}
		return "", fmt.Errorf("getting Secret %q: %w", ref.Name, err)
	}
	if _, ok := secret.Data[ref.Key]; !ok {
		return "", fmt.Errorf("ServiceEntry %s widget %d field %q references key %q which does not exist in Secret %q",
			entryName, widgetIdx, field, ref.Key, ref.Name)
	}

	hash := strings.ToUpper(shortHash(fmt.Sprintf("%s/%s/%d/%s", namespace, entryName, widgetIdx, field)))
	if !seenHash[hash] {
		seenHash[hash] = true
		bySecret[ref.Name] = append(bySecret[ref.Name], corev1.KeyToPath{Key: ref.Key, Path: hash})
		*env = append(*env, corev1.EnvVar{
			Name:  "HOMEPAGE_FILE_" + hash,
			Value: filepath.Join(secretsMountPath, hash),
		})
	}

	return fmt.Sprintf("{{HOMEPAGE_FILE_%s}}", hash), nil
}

// shortHash returns a short, stable hex digest of s.
func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:12]
}
