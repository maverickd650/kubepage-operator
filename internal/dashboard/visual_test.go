//go:build browser

package dashboard

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// visualCase is one point in the visual regression matrix (#118 part 2).
// Rather than the full theme(2) x headerStyle(4) x viewport(3) cross
// product (24 screenshots, most of which vary only one axis from a case
// already covered elsewhere), every case but the first ("dark_clean_desktop",
// the baseline) flips exactly one axis away from it — every axis value still
// gets exercised against the same reference at least once, which is what
// actually catches a regression like #119's active-tab contrast inversion or
// missing viewport meta, at a third of the golden images to store/review/
// regenerate.
type visualCase struct {
	name        string
	theme       string
	headerStyle string
	width       int64
	height      int64
}

var visualCases = []visualCase{
	{name: "dark_clean_desktop", theme: "dark", headerStyle: "clean", width: 1280, height: 800},
	{name: "light_clean_desktop", theme: "light", headerStyle: "clean", width: 1280, height: 800},
	{name: "dark_underlined_desktop", theme: "dark", headerStyle: "underlined", width: 1280, height: 800},
	{name: "dark_boxed_desktop", theme: "dark", headerStyle: "boxed", width: 1280, height: 800},
	{name: "dark_boxedWidgets_desktop", theme: "dark", headerStyle: "boxedWidgets", width: 1280, height: 800},
	{name: "dark_clean_tablet", theme: "dark", headerStyle: "clean", width: 768, height: 1024},
	{name: "dark_clean_mobile", theme: "dark", headerStyle: "clean", width: 375, height: 812},
}

// pixelColorTolerance is the maximum summed per-channel (R+G+B+A) delta
// between a golden and a captured pixel before it counts as "different".
// Anti-aliased text/edges can shift by a few color levels between Chrome
// builds without any visible regression; a real one (a swapped accent
// color, a contrast inversion, a moved element) moves whole regions by far
// more than this.
const pixelColorTolerance = 60

// maxMismatchRatio is the fraction of a golden image's pixels allowed to
// exceed pixelColorTolerance before TestVisualRegression fails. Kept small
// but non-zero to absorb minor font-hinting/sub-pixel differences between
// Chrome versions across environments; a genuine layout/style regression
// (see visualCases' doc comment for examples) moves a far larger fraction
// of the page than this.
const maxMismatchRatio = 0.01

// TestVisualRegression drives a real headless Chrome (via chromedp) across
// visualCases and diffs a full-page screenshot of each against a golden PNG
// committed under testdata/visual/ — the automated version of the manual
// cross-theme/cross-viewport review that found #119's bugs. Run with
// UPDATE_GOLDEN=1 to (re)generate the goldens, e.g. after an intentional
// visual change or a Chrome version bump that shifts anti-aliasing beyond
// maxMismatchRatio.
//
// The fixture data is fully static (fixed Field values, no "datetime"
// header widget, no remote icon URLs) so a render only depends on the
// server's own HTML/CSS/fonts — never on wall-clock time or network
// reachability of a third-party icon CDN, both of which would make a pixel
// golden flaky by construction.
func TestVisualRegression(t *testing.T) {
	for _, tc := range visualCases {
		t.Run(tc.name, func(t *testing.T) {
			baseURL, shutdown := startVisualServer(t, tc.theme, tc.headerStyle)
			defer shutdown()

			got := captureScreenshot(t, baseURL, tc.width, tc.height)
			compareVisualGolden(t, tc.name, got)
		})
	}
}

// startVisualServer boots a real HTTP server (not httptest.NewRecorder, since
// chromedp needs an address to navigate a real browser to) over a static
// fixture: two header widgets, three service cards across two groups, and a
// bookmark group — enough surface to catch layout/contrast regressions in
// the header strip, card grid, and bookmarks alike.
func startVisualServer(t *testing.T, theme, headerStyle string) (baseURL string, shutdown func()) {
	t.Helper()

	store := NewStore()
	// The "greet" InfoWidget below is a static "greeting" type — server.go's
	// buildHeader reads its text straight from Options, never from a live
	// Card — so it needs no Store entry.
	// header/<InfoWidget name>/<entry index> is the Key format poller.go's
	// pollInfoWidget actually stores each header Card under (see
	// buildHeader's doc comment) — it must match here for the "cluster"
	// widget below to pick up its live Fields.
	store.Set(Card{
		Key: "header/cluster/0", Header: true, ServiceName: "cluster",
		Fields: []Field{
			{Label: labelCPU, Value: "23%", Percent: intPtr(23), Highlight: HighlightGood},
			{Label: labelMemory, Value: "61%", Percent: intPtr(61), Highlight: HighlightWarn},
		},
	})
	store.Set(Card{
		Key: "ns/grafana/0", Group: "Monitoring", ServiceName: "Grafana",
		Fields: []Field{
			{Label: labelStatus, Value: statusHealthy, Highlight: HighlightGood},
			{Label: "Version", Value: testGrafanaVersion},
		},
	})
	store.Set(Card{
		Key: "ns/prometheus/0", Group: "Monitoring", ServiceName: "Prometheus",
		Fields: []Field{{Label: labelStatus, Value: statusHealthy, Highlight: HighlightGood}},
	})
	store.Set(Card{
		Key: "ns/broken/0", Group: "Media", ServiceName: testBrokenServiceName,
		Err: testUnreachableErr,
	})

	objs := []client.Object{
		&pagev1alpha1.InfoWidget{
			ObjectMeta: metav1.ObjectMeta{Name: "greet", Namespace: testNamespace},
			Spec: pagev1alpha1.InfoWidgetSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
				Widgets: []pagev1alpha1.InfoWidgetEntry{{
					Type:    "greeting",
					Options: &apiextensionsv1.JSON{Raw: []byte(`{"text":"Good afternoon"}`)},
				}},
			},
		},
		&pagev1alpha1.InfoWidget{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: testNamespace},
			Spec: pagev1alpha1.InfoWidgetSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
				Widgets:      []pagev1alpha1.InfoWidgetEntry{{Type: "kubemetrics"}},
			},
		},
		&pagev1alpha1.Bookmark{
			ObjectMeta: metav1.ObjectMeta{Name: "bookmarks", Namespace: testNamespace},
			Spec: pagev1alpha1.BookmarkSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
				Bookmarks: []pagev1alpha1.BookmarkEntry{
					{Name: "Github", Group: "Links", Href: "https://github.com"},
					{Name: "Docs", Group: "Links", Href: "https://example.com/docs"},
				},
			},
		},
		&pagev1alpha1.DashboardStyle{
			ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
			Spec: pagev1alpha1.DashboardStyleSpec{
				DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
				Title:        strPtr("Homelab"),
				Theme:        strPtr(theme),
				Color:        strPtr("slate"),
				HeaderStyle:  strPtr(headerStyle),
			},
		},
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	// A refresh interval far longer than any test run keeps htmx's interval
	// poll from ever firing (and possibly swapping the DOM mid-screenshot);
	// this fixture's Store content never changes anyway, so there's nothing
	// for a poll to legitimately pick up.
	srv := &Server{
		Store: store, Reader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
		RefreshSeconds: 3600,
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	httpSrv := &http.Server{Handler: srv.Routes()}
	serveErrCh := make(chan error, 1)
	go func() { serveErrCh <- httpSrv.Serve(ln) }()

	baseURL = fmt.Sprintf("http://%s", ln.Addr().String())
	shutdown = func() {
		_ = httpSrv.Close()
		if err := <-serveErrCh; err != nil && err != http.ErrServerClosed {
			t.Errorf("http server error: %v", err)
		}
	}
	return baseURL, shutdown
}

// visualChromeExecPath, when set (e.g. by CI via CHROME_PATH after
// installing a version-pinned Chrome — see .github/workflows/visual.yml),
// points chromedp at that specific binary instead of whatever chromedp
// finds on PATH, so the goldens under testdata/visual/ are diffed against a
// known, reproducible renderer rather than whatever Chrome version happens
// to be preinstalled on a given runner/dev machine.
func visualChromeExecPath() (string, bool) {
	p := os.Getenv("CHROME_PATH")
	return p, p != ""
}

// captureScreenshot navigates to baseURL at the given viewport and returns a
// full-page PNG screenshot, after waiting for the card grid to render, for
// web fonts to finish loading (so the very first frame's fallback-font
// layout never sneaks into the capture), and for consecutive captures to
// stabilize (see the loop below).
func captureScreenshot(t *testing.T, baseURL string, width, height int64) []byte {
	t.Helper()

	allocOpts := chromedp.DefaultExecAllocatorOptions[:]
	// GitHub Actions runners (and many container-based CI/dev environments)
	// don't have unprivileged user namespaces available, which Chrome's
	// zygote/sandbox setup requires — without this flag Chrome refuses to
	// start at all ("No usable sandbox!"). Safe here specifically because
	// this test only ever navigates to a same-process httptest server
	// serving this repo's own fixture data (startVisualServer) — never
	// third-party or user-supplied content.
	allocOpts = append(allocOpts, chromedp.NoSandbox)
	if execPath, ok := visualChromeExecPath(); ok {
		allocOpts = append(allocOpts, chromedp.ExecPath(execPath))
	}
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), allocOpts...)
	defer cancelAlloc()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	ctx, cancelTimeout := context.WithTimeout(ctx, 30*time.Second)
	defer cancelTimeout()

	var fontsReady bool
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(width, height),
		chromedp.Navigate(baseURL),
		chromedp.WaitVisible(`#cards`, chromedp.ByID),
		chromedp.Evaluate(`document.fonts.ready.then(() => true)`, &fontsReady,
			func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) }),
	); err != nil {
		t.Fatalf("loading page: %v", err)
	}

	// FullScreenshot resizes the viewport to the full document height and
	// captures after that resize; a single capture right after
	// fonts.ready intermittently races that resize's own reflow/repaint —
	// below-the-fold content (groups/cards past the original viewport)
	// captures as blank rectangles, observed directly by diffing
	// consecutive local runs, not a theoretical concern. Rather than a
	// blind fixed sleep (which only shrinks the failure window, and by an
	// environment-dependent amount), take consecutive captures until two
	// in a row come back byte-identical — that's the actual signal
	// rendering has settled, on however many frames that takes locally.
	const stableScreenshotAttempts = 10
	var buf, prev []byte
	for range stableScreenshotAttempts {
		if err := chromedp.Run(ctx, chromedp.FullScreenshot(&buf, 100)); err != nil {
			t.Fatalf("capturing screenshot: %v", err)
		}
		if prev != nil && bytes.Equal(buf, prev) {
			return buf
		}
		prev = buf
	}
	t.Logf("screenshot never stabilized across %d attempts; using the last capture", stableScreenshotAttempts)
	return buf
}

// compareVisualGolden diffs got against testdata/visual/<name>.golden.png,
// or (re)writes it when UPDATE_GOLDEN=1 — mirroring golden_test.go's HTML
// golden convention, one image instead of one HTML string. A dimension
// mismatch fails immediately (it always means a gross layout change, e.g.
// the viewport-meta regression #119 fixed, not a rendering nuance);
// otherwise pixelColorTolerance/maxMismatchRatio absorb minor
// anti-aliasing differences. On failure the actual capture is written
// alongside the golden as "<name>.got.png" for local review/diffing.
func compareVisualGolden(t *testing.T, name string, got []byte) {
	t.Helper()

	goldenPath := filepath.Join("testdata", "visual", name+".golden.png")

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated golden file: %s", goldenPath)
		return
	}

	wantFile, err := os.Open(goldenPath) //nolint:gosec // fixed testdata path built from a compile-time literal
	if err != nil {
		t.Fatalf("reading golden file %s: %v (run with UPDATE_GOLDEN=1 to create)", goldenPath, err)
	}
	defer func() { _ = wantFile.Close() }()

	want, err := png.Decode(wantFile)
	if err != nil {
		t.Fatalf("decoding golden PNG %s: %v", goldenPath, err)
	}
	gotImg, err := png.Decode(bytes.NewReader(got))
	if err != nil {
		t.Fatalf("decoding captured PNG: %v", err)
	}

	if want.Bounds() != gotImg.Bounds() {
		writeGotArtifact(t, name, got)
		t.Fatalf("image size = %v, want %v (run with UPDATE_GOLDEN=1 to update if this is intentional)", gotImg.Bounds(), want.Bounds())
	}

	mismatched, total := diffPixels(want, gotImg)
	if ratio := float64(mismatched) / float64(total); ratio > maxMismatchRatio {
		writeGotArtifact(t, name, got)
		t.Fatalf("%d/%d pixels (%.2f%%) differ beyond tolerance, want <= %.2f%% (run with UPDATE_GOLDEN=1 to update if this is intentional; see %s.got.png)",
			mismatched, total, ratio*100, maxMismatchRatio*100, filepath.Join("testdata", "visual", name))
	}
}

// diffPixels counts pixels whose combined RGBA channel delta exceeds
// pixelColorTolerance.
func diffPixels(want, got image.Image) (mismatched, total int) {
	b := want.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			total++
			wr, wg, wb, wa := want.At(x, y).RGBA()
			gr, gg, gb, ga := got.At(x, y).RGBA()
			delta := absInt32(int32(wr)-int32(gr)) + absInt32(int32(wg)-int32(gg)) +
				absInt32(int32(wb)-int32(gb)) + absInt32(int32(wa)-int32(ga))
			// RGBA() returns 16-bit-per-channel values; scale the
			// 8-bit-oriented pixelColorTolerance up to match.
			if delta > pixelColorTolerance*257 {
				mismatched++
			}
		}
	}
	return mismatched, total
}

func absInt32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

func writeGotArtifact(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", "visual", name+".got.png")
	if err := os.WriteFile(path, got, 0o644); err != nil {
		t.Logf("writing diff artifact %s: %v", path, err)
	}
}

func intPtr(v int) *int       { return &v }
func strPtr(v string) *string { return &v }
