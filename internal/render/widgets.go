package render

import (
	"slices"
	"strings"
)

// WidgetInput is a render-ready header/info widget. Name is the underlying
// InfoWidget object's name, used only to break ties when Order is equal or
// unset — homepage's widgets.yaml has no name field, so it is never rendered.
type WidgetInput struct {
	Name    string
	Type    string
	Order   *int32
	Options map[string]any
}

// Widgets renders entries into homepage's widgets.yaml: a flat, ordered
// sequence of single-key maps — unlike services.yaml/bookmarks.yaml, there is
// no group nesting. Entries are ordered by Order (nil sorts last), ties
// broken by Name, since widgets.yaml's list is ordered but Kubernetes objects
// aren't.
func Widgets(entries []WidgetInput) ([]byte, error) {
	sorted := slices.Clone(entries)
	slices.SortFunc(sorted, func(a, b WidgetInput) int {
		if c := compareOrder(a.Order, b.Order); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})

	doc := make([]map[string]any, 0, len(sorted))
	for _, w := range sorted {
		options := w.Options
		if options == nil {
			options = map[string]any{}
		}
		doc = append(doc, map[string]any{w.Type: options})
	}

	return ToYAML(doc)
}
