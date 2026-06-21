package controller

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// secretProjection accumulates secret-backed file projections (and their
// matching HOMEPAGE_FILE_* env vars) across every config CRD an Instance
// renders (ServiceEntry widgets, InfoWidget options, ...), so they all land
// in one aggregated projected Secret volume rather than one per CRD kind.
type secretProjection struct {
	bySecret map[string][]corev1.KeyToPath
	seenHash map[string]bool
	env      []corev1.EnvVar
}

func newSecretProjection() *secretProjection {
	return &secretProjection{bySecret: map[string][]corev1.KeyToPath{}, seenHash: map[string]bool{}}
}

// resolve returns the value to use for a secret-bearing field: the literal
// Value if set, otherwise a {{HOMEPAGE_FILE_<hash>}} placeholder backed by a
// newly-registered projection item + env var for src.SecretKeyRef. hash must
// be a caller-computed, stable, collision-free identifier for this field
// (typically derived from namespace/CRD-kind/object-name/field); resolve
// itself performs no hashing so each call site's existing hash scheme (and
// therefore its env var names, and therefore rollout stability across
// reconciles) is unaffected by sharing this aggregator.
func (p *secretProjection) resolve(ctx context.Context, c client.Client, namespace, hash string, src pagev1alpha1.SecretValueSource, describe func() string) (string, error) {
	if src.SecretKeyRef == nil {
		if src.Value != nil {
			return *src.Value, nil
		}
		return "", fmt.Errorf("%s has neither value nor secretKeyRef set", describe())
	}

	ref := src.SecretKeyRef
	secret := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: namespace}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return "", fmt.Errorf("%s references Secret %q which does not exist in namespace %q", describe(), ref.Name, namespace)
		}
		return "", fmt.Errorf("getting Secret %q: %w", ref.Name, err)
	}
	if _, ok := secret.Data[ref.Key]; !ok {
		return "", fmt.Errorf("%s references key %q which does not exist in Secret %q", describe(), ref.Key, ref.Name)
	}

	if !p.seenHash[hash] {
		p.seenHash[hash] = true
		p.bySecret[ref.Name] = append(p.bySecret[ref.Name], corev1.KeyToPath{Key: ref.Key, Path: hash})
		p.env = append(p.env, corev1.EnvVar{
			Name:  "HOMEPAGE_FILE_" + hash,
			Value: filepath.Join(secretsMountPath, hash),
		})
	}

	return fmt.Sprintf("{{HOMEPAGE_FILE_%s}}", hash), nil
}

// finalize returns the projected-volume sources and env vars accumulated so
// far, in a deterministic order.
func (p *secretProjection) finalize() ([]corev1.VolumeProjection, []corev1.EnvVar) {
	secretNames := make([]string, 0, len(p.bySecret))
	for name := range p.bySecret {
		secretNames = append(secretNames, name)
	}
	slices.Sort(secretNames)

	sources := make([]corev1.VolumeProjection, 0, len(secretNames))
	for _, name := range secretNames {
		items := p.bySecret[name]
		slices.SortFunc(items, func(a, b corev1.KeyToPath) int { return strings.Compare(a.Path, b.Path) })
		sources = append(sources, corev1.VolumeProjection{
			Secret: &corev1.SecretProjection{
				LocalObjectReference: corev1.LocalObjectReference{Name: name},
				Items:                items,
			},
		})
	}

	env := slices.Clone(p.env)
	slices.SortFunc(env, func(a, b corev1.EnvVar) int { return strings.Compare(a.Name, b.Name) })

	return sources, env
}
