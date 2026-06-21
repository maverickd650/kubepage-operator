package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
	"github.com/maverickd650/kubepage-operator/internal/render"
)

// buildWidgetInputs converts widgets into render-ready WidgetInputs,
// resolving every Secrets field into a {{HOMEPAGE_FILE_*}} Options
// placeholder via projection, the same way buildServiceInputs does for
// ServiceEntry widgets. Both share projection so every secret an Instance's
// bound config CRDs reference lands in one aggregated projected Secret
// volume.
func (r *InstanceReconciler) buildWidgetInputs(ctx context.Context, namespace string, widgets []pagev1alpha1.InfoWidget, projection *secretProjection) ([]render.WidgetInput, error) {
	inputs := make([]render.WidgetInput, 0, len(widgets))
	for _, w := range widgets {
		options := map[string]any{}
		if w.Spec.Options != nil && len(w.Spec.Options.Raw) > 0 {
			if err := json.Unmarshal(w.Spec.Options.Raw, &options); err != nil {
				return nil, fmt.Errorf("decoding options for InfoWidget %s/%s: %w", w.Namespace, w.Name, err)
			}
		}

		fields := make([]string, 0, len(w.Spec.Secrets))
		for field := range w.Spec.Secrets {
			fields = append(fields, field)
		}
		slices.Sort(fields)

		for _, field := range fields {
			hash := strings.ToUpper(shortHash(fmt.Sprintf("%s/infowidget/%s/%s", namespace, w.Name, field)))
			describe := func() string { return fmt.Sprintf("InfoWidget %s field %q", w.Name, field) }
			val, err := projection.resolve(ctx, r.Client, namespace, hash, w.Spec.Secrets[field], describe)
			if err != nil {
				return nil, err
			}
			options[field] = val
		}

		inputs = append(inputs, render.WidgetInput{
			Name:    w.Name,
			Type:    w.Spec.Type,
			Order:   w.Spec.Order,
			Options: options,
		})
	}

	return inputs, nil
}
