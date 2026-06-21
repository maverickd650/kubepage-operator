package render

import (
	"encoding/json"
	"fmt"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// Settings renders a Configuration's spec into homepage's settings.yaml.
// Fields not modeled as typed ConfigurationSpec fields can be supplied via
// spec.Extra; if a key is set both there and via a typed field, the typed
// field wins. instanceRef and extra are bookkeeping for the operator and are
// never themselves emitted into the rendered document.
func Settings(spec *pagev1alpha1.ConfigurationSpec) ([]byte, error) {
	doc, err := toMap(spec)
	if err != nil {
		return nil, fmt.Errorf("converting configuration spec: %w", err)
	}
	delete(doc, "instanceRef")
	delete(doc, "extra")

	if spec.Extra != nil && len(spec.Extra.Raw) > 0 {
		var extra map[string]any
		if err := json.Unmarshal(spec.Extra.Raw, &extra); err != nil {
			return nil, fmt.Errorf("decoding extra: %w", err)
		}
		for k, v := range extra {
			if _, set := doc[k]; !set {
				doc[k] = v
			}
		}
	}

	return ToYAML(doc)
}

// toMap round-trips v through JSON to get a plain map keyed by its json tags,
// the same shape ToYAML would otherwise produce directly from the struct.
func toMap(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}
