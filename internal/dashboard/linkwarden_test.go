package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestLinkwardenWidgetPoll(t *testing.T) {
	tests := map[string]struct {
		response   string
		statusCode int
		want       []Field
	}{
		"some links": {
			response:   `{"response":[{},{},{}]}`,
			statusCode: http.StatusOK,
			want:       []Field{{Label: labelLinks, Value: "3"}},
		},
		"no links": {
			response:   `{"response":[]}`,
			statusCode: http.StatusOK,
			want:       []Field{{Label: labelLinks, Value: "0"}},
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

			got, err := (linkwardenWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
				URL:     srv.URL,
				Secrets: map[string]string{testSecretField: "lwtok"},
			})
			if err != nil {
				t.Fatalf("Poll() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(tc.want, got) {
				t.Errorf("Poll() = %+v, want %+v", got, tc.want)
			}
			if gotAuth != "Bearer lwtok" {
				t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer lwtok")
			}
		})
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
	if len(got) != 1 || got[0].Label != labelLinks {
		t.Errorf("Sample() = %+v, want a single Links field", got)
	}
	if !reflect.DeepEqual(got, (linkwardenWidget{}).Sample(WidgetConfig{})) {
		t.Error("Sample() is not deterministic")
	}
}
