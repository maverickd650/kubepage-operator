package dashboard

import "testing"

func TestIconURL(t *testing.T) {
	cases := []struct {
		name string
		icon *string
		want string
	}{
		{"nil", nil, ""},
		{"empty", new(""), ""},
		{"full https url passes through", new("https://example.invalid/icon.png"), "https://example.invalid/icon.png"},
		{"full http url passes through", new("http://example.invalid/icon.png"), "http://example.invalid/icon.png"},
		{"plain slug resolves to dashboard-icons CDN", new("prometheus"), dashboardIconsBaseURL + "prometheus.png"},
		{"plain slug lowercased", new("Grafana"), dashboardIconsBaseURL + "grafana.png"},
		{"mdi- resolves to iconify mdi set", new("mdi-flask-outline"), iconifyBaseURL + "mdi/flask-outline.svg"},
		{"si- resolves to iconify simple-icons set", new("si-github"), iconifyBaseURL + "simple-icons/github.svg"},
		{"mdi- with hex color suffix", new("mdi-home-#f0d453"), iconifyBaseURL + "mdi/home.svg?color=%23f0d453"},
		{"si- with hex color suffix", new("si-github-#a712a2"), iconifyBaseURL + "simple-icons/github.svg?color=%23a712a2"},
		{"sh- bare slug defaults to png", new("sh-plex"), selfhstIconsBaseURL + "png/plex.png"},
		{"sh- with explicit svg extension", new("sh-plex.svg"), selfhstIconsBaseURL + "svg/plex.svg"},
		{"sh- with explicit webp extension", new("sh-plex.webp"), selfhstIconsBaseURL + "webp/plex.webp"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IconURL(tc.icon); got != tc.want {
				t.Errorf("IconURL(%v) = %q, want %q", tc.icon, got, tc.want)
			}
		})
	}
}
