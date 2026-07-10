package dashboard

import (
	"maps"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// mergeWidgetSecrets returns the effective secrets map and CA cert for a
// widget of widgetType, filling gaps in the widget's own secrets/caCert from
// defaults[widgetType] (Dashboard.Spec.WidgetDefaults) rather than
// overriding them — the widget's own value always wins for a given key. defaults
// may be nil (no widgetDefaults set) or missing an entry for widgetType, in
// which case secrets/caCert are returned unchanged.
func mergeWidgetSecrets(
	widgetType string,
	secrets map[string]pagev1alpha1.SecretValueSource,
	caCert *pagev1alpha1.SecretValueSource,
	defaults map[string]pagev1alpha1.WidgetDefaultsEntry,
) (map[string]pagev1alpha1.SecretValueSource, *pagev1alpha1.SecretValueSource) {
	entry, ok := defaults[widgetType]
	if !ok {
		return secrets, caCert
	}

	merged := secrets
	if len(entry.Secrets) > 0 {
		merged = make(map[string]pagev1alpha1.SecretValueSource, len(secrets)+len(entry.Secrets))
		maps.Copy(merged, entry.Secrets)
		maps.Copy(merged, secrets)
	}

	if caCert == nil {
		caCert = entry.CACert
	}
	return merged, caCert
}
