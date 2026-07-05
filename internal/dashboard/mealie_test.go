package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestMealieWidgetPoll(t *testing.T) {
	tests := map[string]struct {
		response   string
		statusCode int
		want       []Field
	}{
		"recipes": {
			response:   `{"total":87}`,
			statusCode: http.StatusOK,
			want:       []Field{{Label: labelRecipes, Value: "87"}},
		},
		"empty": {
			response:   `{"total":0}`,
			statusCode: http.StatusOK,
			want:       []Field{{Label: labelRecipes, Value: "0"}},
		},
		testCaseNon200: {
			statusCode: http.StatusUnauthorized,
			want:       []Field{{Label: labelStatus, Value: testHTTP401}},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var gotAuth string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAuth = r.Header.Get("Authorization")
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.response))
			}))
			defer srv.Close()

			got, err := (mealieWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
				URL:     srv.URL,
				Secrets: map[string]string{testSecretField: "mealtok"},
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
		})
	}
}

func TestMealieWidgetPollMissingURL(t *testing.T) {
	if _, err := (mealieWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
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
	if len(got) != 1 || got[0].Label != labelRecipes {
		t.Errorf("Sample() = %+v, want a single Recipes field", got)
	}
	if !reflect.DeepEqual(got, (mealieWidget{}).Sample(WidgetConfig{})) {
		t.Error("Sample() is not deterministic")
	}
}
