//go:build browser

package dashboard

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

func TestBrowserHTMXPolling(t *testing.T) {
	store := NewStore()
	store.Set(Card{
		Key: "ns/grafana/0", Group: "Monitoring", ServiceName: "Grafana",
		Fields: []Field{{Label: "Status", Value: "Initial"}},
	})

	style := &pagev1alpha1.DashboardStyle{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardStyleSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
		},
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(style).Build()
	srv := &Server{
		Store:         store,
		Reader:        cl,
		Namespace:     testNamespace,
		DashboardName: testDashboardName,
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	httpSrv := &http.Server{Handler: srv.Routes()}
	serveErrCh := make(chan error, 1)
	go func() { serveErrCh <- httpSrv.Serve(ln) }()
	defer func() {
		httpSrv.Close()
		if err := <-serveErrCh; err != nil && err != http.ErrServerClosed {
			t.Errorf("http server error: %v", err)
		}
	}()

	baseURL := fmt.Sprintf("http://%s", ln.Addr().String())

	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()

	ctx, cancelTimeout := context.WithTimeout(ctx, 30*time.Second)
	defer cancelTimeout()

	var initialHTML string
	err = chromedp.Run(ctx,
		chromedp.Navigate(baseURL),
		chromedp.WaitVisible(`#cards`, chromedp.ByID),
		chromedp.InnerHTML(`#cards`, &initialHTML, chromedp.ByID),
	)
	if err != nil {
		t.Fatalf("initial page load: %v", err)
	}
	if initialHTML == "" {
		t.Fatal("initial #cards content is empty")
	}

	store.Set(Card{
		Key: "ns/grafana/0", Group: "Monitoring", ServiceName: "Grafana",
		Fields: []Field{{Label: "Status", Value: "Updated"}},
	})

	// Poll until the htmx fragment swap picks up the change, rather than a
	// fixed sleep (the poll interval and htmx timing can both vary under CI
	// load, and a fixed sleep either wastes time or flakes).
	var updatedHTML string
	deadline := time.Now().Add(15 * time.Second)
	for {
		err = chromedp.Run(ctx, chromedp.InnerHTML(`#cards`, &updatedHTML, chromedp.ByID))
		if err != nil {
			t.Fatalf("reading updated content: %v", err)
		}
		if updatedHTML != initialHTML {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("htmx polling did not update the fragment content within 15s")
		}
		time.Sleep(200 * time.Millisecond)
	}

	// window.performance.navigation is deprecated (Navigation Timing Level
	// 1); use the Level 2 PerformanceNavigationTiming entries instead. A
	// full-page navigation/reload would show up as more than one "navigate"
	// entry or an entry of a different type, whereas htmx fragment swaps
	// never touch the navigation timeline at all.
	var navEntryTypes []string
	err = chromedp.Run(ctx,
		chromedp.Evaluate(
			`performance.getEntriesByType('navigation').map(e => e.type)`,
			&navEntryTypes,
		),
	)
	if err != nil {
		t.Fatalf("checking navigation entries: %v", err)
	}
	if len(navEntryTypes) != 1 || navEntryTypes[0] != "navigate" {
		t.Errorf("navigation entries = %v, want exactly one %q entry (extra/other entries indicate a full-page navigation)",
			navEntryTypes, "navigate")
	}
}
