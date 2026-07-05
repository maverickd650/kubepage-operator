package dashboard

import (
	"net/http"
	"reflect"
	"testing"
)

func TestIframeWidgetPoll(t *testing.T) {
	tests := map[string]struct {
		url     string
		config  string
		want    []Field
		wantErr bool
	}{
		"default height": {
			url:  testIframeURL,
			want: []Field{{Label: labelIframeSrc, Value: testIframeURL}, {Label: labelIframeHeight, Value: iframeDefaultHeight}},
		},
		"custom height": {
			url:    testIframeURL,
			config: `{"height":"50vh"}`,
			want:   []Field{{Label: labelIframeSrc, Value: testIframeURL}, {Label: labelIframeHeight, Value: "50vh"}},
		},
		"missing url": {
			wantErr: true,
		},
		"non-http scheme": {
			url:     testJSSchemeURL,
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := (iframeWidget{}).Poll(t.Context(), http.DefaultClient, WidgetConfig{
				URL:    tc.url,
				Config: []byte(tc.config),
			})
			if tc.wantErr {
				if err == nil {
					t.Fatal("Poll() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Poll() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(tc.want, got) {
				t.Errorf("Poll() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestIframeWidgetSample(t *testing.T) {
	tests := map[string]struct {
		url    string
		config string
		want   []Field
	}{
		"url and default height": {
			url:  testIframeURL,
			want: []Field{{Label: labelIframeSrc, Value: testIframeURL}, {Label: labelIframeHeight, Value: iframeDefaultHeight}},
		},
		"custom height": {
			url:    testIframeURL,
			config: `{"height":"50vh"}`,
			want:   []Field{{Label: labelIframeSrc, Value: testIframeURL}, {Label: labelIframeHeight, Value: "50vh"}},
		},
		"no url falls back to a placeholder": {
			want: []Field{{Label: labelIframeSrc, Value: "https://example.invalid/embed"}, {Label: labelIframeHeight, Value: iframeDefaultHeight}},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := (iframeWidget{}).Sample(WidgetConfig{URL: tc.url, Config: []byte(tc.config)})
			if !reflect.DeepEqual(tc.want, got) {
				t.Errorf("Sample() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestIframeSrcAndHeight(t *testing.T) {
	fields := []Field{{Label: labelIframeSrc, Value: testExampleURL}, {Label: labelIframeHeight, Value: testIframeHeight}}
	if got := iframeSrc(fields); got != testExampleURL {
		t.Errorf("iframeSrc() = %q, want %q", got, testExampleURL)
	}
	if got := iframeHeight(fields); got != testIframeHeight {
		t.Errorf("iframeHeight() = %q, want %q", got, testIframeHeight)
	}
	if got := iframeSrc(nil); got != "" {
		t.Errorf("iframeSrc(nil) = %q, want empty", got)
	}
}
