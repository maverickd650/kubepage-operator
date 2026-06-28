package dashboard

import (
	"strings"
	"testing"
)

func TestPercentBarStyle(t *testing.T) {
	tests := map[string]struct {
		p    int
		want string
	}{
		"in range":   {p: 42, want: "width: 42%;"},
		"negative":   {p: -5, want: "width: 0%;"},
		"over 100":   {p: 150, want: "width: 100%;"},
		"zero":       {p: 0, want: "width: 0%;"},
		"exactly100": {p: 100, want: "width: 100%;"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := percentBarStyle(tc.p); got != tc.want {
				t.Errorf("percentBarStyle(%d) = %q, want %q", tc.p, got, tc.want)
			}
		})
	}
}

func TestCardTarget(t *testing.T) {
	tests := map[string]struct {
		card       Card
		siteTarget string
		want       string
	}{
		"card override wins": {card: Card{Target: "_top"}, siteTarget: defaultTarget, want: "_top"},
		"falls back to site": {card: Card{}, siteTarget: defaultTarget, want: defaultTarget},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := cardTarget(tc.card, tc.siteTarget); got != tc.want {
				t.Errorf("cardTarget() = %q, want %q", got, tc.want)
			}
		})
	}
}

const testLatency = "12ms"

func TestStatusWithLatency(t *testing.T) {
	tests := map[string]struct {
		status, latency, want string
	}{
		"with latency":    {status: "Up", latency: testLatency, want: "Up · " + testLatency},
		"without latency": {status: statusDown, latency: "", want: statusDown},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := statusWithLatency(tc.status, tc.latency); got != tc.want {
				t.Errorf("statusWithLatency() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestStatusPillText(t *testing.T) {
	tests := map[string]struct {
		card Card
		want string
	}{
		"prefers latency":      {card: Card{Status: "Up", Latency: testLatency}, want: testLatency},
		"falls back to status": {card: Card{Status: statusDown, Latency: ""}, want: statusDown},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := statusPillText(tc.card); got != tc.want {
				t.Errorf("statusPillText() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLangOrDefault(t *testing.T) {
	tests := map[string]struct{ lang, want string }{
		"explicit":     {lang: "fr", want: "fr"},
		"empty string": {lang: "", want: "en"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := langOrDefault(tc.lang); got != tc.want {
				t.Errorf("langOrDefault(%q) = %q, want %q", tc.lang, got, tc.want)
			}
		})
	}
}

func TestRootVarsCSS(t *testing.T) {
	ramp := Ramp{C50: "1", C100: "2", C200: "3", C300: "4", C400: "5", C500: "6", C600: "7", C700: "8", C800: "9", C900: "10"}

	t.Run("no blur or background", func(t *testing.T) {
		got := rootVarsCSS("#fff", ramp, "", nil)
		if !strings.Contains(got, "--accent: #fff;") {
			t.Errorf("rootVarsCSS() = %q, missing accent", got)
		}
		if strings.Contains(got, "--card-blur") || strings.Contains(got, "--card-opacity") {
			t.Errorf("rootVarsCSS() = %q, want no blur/opacity vars", got)
		}
	})

	t.Run("with blur and background opacity", func(t *testing.T) {
		got := rootVarsCSS("#fff", ramp, "8px", &Background{Opacity: ptr(int32(75))})
		if !strings.Contains(got, "--card-blur: 8px;") {
			t.Errorf("rootVarsCSS() = %q, missing card-blur", got)
		}
		if !strings.Contains(got, "--card-opacity: 75%;") {
			t.Errorf("rootVarsCSS() = %q, missing card-opacity", got)
		}
	})

	t.Run("background set but opacity nil", func(t *testing.T) {
		got := rootVarsCSS("#fff", ramp, "", &Background{})
		if strings.Contains(got, "--card-opacity") {
			t.Errorf("rootVarsCSS() = %q, want no card-opacity when Opacity is nil", got)
		}
	})
}

const testBgImageURL = "https://example.com/bg.png"

func TestCSSStringEscape(t *testing.T) {
	tests := map[string]struct{ in, want string }{
		"plain":              {in: testBgImageURL, want: testBgImageURL},
		"backslash":          {in: `a\b`, want: `a\\b`},
		"double quote":       {in: `a"b`, want: `a\"b`},
		"angle brackets":     {in: `a<b>c`, want: `a&lt;b&gt;c`},
		"style tag breakout": {in: `"></style><script>alert(1)</script>`, want: `\"&gt;&lt;/style&gt;&lt;script&gt;alert(1)&lt;/script&gt;`},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := cssStringEscape(tc.in); got != tc.want {
				t.Errorf("cssStringEscape(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestBackgroundStyle(t *testing.T) {
	t.Run("nil background", func(t *testing.T) {
		if got := backgroundStyle(nil); got != "" {
			t.Errorf("backgroundStyle(nil) = %q, want empty string", got)
		}
	})

	t.Run("plain image URL is embedded as-is", func(t *testing.T) {
		got := backgroundStyle(&Background{Image: testBgImageURL})
		want := `<style>body { background-image: url("` + testBgImageURL + `"); background-size: cover; background-position: center; background-attachment: fixed; }</style>`
		if got != want {
			t.Errorf("backgroundStyle() = %q, want %q", got, want)
		}
	})

	// Regression test: backgroundStyle's output is emitted into the page via
	// @templ.Raw, i.e. as unescaped HTML. A CRD-supplied Background.Image
	// containing a literal `</style>` must not be able to close the <style>
	// element early and inject arbitrary markup after it — escaping only the
	// CSS-string metacharacters (backslash/quote) is not enough to prevent
	// that, since HTML tag-termination doesn't care about CSS string escaping.
	t.Run("malicious image value cannot break out of the style tag", func(t *testing.T) {
		got := backgroundStyle(&Background{Image: `"></style><script>alert(1)</script>`})

		if n := strings.Count(got, "</style>"); n != 1 {
			t.Fatalf("backgroundStyle() output contains %d literal </style> closing tags, want exactly 1 (the legitimate one): %q", n, got)
		}
		if strings.Contains(got, "<script") {
			t.Errorf("backgroundStyle() output contains an unescaped <script> tag, want it neutralized: %q", got)
		}
	})
}
