package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

const (
	testLinkwardenToken       = "lwtok"
	linkwardenCollectionsPath = "/api/v1/collections"
	linkwardenTagsPath        = "/api/v1/tags"
)

func TestLinkwardenWidgetPoll(t *testing.T) {
	tests := map[string]struct {
		collectionsResponse string
		tagsResponse        string
		statusCode          int
		want                []Field
	}{
		"two collections": {
			collectionsResponse: `{"response":[{"_count":{"links":40}},{"_count":{"links":2}}]}`,
			tagsResponse:        `{"response":[{"id":1},{"id":2},{"id":3}]}`,
			statusCode:          http.StatusOK,
			want: []Field{
				{Label: labelLinks, Value: "42"},
				{Label: labelCollections, Value: "2"},
				{Label: labelTags, Value: "3"},
			},
		},
		"no collections or tags": {
			collectionsResponse: `{"response":[]}`,
			tagsResponse:        `{"response":[]}`,
			statusCode:          http.StatusOK,
			want: []Field{
				{Label: labelLinks, Value: "0"},
				{Label: labelCollections, Value: "0"},
				{Label: labelTags, Value: "0"},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var gotPaths []string
			var gotAuth string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPaths = append(gotPaths, r.URL.Path)
				gotAuth = r.Header.Get("Authorization")
				w.WriteHeader(tc.statusCode)
				switch r.URL.Path {
				case linkwardenCollectionsPath:
					_, _ = w.Write([]byte(tc.collectionsResponse))
				case linkwardenTagsPath:
					_, _ = w.Write([]byte(tc.tagsResponse))
				}
			}))
			defer srv.Close()

			got, err := (linkwardenWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
				URL:     srv.URL,
				Secrets: map[string]string{testSecretField: testLinkwardenToken},
			})
			if err != nil {
				t.Fatalf("Poll() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(tc.want, got) {
				t.Errorf("Poll() = %+v, want %+v", got, tc.want)
			}
			if len(gotPaths) != 2 || gotPaths[0] != linkwardenCollectionsPath || gotPaths[1] != linkwardenTagsPath {
				t.Errorf("request paths = %v, want [/api/v1/collections /api/v1/tags]", gotPaths)
			}
			if gotAuth != "Bearer lwtok" {
				t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer lwtok")
			}
		})
	}
}

func TestLinkwardenWidgetPollNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	got, err := (linkwardenWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{testSecretField: testLinkwardenToken},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: testHTTP401}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

// TestLinkwardenWidgetPollTagsNon200 exercises the second (tags) request
// failing after the first (collections) succeeds.
func TestLinkwardenWidgetPollTagsNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case linkwardenCollectionsPath:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"response":[]}`))
		case linkwardenTagsPath:
			w.WriteHeader(http.StatusUnauthorized)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	got, err := (linkwardenWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
		URL:     srv.URL,
		Secrets: map[string]string{testSecretField: testLinkwardenToken},
	})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: testHTTP401}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestLinkwardenWidgetPollMissingURL(t *testing.T) {
	if _, err := (linkwardenWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestLinkwardenWidgetPollUnreachable(t *testing.T) {
	got, err := (linkwardenWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: testUnreachableAddr})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestLinkwardenWidgetSample(t *testing.T) {
	got := (linkwardenWidget{}).Sample(WidgetConfig{})
	if len(got) != 3 || got[0].Label != labelLinks || got[1].Label != labelCollections || got[2].Label != labelTags {
		t.Errorf("Sample() = %+v, want Links/Collections/Tags fields", got)
	}
	assertSampleDeterministic(t, linkwardenWidget{})
}
