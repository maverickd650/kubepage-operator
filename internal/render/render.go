// Package render turns the page.kubepage.dev CRDs into the homepage YAML
// config files (settings.yaml, services.yaml, bookmarks.yaml, widgets.yaml)
// that get written into the Instance's owned ConfigMap. See
// IMPLEMENTATION_PLAN.md for the overall architecture; renderers for each
// config domain are added phase by phase (Settings in Phase 1, Services in
// Phase 2, Bookmarks in Phase 3, InfoWidgets in Phase 4).
package render

import (
	"sigs.k8s.io/yaml"
)

// ToYAML marshals v (a Go struct with json tags) to YAML bytes, matching the
// key casing/omitempty rules of its json tags. All per-domain render
// functions (Settings, Services, Bookmarks, Widgets) build on this.
func ToYAML(v any) ([]byte, error) {
	return yaml.Marshal(v)
}
