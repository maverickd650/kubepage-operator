package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestMealieWidgetPoll(t *testing.T) {
	tests := map[string]struct {
		config     []byte
		response   string
		statusCode int
		wantPath   string
		want       []Field
	}{
		"v2 (default) statistics": {
			response:   `{"totalRecipes":87,"totalUsers":3,"totalCategories":12,"totalTags":20}`,
			statusCode: http.StatusOK,
			wantPath:   mealieStatisticsPathV2,
			want: []Field{
				{Label: labelRecipes, Value: "87"},
				{Label: labelUsers, Value: "3"},
				{Label: labelCategories, Value: "12"},
				{Label: labelTags, Value: "20"},
			},
		},
		"v1 statistics": {
			config:     []byte(`{"version":1}`),
			response:   `{"totalRecipes":10,"totalUsers":1,"totalCategories":2,"totalTags":5}`,
			statusCode: http.StatusOK,
			wantPath:   mealieStatisticsPathV1,
			want: []Field{
				{Label: labelRecipes, Value: "10"},
				{Label: labelUsers, Value: "1"},
				{Label: labelCategories, Value: "2"},
				{Label: labelTags, Value: "5"},
			},
		},
		"empty": {
			response:   `{"totalRecipes":0,"totalUsers":0,"totalCategories":0,"totalTags":0}`,
			statusCode: http.StatusOK,
			wantPath:   mealieStatisticsPathV2,
			want: []Field{
				{Label: labelRecipes, Value: "0"},
				{Label: labelUsers, Value: "0"},
				{Label: labelCategories, Value: "0"},
				{Label: labelTags, Value: "0"},
			},
		},
		testCaseNon200: {
			statusCode: http.StatusUnauthorized,
			wantPath:   mealieStatisticsPathV2,
			want:       []Field{{Label: labelStatus, Value: testHTTP401}},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var gotAuth, gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAuth = r.Header.Get("Authorization")
				gotPath = r.URL.Path
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.response))
			}))
			defer srv.Close()

			got, err := (mealieWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
				URL:     srv.URL,
				Secrets: map[string]string{testSecretField: "mealtok"},
				Config:  tc.config,
			})
			if err != nil {
				t.Fatalf("Poll() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(tc.want, got) {
				t.Errorf("Poll() = %+v, want %+v", got, tc.want)
			}
			if gotAuth != "Bearer mealtok" {
				t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer mealtok")
			}
			if gotPath != tc.wantPath {
				t.Errorf("request path = %q, want %q", gotPath, tc.wantPath)
			}
		})
	}
}

func TestMealieWidgetPollMissingURL(t *testing.T) {
	if _, err := (mealieWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestMealieWidgetPollInvalidConfig(t *testing.T) {
	if _, err := (mealieWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{
		URL:    testUnreachableAddr,
		Config: []byte(`{not valid json`),
	}); err == nil {
		t.Fatal("Poll() expected error for malformed config, got nil")
	}
}

func TestMealieWidgetPollUnreachable(t *testing.T) {
	got, err := (mealieWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: testUnreachableAddr})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestMealieWidgetSample(t *testing.T) {
	got := (mealieWidget{}).Sample(WidgetConfig{})
	if len(got) != 4 || got[0].Label != labelRecipes || got[1].Label != labelUsers ||
		got[2].Label != labelCategories || got[3].Label != labelTags {
		t.Errorf("Sample() = %+v, want Recipes/Users/Categories/Tags fields", got)
	}
	assertSampleDeterministic(t, mealieWidget{})
}
