//go:build browser

package dashboard

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// TestBrowserTabAndGroupStateSurviveFragmentSwap is the regression test for
// the fragment-swap flicker described in docs/design (tab selection and
// collapsed-group state visibly reverting for one frame on every
// htmx-triggered /fragment refresh, because the server always renders tab 0
// active and every group open — see index.templ's htmx:beforeSwap listener).
//
// The fixture needs at least two tabs for tab-1 selection to mean anything
// (a single-group fixture never renders a tab bar at all), and the bug
// doesn't depend on the underlying data changing between polls — the server
// re-renders tab-0-active/all-groups-open on every /fragment response
// regardless — so this drives a manual htmx.ajax refetch of identical data,
// exactly like the production SSE/interval/refocus paths do (see index.templ),
// rather than mutating the Store.
func TestBrowserTabAndGroupStateSurviveFragmentSwap(t *testing.T) {
	store := NewStore()
	store.Set(Card{Key: "ns/grafana/0", Group: "Monitoring", ServiceName: "Grafana",
		Fields: []Field{{Label: labelStatus, Value: statusHealthy}}})
	store.Set(Card{Key: "ns/plex/0", Group: testGroupMedia, ServiceName: testMultiEntryNamePlex,
		Fields: []Field{{Label: labelStatus, Value: statusHealthy}}})

	style := &pagev1alpha1.DashboardStyle{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardStyleSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Layout: []pagev1alpha1.LayoutTabSpec{
				{Name: "Infra", Groups: []pagev1alpha1.LayoutGroupSpec{{Name: "Monitoring"}}},
				{Name: "Apps", Groups: []pagev1alpha1.LayoutGroupSpec{{Name: testGroupMedia}}},
			},
		},
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(style).Build()
	srv := &Server{
		Store: store, Reader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
		// Long enough that htmx's own interval trigger never fires during the
		// test — the manual htmx.ajax call below is what drives the refresh
		// under test, not the interval poll.
		RefreshSeconds: 3600,
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()

	httpSrv := &http.Server{Handler: srv.Routes()}
	serveErrCh := make(chan error, 1)
	go func() { serveErrCh <- httpSrv.Serve(ln) }()
	defer func() {
		_ = httpSrv.Close()
		if err := <-serveErrCh; err != nil && err != http.ErrServerClosed {
			t.Errorf("http server error: %v", err)
		}
	}()

	baseURL := fmt.Sprintf("http://%s", ln.Addr().String())

	ctx, cancel := chromedp.NewContext(t.Context())
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(ctx, 30*time.Second)
	defer cancelTimeout()

	// Select tab 1 ("Apps"/Media) and collapse the "Monitoring" group (it's
	// visible from tab 0, before switching), then install a MutationObserver
	// with attributeOldValue on both tab panels' class attribute and on the
	// group's open attribute.
	//
	// A plain "did any mutation fire" check is not enough: idiomorph/htmx can
	// legitimately call setAttribute/classList with a value that ends up
	// identical to the live one (e.g. re-asserting "tab-panel hidden" on a
	// panel that was already hidden), which fires a MutationRecord without
	// ever putting the DOM in a wrong state. What actually matters is whether
	// the attribute's value was ever wrong at any point in time, which
	// attributeOldValue lets us reconstruct: each record's oldValue is the
	// value immediately before that mutation, so record[i].oldValue is
	// exactly the value the DOM had right after record[i-1] applied (for the
	// same target/attribute). A record whose oldValue shows the wrong state
	// (panel-0 missing "hidden", panel-1 having "hidden", or the group's
	// "open" attribute present) proves that wrong state was live in the DOM
	// for at least one moment, however briefly — the one-frame flash this
	// test exists to catch — even if a later mutation or the afterSettle
	// backstop corrected it before this callback ran. This is more reliable
	// than a PerformanceObserver('layout-shift')/rAF sampler, which can miss
	// a one-frame transient entirely in headless Chrome.
	var setupErr string
	err = chromedp.Run(ctx,
		chromedp.Navigate(baseURL),
		chromedp.WaitVisible(`#cards`, chromedp.ByID),
		chromedp.Click(`#tab-1`, chromedp.ByID),
		chromedp.Evaluate(`
			(function () {
				window.__panelMutations = [];
				window.__groupMutations = [];
				const panel0 = document.getElementById("panel-0");
				const panel1 = document.getElementById("panel-1");
				if (!panel0 || !panel1) return "missing tab panels";
				const group = document.querySelector('#cards details.group[data-group-name]');
				if (group) group.open = false;
				const panelObserver = new MutationObserver(function (records) {
					records.forEach(function (r) {
						window.__panelMutations.push({ id: r.target.id, oldValue: r.oldValue });
					});
				});
				panelObserver.observe(panel0, { attributes: true, attributeFilter: ["class"], attributeOldValue: true });
				panelObserver.observe(panel1, { attributes: true, attributeFilter: ["class"], attributeOldValue: true });
				if (group) {
					const groupObserver = new MutationObserver(function (records) {
						records.forEach(function (r) {
							window.__groupMutations.push({ oldValue: r.oldValue });
						});
					});
					groupObserver.observe(group, { attributes: true, attributeFilter: ["open"], attributeOldValue: true });
				}
				return "";
			})()
		`, &setupErr),
	)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if setupErr != "" {
		t.Fatalf("setup: %s", setupErr)
	}

	// Trigger a refresh the same way production does: an SSE-triggered (or
	// interval-triggered) htmx.ajax call against #cards with morph:innerHTML.
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`htmx.ajax("GET", "/fragment", { source: "#cards", target: "#cards", swap: "morph:innerHTML" })`, nil),
	)
	if err != nil {
		t.Fatalf("triggering refresh: %v", err)
	}

	// Give the swap/settle sequence (measured in the bug report at ~20ms) a
	// generous margin past which any wrong-tab/reopened-group state, if it
	// ever appeared, would already have been recorded by the observers above.
	if err := chromedp.Run(ctx, chromedp.Sleep(500*time.Millisecond)); err != nil {
		t.Fatalf("waiting past settle window: %v", err)
	}

	type mutation struct {
		ID string `json:"id"`
		// OldValue is a pointer because MutationObserver reports null (not "")
		// for an attribute that was absent before the mutation — and a bare
		// boolean attribute like <details open> has "" as its present value,
		// so nil vs "" is exactly the closed-vs-open distinction below.
		OldValue *string `json:"oldValue"`
	}
	var panelMutations, groupMutations []mutation
	var panel0Hidden, panel1Hidden, groupOpen bool
	err = chromedp.Run(ctx,
		chromedp.Evaluate(`window.__panelMutations`, &panelMutations),
		chromedp.Evaluate(`window.__groupMutations`, &groupMutations),
		chromedp.Evaluate(`document.getElementById("panel-0").classList.contains("hidden")`, &panel0Hidden),
		chromedp.Evaluate(`document.getElementById("panel-1").classList.contains("hidden")`, &panel1Hidden),
		chromedp.Evaluate(`(function(){ const g = document.querySelector('#cards details.group[data-group-name]'); return g ? g.open : false; })()`, &groupOpen),
	)
	if err != nil {
		t.Fatalf("reading final state: %v", err)
	}

	// Every recorded oldValue is a state the DOM actually held, however
	// briefly. #panel-0 (not the selected tab) must never have been observed
	// without "hidden"; #panel-1 (the selected tab) must never have been
	// observed with "hidden".
	for _, m := range panelMutations {
		var oldClass string
		if m.OldValue != nil {
			oldClass = *m.OldValue
		}
		hadHidden := containsToken(oldClass, "hidden")
		switch m.ID {
		case "panel-0":
			if !hadHidden {
				t.Errorf("#panel-0 was observed visible (class=%q) at some point during the refresh, want always hidden while tab 1 is selected", oldClass)
			}
		case "panel-1":
			if hadHidden {
				t.Errorf("#panel-1 was observed hidden (class=%q) at some point during the refresh, want always visible while tab 1 is selected", oldClass)
			}
		}
	}
	// A closed <details>'s "open" attribute is absent, so oldValue is null
	// (nil here) when it was in fact closed; a non-nil oldValue — "" for a
	// bare open attribute — means the attribute was present, i.e. the group
	// had flashed open.
	for _, m := range groupMutations {
		if m.OldValue != nil {
			t.Errorf("collapsed group was observed open (open attribute=%q) at some point during the refresh, want it to stay collapsed", *m.OldValue)
		}
	}

	if panel0Hidden != true {
		t.Errorf("#panel-0 hidden = %v, want true (tab 1 should stay selected across the refresh)", panel0Hidden)
	}
	if panel1Hidden != false {
		t.Errorf("#panel-1 hidden = %v, want false (tab 1 should stay selected across the refresh)", panel1Hidden)
	}
	if groupOpen != false {
		t.Errorf("collapsed group open = %v, want false (collapsed group should not reopen across the refresh)", groupOpen)
	}
}

// containsToken reports whether class (a space-separated class attribute
// value) contains the exact token want.
func containsToken(class, want string) bool {
	return slices.Contains(strings.Fields(class), want)
}
