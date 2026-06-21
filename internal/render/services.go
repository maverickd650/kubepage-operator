package render

import (
	"maps"
	"slices"
	"strings"
)

// ServiceWidgetInput is a render-ready widget: Config already has any secret
// fields resolved to a plain placeholder string (e.g. "{{HOMEPAGE_FILE_*}}")
// by the caller — the renderer never sees a SecretValueSource, only the
// already-resolved value, keeping rendering pure and clientless.
type ServiceWidgetInput struct {
	Type   string
	URL    *string
	Config map[string]any
}

// ServiceInput is a render-ready service card. Group is carried per-entry
// (rather than pre-grouped by the caller) so Services can own all of the
// grouping/ordering logic in one place.
type ServiceInput struct {
	Group       string
	Name        string
	Order       *int32
	Href        *string
	Icon        *string
	Description *string
	Ping        *string
	SiteMonitor *string
	Target      *string
	StatusStyle *string
	ShowStats   *bool
	HideErrors  *bool
	Widgets     []ServiceWidgetInput
}

// Services renders entries into homepage's services.yaml: a sequence of
// single-key group maps, each containing a sequence of single-key service
// maps. Groups and entries within a group are ordered by Order (nil sorts
// last), ties broken by name, since services.yaml's lists are ordered but
// Kubernetes objects aren't.
func Services(entries []ServiceInput) ([]byte, error) {
	var groupNames []string
	groupOrder := map[string]*int32{}
	groupEntries := map[string][]ServiceInput{}

	for _, e := range entries {
		if _, ok := groupEntries[e.Group]; !ok {
			groupNames = append(groupNames, e.Group)
			groupOrder[e.Group] = e.Order
		} else if better(e.Order, groupOrder[e.Group]) {
			groupOrder[e.Group] = e.Order
		}
		groupEntries[e.Group] = append(groupEntries[e.Group], e)
	}

	slices.SortFunc(groupNames, func(a, b string) int {
		if c := compareOrder(groupOrder[a], groupOrder[b]); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	})

	doc := make([]map[string]any, 0, len(groupNames))
	for _, gname := range groupNames {
		svcs := groupEntries[gname]
		slices.SortFunc(svcs, func(a, b ServiceInput) int {
			if c := compareOrder(a.Order, b.Order); c != 0 {
				return c
			}
			return strings.Compare(a.Name, b.Name)
		})

		svcList := make([]map[string]any, 0, len(svcs))
		for _, s := range svcs {
			svcList = append(svcList, map[string]any{s.Name: serviceFields(s)})
		}
		doc = append(doc, map[string]any{gname: svcList})
	}

	return ToYAML(doc)
}

func serviceFields(s ServiceInput) map[string]any {
	fields := map[string]any{}
	setStr(fields, "href", s.Href)
	setStr(fields, "icon", s.Icon)
	setStr(fields, "description", s.Description)
	setStr(fields, "ping", s.Ping)
	setStr(fields, "siteMonitor", s.SiteMonitor)
	setStr(fields, "target", s.Target)
	setStr(fields, "statusStyle", s.StatusStyle)
	if s.ShowStats != nil {
		fields["showStats"] = *s.ShowStats
	}
	if s.HideErrors != nil {
		fields["hideErrors"] = *s.HideErrors
	}

	switch len(s.Widgets) {
	case 0:
	case 1:
		fields["widget"] = widgetFields(s.Widgets[0])
	default:
		widgets := make([]map[string]any, len(s.Widgets))
		for i, w := range s.Widgets {
			widgets[i] = widgetFields(w)
		}
		fields["widgets"] = widgets
	}

	return fields
}

func widgetFields(w ServiceWidgetInput) map[string]any {
	fields := make(map[string]any, len(w.Config)+2)
	maps.Copy(fields, w.Config)
	fields["type"] = w.Type
	setStr(fields, "url", w.URL)
	return fields
}

func setStr(fields map[string]any, key string, v *string) {
	if v != nil {
		fields[key] = *v
	}
}

// better reports whether candidate should replace current as a group's
// representative order (used to pick the min order among a group's members).
func better(candidate, current *int32) bool {
	return compareOrder(candidate, current) < 0
}

// compareOrder orders *int32 ascending with nil sorting last.
func compareOrder(a, b *int32) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return 1
	case b == nil:
		return -1
	case *a < *b:
		return -1
	case *a > *b:
		return 1
	default:
		return 0
	}
}
