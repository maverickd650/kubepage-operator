package render

import (
	"slices"
	"strings"
)

// BookmarkInput is a render-ready bookmark.
type BookmarkInput struct {
	Group       string
	Name        string
	Order       *int32
	Href        string
	Abbr        *string
	Icon        *string
	Description *string
}

// Bookmarks renders entries into homepage's bookmarks.yaml: a sequence of
// single-key group maps, each containing a sequence of single-key bookmark
// maps, each of which maps to a list containing exactly one map of fields
// (homepage's "list-under-name" shape, distinct from services.yaml's
// name-to-map shape). Groups and entries within a group are ordered by Order
// (nil sorts last), ties broken by name, since bookmarks.yaml's lists are
// ordered but Kubernetes objects aren't.
func Bookmarks(entries []BookmarkInput) ([]byte, error) {
	var groupNames []string
	groupOrder := map[string]*int32{}
	groupEntries := map[string][]BookmarkInput{}

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
		bms := groupEntries[gname]
		slices.SortFunc(bms, func(a, b BookmarkInput) int {
			if c := compareOrder(a.Order, b.Order); c != 0 {
				return c
			}
			return strings.Compare(a.Name, b.Name)
		})

		bmList := make([]map[string]any, 0, len(bms))
		for _, b := range bms {
			bmList = append(bmList, map[string]any{b.Name: []map[string]any{bookmarkFields(b)}})
		}
		doc = append(doc, map[string]any{gname: bmList})
	}

	return ToYAML(doc)
}

func bookmarkFields(b BookmarkInput) map[string]any {
	fields := map[string]any{"href": b.Href}
	setStr(fields, "abbr", b.Abbr)
	setStr(fields, "icon", b.Icon)
	setStr(fields, "description", b.Description)
	return fields
}
