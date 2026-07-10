package controller

import (
	"encoding/json"
	"fmt"
	"strings"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/maverickd650/kubepage-operator/internal/dashboard/widgetschema"
)

// widgetConfigInstance is one widget's config/options block to validate
// against widgetschema.ConfigSchemas, identified by a human-readable
// Location used in condition messages.
type widgetConfigInstance struct {
	// Location names where this widget lives, for status condition
	// messages, e.g. `entry "plex" widget[0] (type "plex")`.
	Location string
	// WidgetType is the ServiceWidget.Type/InfoWidgetEntry.Type value.
	WidgetType string
	// Raw is the widget's Config/Options block, verbatim from the CRD.
	Raw *apiextensionsv1.JSON
	// URLSet is true when the entry also carries a typed URL field
	// (InfoWidgetEntry.URL) that satisfies a schema's "url" key the same
	// way setting it in Options would (glances, longhorn).
	URLSet bool
}

// validateWidgetConfigs checks every instance's Raw config against
// widgetschema.ConfigSchemas for its WidgetType. It returns availableOverride
// non-nil only when at least one instance has a missing required key or an
// unparseable config block — the caller should use it to override an
// otherwise-True Available condition — and configValid, the ConfigValid
// condition to set unconditionally.
func validateWidgetConfigs(instances []widgetConfigInstance, generation int64) (availableOverride *metav1.Condition, configValid metav1.Condition) {
	var invalidMsgs, unknownMsgs []string

	for _, inst := range instances {
		schema, ok := widgetschema.ConfigSchemas[inst.WidgetType]
		if !ok {
			// Unknown widget type: the CRD's Enum marker already rejects
			// this at admission, so this only happens for types that
			// somehow slipped through; nothing to validate against.
			continue
		}

		keys, ok := decodeConfigKeys(inst.Raw)
		if !ok {
			invalidMsgs = append(invalidMsgs, fmt.Sprintf("%s: config is not a JSON object", inst.Location))
			continue
		}
		if inst.URLSet {
			keys["url"] = struct{}{}
		}

		missing, unknown := widgetschema.ValidateConfig(keys, schema)
		if len(missing) > 0 {
			invalidMsgs = append(invalidMsgs, fmt.Sprintf("%s: missing required config keys: %s", inst.Location, strings.Join(missing, ", ")))
		}
		if len(unknown) > 0 {
			unknownMsgs = append(unknownMsgs, fmt.Sprintf("%s: unknown config keys: %s", inst.Location, strings.Join(unknown, ", ")))
		}
	}

	if len(invalidMsgs) > 0 {
		availableOverride = &metav1.Condition{
			Type: typeAvailableBound, Status: metav1.ConditionFalse, Reason: reasonInvalidWidgetConfig,
			Message:            strings.Join(invalidMsgs, "; "),
			ObservedGeneration: generation,
		}
	}

	switch {
	case len(invalidMsgs) > 0:
		configValid = metav1.Condition{
			Type: typeConfigValid, Status: metav1.ConditionFalse, Reason: reasonInvalidWidgetConfig,
			Message:            strings.Join(append(invalidMsgs, unknownMsgs...), "; "),
			ObservedGeneration: generation,
		}
	case len(unknownMsgs) > 0:
		configValid = metav1.Condition{
			Type: typeConfigValid, Status: metav1.ConditionFalse, Reason: reasonUnknownConfigKeys,
			Message:            strings.Join(unknownMsgs, "; "),
			ObservedGeneration: generation,
		}
	default:
		configValid = metav1.Condition{
			Type: typeConfigValid, Status: metav1.ConditionTrue, Reason: reasonConfigValid,
			Message:            "Every widget config key is recognized",
			ObservedGeneration: generation,
		}
	}
	return availableOverride, configValid
}

// decodeConfigKeys unmarshals raw into a map of its top-level keys. A nil or
// empty raw decodes to an empty map (no keys means no required key can be
// present, and nothing is unknown). ok is false when raw is non-empty but
// isn't a JSON object (e.g. an array or scalar somehow stored there).
func decodeConfigKeys(raw *apiextensionsv1.JSON) (keys map[string]any, ok bool) {
	if raw == nil || len(raw.Raw) == 0 {
		return map[string]any{}, true
	}
	var m map[string]any
	if err := json.Unmarshal(raw.Raw, &m); err != nil {
		return nil, false
	}
	return m, true
}
