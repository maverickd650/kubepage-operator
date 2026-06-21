package dashboard

import "testing"

func ptr[T any](v T) *T { return &v }

func TestIconURL(t *testing.T) {
	httpURL := "https://example.invalid/icon.png"
	slug := "prometheus"
	mdiSlug := "mdi-server"

	cases := []struct {
		name string
		icon *string
		want string
	}{
		{"nil", nil, ""},
		{"empty", ptr(""), ""},
		{"full url passes through", &httpURL, httpURL},
		{"plain slug resolves to CDN", &slug, dashboardIconsBaseURL + "prometheus.png"},
		{"mdi- prefix stripped", &mdiSlug, dashboardIconsBaseURL + "server.png"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IconURL(tc.icon); got != tc.want {
				t.Errorf("IconURL(%v) = %q, want %q", tc.icon, got, tc.want)
			}
		})
	}
}
