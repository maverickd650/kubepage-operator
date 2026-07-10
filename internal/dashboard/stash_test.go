package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestStashWidgetPoll(t *testing.T) {
	tests := map[string]struct {
		response   string
		statusCode int
		want       []Field
	}{
		"library stats": {
			response:   `{"data":{"stats":{"scene_count":120,"image_count":45,"gallery_count":7}}}`,
			statusCode: http.StatusOK,
			want: []Field{
				{Label: labelScenes, Value: "120"},
				{Label: labelImages, Value: "45"},
				{Label: labelGalleries, Value: "7"},
			},
		},
		"empty library": {
			response:   `{"data":{"stats":{"scene_count":0,"image_count":0,"gallery_count":0}}}`,
			statusCode: http.StatusOK,
			want: []Field{
				{Label: labelScenes, Value: "0"},
				{Label: labelImages, Value: "0"},
				{Label: labelGalleries, Value: "0"},
			},
		},
		testCaseNon200: {
			statusCode: http.StatusForbidden,
			want: []Field{
				{Label: labelStatus, Value: testHTTP403},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var gotKey, gotMethod string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotKey = r.Header.Get("ApiKey")
				gotMethod = r.Method
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.response))
			}))
			defer srv.Close()

			got, err := (stashWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
				URL:     srv.URL,
				Secrets: map[string]string{testSecretField: "stashkey"},
			})
			if err != nil {
				t.Fatalf("Poll() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(tc.want, got) {
				t.Errorf("Poll() = %+v, want %+v", got, tc.want)
			}
			if gotKey != "stashkey" {
				t.Errorf("ApiKey header = %q, want %q", gotKey, "stashkey")
			}
			if gotMethod != http.MethodPost {
				t.Errorf("method = %q, want POST", gotMethod)
			}
		})
	}
}

func TestStashWidgetPollMissingURL(t *testing.T) {
	if _, err := (stashWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestStashWidgetPollUnreachable(t *testing.T) {
	got, err := (stashWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: testUnreachableAddr})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestStashWidgetSample(t *testing.T) {
	got := (stashWidget{}).Sample(WidgetConfig{})
	if len(got) != 3 || got[0].Label != labelScenes || got[1].Label != labelImages || got[2].Label != labelGalleries {
		t.Errorf("Sample() = %+v, want Scenes/Images/Galleries fields", got)
	}
	assertSampleDeterministic(t, stashWidget{})
}
