package render

import "testing"

func TestServices_SingleGroupSingleService(t *testing.T) {
	got, err := Services([]ServiceInput{
		{
			Group:       "Media",
			Name:        "Sonarr",
			Href:        ptr("http://sonarr.example.com"),
			Icon:        ptr("sonarr.png"),
			Description: ptr("Series management"),
		},
	})
	if err != nil {
		t.Fatalf("Services: %v", err)
	}
	assertGolden(t, "services_single", got)
}

// TestServices_OrderingAndGrouping covers: groups ordered by their minimum
// member Order (nil last) then name; entries within a group ordered the
// same way; a group with no explicit Order on any member sorts after ones
// that have one.
func TestServices_OrderingAndGrouping(t *testing.T) {
	got, err := Services([]ServiceInput{
		{Group: "Zeta", Name: "NoOrderService", Href: ptr("http://z.example.com")},
		{Group: "Alpha", Name: "Second", Order: ptr(int32(2)), Href: ptr("http://a2.example.com")},
		{Group: "Alpha", Name: "First", Order: ptr(int32(1)), Href: ptr("http://a1.example.com")},
		{Group: "Beta", Name: "OnlyOne", Order: ptr(int32(5)), Href: ptr("http://b.example.com")},
	})
	if err != nil {
		t.Fatalf("Services: %v", err)
	}
	assertGolden(t, "services_ordering", got)
}

func TestServices_MultipleWidgets(t *testing.T) {
	got, err := Services([]ServiceInput{
		{
			Group: "Monitoring",
			Name:  "Multi",
			Href:  ptr("http://multi.example.com"),
			Widgets: []ServiceWidgetInput{
				{Type: "emby", URL: ptr("http://emby.example.com")},
				{Type: "uptimekuma", URL: ptr("http://kuma.example.com"), Config: map[string]any{"slug": "statuspageslug"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("Services: %v", err)
	}
	assertGolden(t, "services_multi_widget", got)
}

// TestServices_SecretPlaceholderPassthrough proves the renderer treats an
// already-resolved secret placeholder as an ordinary Config string value —
// it has no special knowledge of secrets at all, that's the caller's job.
func TestServices_SecretPlaceholderPassthrough(t *testing.T) {
	got, err := Services([]ServiceInput{
		{
			Group: "Media",
			Name:  "Sonarr",
			Href:  ptr("http://sonarr.example.com"),
			Widgets: []ServiceWidgetInput{
				{
					Type: "sonarr",
					URL:  ptr("http://sonarr.example.com"),
					Config: map[string]any{
						"key":    "{{HOMEPAGE_FILE_ABC123}}",
						"fields": []string{"wanted", "queued"},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Services: %v", err)
	}
	assertGolden(t, "services_secret_placeholder", got)
}
