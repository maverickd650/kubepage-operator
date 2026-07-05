package dashboard

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestLonghornWidgetPoll(t *testing.T) {
	fifty := 50
	tests := map[string]struct {
		response   string
		statusCode int
		want       []Field
	}{
		"single node two disks": {
			response: `{"data":[{"diskStatus":{
				"disk-1":{"storageMaximum":1000,"storageAvailable":500},
				"disk-2":{"storageMaximum":1000,"storageAvailable":500}
			}}]}`,
			statusCode: http.StatusOK,
			want:       []Field{{Label: labelStorage, Value: "0 / 0 GiB (50%)", Percent: &fifty}},
		},
		"no disks": {
			response:   `{"data":[]}`,
			statusCode: http.StatusOK,
			want:       []Field{{Label: labelStatus, Value: statusUnknown}},
		},
		testCaseNon200: {
			statusCode: http.StatusInternalServerError,
			want:       []Field{{Label: labelStatus, Value: testHTTP500}},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/nodes" {
					t.Errorf("request path = %q, want /v1/nodes", r.URL.Path)
				}
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.response))
			}))
			defer srv.Close()

			got, err := (longhornWidget{}).Poll(t.Context(), srv.Client(), WidgetConfig{URL: srv.URL})
			if err != nil {
				t.Fatalf("Poll() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(tc.want, got) {
				t.Errorf("Poll() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestLonghornWidgetPollMissingURL(t *testing.T) {
	if _, err := (longhornWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{}); err == nil {
		t.Fatal("Poll() expected error for missing URL, got nil")
	}
}

func TestLonghornWidgetPollUnreachable(t *testing.T) {
	got, err := (longhornWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{URL: testUnreachableAddr})
	if err != nil {
		t.Fatalf("Poll() unexpected error: %v", err)
	}
	want := []Field{{Label: labelStatus, Value: statusUnreach}}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("Poll() = %+v, want %+v", got, want)
	}
}

func TestLonghornWidgetSample(t *testing.T) {
	got := (longhornWidget{}).Sample(WidgetConfig{})
	if len(got) != 1 || got[0].Label != labelStorage || got[0].Percent == nil {
		t.Fatalf("Sample() = %+v, want a single Storage field with Percent set", got)
	}
	if !reflect.DeepEqual(got, (longhornWidget{}).Sample(WidgetConfig{})) {
		t.Error("Sample() is not deterministic")
	}
}
