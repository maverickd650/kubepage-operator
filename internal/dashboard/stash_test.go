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
			response: `{"data":{"stats":{` +
				`"scene_count":120,"scenes_size":10737418240,"scenes_duration":36000,"scenes_played":30,` +
				`"image_count":45,"images_size":1073741824,"gallery_count":7,` +
				`"performer_count":12,"studio_count":3,"movie_count":2,"tag_count":20,` +
				`"total_o_count":9,"total_play_count":40,"total_play_duration":7200` +
				`}}}`,
			statusCode: http.StatusOK,
			want: []Field{
				{Label: labelScenes, Value: "120"},
				{Label: labelSceneSize, Value: formatBytesHumanized(10737418240)},
				{Label: labelSceneDuration, Value: formatUptime(36000)},
				{Label: labelScenesPlayed, Value: "30"},
				{Label: labelImages, Value: "45"},
				{Label: labelImageSize, Value: formatBytesHumanized(1073741824)},
				{Label: labelGalleries, Value: "7"},
				{Label: labelPerformers, Value: "12"},
				{Label: labelStudios, Value: "3"},
				{Label: labelMovies, Value: "2"},
				{Label: labelTags, Value: "20"},
				{Label: labelOCount, Value: "9"},
				{Label: labelPlayCount, Value: "40"},
				{Label: labelPlayDuration, Value: formatUptime(7200)},
			},
		},
		"empty library": {
			response:   `{"data":{"stats":{"scene_count":0,"image_count":0,"gallery_count":0}}}`,
			statusCode: http.StatusOK,
			want: []Field{
				{Label: labelScenes, Value: "0"},
				{Label: labelSceneSize, Value: formatBytesHumanized(0)},
				{Label: labelSceneDuration, Value: formatUptime(0)},
				{Label: labelScenesPlayed, Value: "0"},
				{Label: labelImages, Value: "0"},
				{Label: labelImageSize, Value: formatBytesHumanized(0)},
				{Label: labelGalleries, Value: "0"},
				{Label: labelPerformers, Value: "0"},
				{Label: labelStudios, Value: "0"},
				{Label: labelMovies, Value: "0"},
				{Label: labelTags, Value: "0"},
				{Label: labelOCount, Value: "0"},
				{Label: labelPlayCount, Value: "0"},
				{Label: labelPlayDuration, Value: formatUptime(0)},
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
	wantLabels := []string{
		labelScenes, labelSceneSize, labelSceneDuration, labelScenesPlayed,
		labelImages, labelImageSize, labelGalleries,
		labelPerformers, labelStudios, labelMovies, labelTags,
		labelOCount, labelPlayCount, labelPlayDuration,
	}
	if len(got) != len(wantLabels) {
		t.Fatalf("Sample() returned %d fields, want %d: %+v", len(got), len(wantLabels), got)
	}
	for i, label := range wantLabels {
		if got[i].Label != label {
			t.Errorf("Sample()[%d].Label = %q, want %q", i, got[i].Label, label)
		}
		if got[i].Value == "" {
			t.Errorf("Sample()[%d] (%s) has empty Value", i, label)
		}
	}
	assertSampleDeterministic(t, stashWidget{})
}
