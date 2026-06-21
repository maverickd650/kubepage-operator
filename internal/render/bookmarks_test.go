package render

import "testing"

const groupTools = "Tools"

func TestBookmarks_SingleGroupSingleBookmark(t *testing.T) {
	got, err := Bookmarks([]BookmarkInput{
		{
			Group: groupTools,
			Name:  "Github",
			Href:  "https://github.com/",
			Abbr:  ptr("GH"),
		},
	})
	if err != nil {
		t.Fatalf("Bookmarks: %v", err)
	}
	assertGolden(t, "bookmarks_single", got)
}

func TestBookmarks_IconAndDescription(t *testing.T) {
	got, err := Bookmarks([]BookmarkInput{
		{
			Group:       "Social",
			Name:        "Reddit",
			Href:        "https://reddit.com/",
			Icon:        ptr("reddit.png"),
			Description: ptr("The front page of the internet"),
		},
	})
	if err != nil {
		t.Fatalf("Bookmarks: %v", err)
	}
	assertGolden(t, "bookmarks_icon_description", got)
}

// TestBookmarks_OrderingAndGrouping covers: groups ordered by their minimum
// member Order (nil last) then name; entries within a group ordered the same
// way; a group with no explicit Order on any member sorts after ones that
// have one.
func TestBookmarks_OrderingAndGrouping(t *testing.T) {
	got, err := Bookmarks([]BookmarkInput{
		{Group: "Zeta", Name: "NoOrderBookmark", Href: "https://z.example.com/"},
		{Group: "Apps", Name: "Second", Order: ptr(int32(2)), Href: "https://a2.example.com/"},
		{Group: "Apps", Name: "First", Order: ptr(int32(1)), Href: "https://a1.example.com/"},
		{Group: "Beta", Name: "OnlyOne", Order: ptr(int32(5)), Href: "https://b.example.com/"},
	})
	if err != nil {
		t.Fatalf("Bookmarks: %v", err)
	}
	assertGolden(t, "bookmarks_ordering", got)
}

func TestBookmarks_MultipleBookmarksInGroup(t *testing.T) {
	got, err := Bookmarks([]BookmarkInput{
		{Group: groupTools, Name: "Github", Href: "https://github.com/", Abbr: ptr("GH")},
		{Group: groupTools, Name: "Vercel", Href: "https://vercel.com/", Icon: ptr("vercel.png")},
	})
	if err != nil {
		t.Fatalf("Bookmarks: %v", err)
	}
	assertGolden(t, "bookmarks_multi", got)
}
