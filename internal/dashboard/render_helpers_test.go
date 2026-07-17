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

// TestGridClasses pins the one place style/columns combine (gridClasses):
// homepage's layout semantics make `style: row` + explicit columns a normal
// wrapping N-column grid, so grid-row (the horizontal scroller) is emitted
// only for row WITHOUT columns.
func TestGridClasses(t *testing.T) {
	const wantBareGrid = "grid"
	two := int32(2)
	tests := map[string]struct {
		extra        string
		style        string
		columns      *int32
		equalHeights bool
		want         string
	}{
		"default":                {want: wantBareGrid},
		"row without columns":    {style: styleRow, want: "grid grid-row"},
		"row with columns":       {style: styleRow, columns: &two, want: wantBareGrid},
		"columns only":           {columns: &two, want: wantBareGrid},
		"equal heights":          {equalHeights: true, want: "grid grid-equal"},
		"row equal heights":      {style: styleRow, equalHeights: true, want: "grid grid-row grid-equal"},
		"row columns equal":      {style: styleRow, columns: &two, equalHeights: true, want: "grid grid-equal"},
		"extra class prefixes":   {extra: "subgroups", style: styleRow, want: "subgroups grid grid-row"},
		"extra with columns":     {extra: "subgroups", style: styleRow, columns: &two, want: "subgroups grid"},
		"column style stays off": {style: "column", want: wantBareGrid},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := gridClasses(tc.extra, tc.style, tc.columns, tc.equalHeights); got != tc.want {
				t.Errorf("gridClasses(%q, %q, %v, %v) = %q, want %q", tc.extra, tc.style, tc.columns, tc.equalHeights, got, tc.want)
			}
		})
	}
}

func TestIsNewTabTarget(t *testing.T) {
	tests := map[string]struct {
		target string
		want   bool
	}{
		"blank opens a new tab":         {target: defaultTarget, want: true},
		"empty stays in place":          {target: "", want: false},
		"self stays in place":           {target: targetSelf, want: false},
		"parent stays in place":         {target: "_parent", want: false},
		"top stays in place":            {target: targetTop, want: false},
		"named frame opens new context": {target: "sidebar", want: true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := isNewTabTarget(tc.target); got != tc.want {
				t.Errorf("isNewTabTarget(%q) = %v, want %v", tc.target, got, tc.want)
			}
		})
	}
}

func TestIsHTTPURL(t *testing.T) {
	tests := map[string]struct {
		in   string
		want bool
	}{
		"https scheme":        {in: "https://example.com/search?q=", want: true},
		"http scheme":         {in: "http://example.com/search?q=", want: true},
		"javascript scheme":   {in: testJSSchemeURL, want: false},
		"data scheme":         {in: "data:text/html,<script>alert(1)</script>", want: false},
		"file scheme":         {in: "file:///etc/passwd", want: false},
		"scheme-relative":     {in: "//example.com/search?q=", want: false},
		"empty string":        {in: "", want: false},
		"http in middle":      {in: "javascript:void(0)//http://example.com", want: false},
		"uppercase scheme":    {in: "HTTPS://example.com", want: false},
		"whitespace prefixed": {in: " http://example.com", want: false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := isHTTPURL(tc.in); got != tc.want {
				t.Errorf("isHTTPURL(%q) = %v, want %v", tc.in, got, tc.want)
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
		"card override wins": {card: Card{Target: targetTop}, siteTarget: defaultTarget, want: targetTop},
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

// TestBookmarkTarget covers bookmarkTarget's fallback chain: a bookmark's
// own Target override wins over the spec.style-derived site default,
// which itself defaults to "_blank" (see site.go's LoadSite).
func TestBookmarkTarget(t *testing.T) {
	tests := map[string]struct {
		bookmark   BookmarkCard
		siteTarget string
		want       string
	}{
		"entry override wins":             {bookmark: BookmarkCard{Target: targetSelf}, siteTarget: defaultTarget, want: targetSelf},
		"falls back to style default":     {bookmark: BookmarkCard{}, siteTarget: targetSelf, want: targetSelf},
		"falls back to _blank when unset": {bookmark: BookmarkCard{}, siteTarget: defaultTarget, want: defaultTarget},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := bookmarkTarget(tc.bookmark, tc.siteTarget); got != tc.want {
				t.Errorf("bookmarkTarget() = %q, want %q", got, tc.want)
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

func TestPodPillText(t *testing.T) {
	tests := map[string]struct {
		card Card
		want string
	}{
		"up renders Running":         {card: Card{PodStatus: "Up"}, want: podPillRunning},
		"partial with ready text":    {card: Card{PodStatus: statusPartial, PodReadyText: testReadyText}, want: testReadyText},
		"partial without ready text": {card: Card{PodStatus: statusPartial, PodReadyText: ""}, want: statusPartial},
		"down renders raw status":    {card: Card{PodStatus: statusDown}, want: statusDown},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := podPillText(tc.card); got != tc.want {
				t.Errorf("podPillText() = %q, want %q", got, tc.want)
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

func TestThemeColorHex(t *testing.T) {
	ramp := Ramp{C50: "#f8fafc", C900: "#0f172a"}
	tests := map[string]struct{ theme, want string }{
		"light theme uses C50":        {theme: "light", want: ramp.C50},
		"dark theme uses C900":        {theme: "dark", want: ramp.C900},
		"empty theme defaults dark":   {theme: "", want: ramp.C900},
		"unknown theme defaults dark": {theme: "sepia", want: ramp.C900},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := themeColorHex(tc.theme, ramp); got != tc.want {
				t.Errorf("themeColorHex(%q, ramp) = %q, want %q", tc.theme, got, tc.want)
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
		if strings.Contains(got, "--card-blur") || strings.Contains(got, "--card-opacity") || strings.Contains(got, "--card-backdrop") {
			t.Errorf("rootVarsCSS() = %q, want no blur/opacity/backdrop vars", got)
		}
	})

	t.Run("with blur and background opacity", func(t *testing.T) {
		got := rootVarsCSS("#fff", ramp, "8px", &Background{Opacity: new(int32(75))})
		if !strings.Contains(got, "--card-blur: 8px;") {
			t.Errorf("rootVarsCSS() = %q, missing card-blur", got)
		}
		if !strings.Contains(got, "--card-opacity: 75%;") {
			t.Errorf("rootVarsCSS() = %q, missing card-opacity", got)
		}
		if !strings.Contains(got, "--card-backdrop: blur(var(--card-blur, 8px));") {
			t.Errorf("rootVarsCSS() = %q, missing card-backdrop", got)
		}
	})

	t.Run("background set but opacity nil", func(t *testing.T) {
		got := rootVarsCSS("#fff", ramp, "", &Background{})
		if strings.Contains(got, "--card-opacity") {
			t.Errorf("rootVarsCSS() = %q, want no card-opacity when Opacity is nil", got)
		}
		if !strings.Contains(got, "--card-backdrop") {
			t.Errorf("rootVarsCSS() = %q, want card-backdrop when background is set even without opacity", got)
		}
	})

	t.Run("card blur without background still emits backdrop", func(t *testing.T) {
		// Regression: an explicit cardBlur with no background image must still
		// apply the blur (the .card rule reads --card-backdrop, not
		// --card-blur, so without --card-backdrop the blur silently does
		// nothing).
		got := rootVarsCSS("#fff", ramp, "16px", nil)
		if !strings.Contains(got, "--card-blur: 16px;") {
			t.Errorf("rootVarsCSS() = %q, missing card-blur", got)
		}
		if !strings.Contains(got, "--card-backdrop: blur(var(--card-blur, 8px));") {
			t.Errorf("rootVarsCSS() = %q, want card-backdrop when cardBlur is set even without a background", got)
		}
		if strings.Contains(got, "--card-opacity") {
			t.Errorf("rootVarsCSS() = %q, want no card-opacity without a background", got)
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
		if got := backgroundStyle("test-nonce", nil); got != "" {
			t.Errorf("backgroundStyle(nil) = %q, want empty string", got)
		}
	})

	t.Run("plain image URL is embedded as-is", func(t *testing.T) {
		got := backgroundStyle("test-nonce", &Background{Image: testBgImageURL})
		want := `<style nonce="test-nonce">body::before { content: ""; position: fixed; inset: 0; z-index: -1; background-image: url("` + testBgImageURL + `"); background-size: cover; background-position: center; will-change: transform; }</style>`
		if got != want {
			t.Errorf("backgroundStyle() = %q, want %q", got, want)
		}
	})

	t.Run("brightness and saturate emit a filter", func(t *testing.T) {
		brightness := int32(55)
		saturate := int32(80)
		got := backgroundStyle("test-nonce", &Background{Image: testBgImageURL, Brightness: &brightness, Saturate: &saturate})
		want := `<style nonce="test-nonce">body::before { content: ""; position: fixed; inset: 0; z-index: -1; background-image: url("` + testBgImageURL + `"); background-size: cover; background-position: center; will-change: transform; filter: brightness(55%) saturate(80%); }</style>`
		if got != want {
			t.Errorf("backgroundStyle() = %q, want %q", got, want)
		}
	})

	t.Run("blur emits a filter and oversizes the inset past the viewport", func(t *testing.T) {
		got := backgroundStyle("test-nonce", &Background{Image: testBgImageURL, Blur: blurPxXL})
		want := `<style nonce="test-nonce">body::before { content: ""; position: fixed; inset: calc(-2 * 24px); z-index: -1; background-image: url("` + testBgImageURL + `"); background-size: cover; background-position: center; will-change: transform; filter: blur(24px); }</style>`
		if got != want {
			t.Errorf("backgroundStyle() = %q, want %q", got, want)
		}
	})

	t.Run("all three filters combine in blur-brightness-saturate order", func(t *testing.T) {
		brightness := int32(75)
		saturate := int32(50)
		got := backgroundStyle("test-nonce", &Background{Image: testBgImageURL, Blur: blurPxSM, Brightness: &brightness, Saturate: &saturate})
		if !strings.Contains(got, "filter: blur(4px) brightness(75%) saturate(50%);") {
			t.Errorf("backgroundStyle() = %q, want combined filter blur(4px) brightness(75%%) saturate(50%%)", got)
		}
	})

	// Regression test: backgroundStyle's output is emitted into the page via
	// @templ.Raw, i.e. as unescaped HTML. A CRD-supplied Background.Image
	// containing a literal `</style>` must not be able to close the <style>
	// element early and inject arbitrary markup after it — escaping only the
	// CSS-string metacharacters (backslash/quote) is not enough to prevent
	// that, since HTML tag-termination doesn't care about CSS string escaping.
	t.Run("malicious image value cannot break out of the style tag", func(t *testing.T) {
		got := backgroundStyle("test-nonce", &Background{Image: `"></style><script>alert(1)</script>`})

		if n := strings.Count(got, "</style>"); n != 1 {
			t.Fatalf("backgroundStyle() output contains %d literal </style> closing tags, want exactly 1 (the legitimate one): %q", n, got)
		}
		if strings.Contains(got, "<script") {
			t.Errorf("backgroundStyle() output contains an unescaped <script> tag, want it neutralized: %q", got)
		}
	})
}

func TestCustomStyleAndCustomScript(t *testing.T) {
	t.Run("empty input renders nothing", func(t *testing.T) {
		if got := customStyle("nonce", ""); got != "" {
			t.Errorf("customStyle(nonce, \"\") = %q, want empty string", got)
		}
		if got := customScript("nonce", ""); got != "" {
			t.Errorf("customScript(nonce, \"\") = %q, want empty string", got)
		}
	})

	t.Run("carries the nonce and escapes its own closing tag", func(t *testing.T) {
		css := customStyle("abc123", "body{}</style><script>alert(1)</script>")
		if !strings.Contains(css, `nonce="abc123"`) {
			t.Errorf("customStyle() = %q, want it to carry nonce=\"abc123\"", css)
		}
		if strings.Count(css, "</style>") != 1 {
			t.Errorf("customStyle() = %q, want exactly one </style> (the legitimate one)", css)
		}

		js := customScript("abc123", "1</script><script>alert(1)</script>")
		if !strings.Contains(js, `nonce="abc123"`) {
			t.Errorf("customScript() = %q, want it to carry nonce=\"abc123\"", js)
		}
		if strings.Count(js, "</script>") != 1 {
			t.Errorf("customScript() = %q, want exactly one </script> (the legitimate one)", js)
		}
	})
}
