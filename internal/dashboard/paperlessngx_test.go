package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestPaperlessngxWidgetPoll(t *testing.T) {
	tests := map[string]struct {
		response   string
		statusCode int
		want       []Field
	}{
		"documents": {
			response:   `{"documents_total":542,"documents_inbox":3}`,
			statusCode: http.StatusOK,
			want: []Field{
				{Label: "Documents", Value: "542"},
				{Label: "Inbox", Value: "3"},
			},
		},
		testCaseNon200: {
			statusCode: http.StatusUnauthorized,
			want: []Field{
				{Label: labelStatus, Value: testHTTP401},
			},
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

			got, err := (paperlessngxWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{
				URL:     srv.URL,
				Secrets: map[string]string{testSecretField: "ptok"},
			})
			if err != nil {
				t.Fatalf("Poll() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(tc.want, got) {
				t.Errorf("Poll() = %+v, want %+v", got, tc.want)
			}
			if gotAuth != "Token ptok" {
				t.Errorf("Authorization header = %q, want %q", gotAuth, "Token ptok")
			}
		})
	}
}

func TestPaperlessngxWidgetPollMissingURL(t *testing.T) {
	if _, err := (paperlessngxWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestPaperlessngxWidgetPollUnreachable(t *testing.T) {
	got, err := (paperlessngxWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: testUnreachableAddr})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}
