package dashboard

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

const (
	testNamespace               = "ns"
	testDashboardName           = "main"
	testGroup                   = "Monitoring"
	testServiceName             = "Prometheus"
	testRenamedServiceName      = "Renamed"
	testSvcAName                = "Svc A"
	testCardKeyA                = "ns/a/0"
	testWidgetType              = "prometheus"
	testSecretField             = "token"
	testBookmarkGroup           = "Reading"
	testOtherGroup              = "Other"
	testTab1                    = "Tab1"
	testTab2                    = "Tab2"
	testInfraTab                = "Infrastructure"
	testStyleRow                = "row"
	testColor                   = "blue"
	testDoesNotExist            = "does-not-exist"
	testMultiEntryNamePlex      = "Plex"
	testMultiEntryNameStash     = "Stash"
	testMultiEntryNameMonitored = "Monitored"
	testSecretMissingName       = "missing"
	testPlexSecretRefName       = "plex-secret"
	testWidgetTypePlex          = "plex"

	// Nested-group test fixtures (docs/design/nested-groups.md): a
	// "Media" root with "Movies"/"TV" subgroups, and one 3-level path
	// exercising the CRD's max depth.
	testGroupMedia       = "Media"
	testGroupMediaMovies = "Media/Movies"
	testGroupMediaTV     = "Media/TV"
	testGroupA           = "A"
	testGroupABC         = "A/B/C"
	testNameMovies       = "Movies"
	testNameRadarr       = "Radarr"
	testNameSonarr       = "Sonarr"

	// testEphemeralAddr requests an OS-assigned port, for tests that don't
	// care which one they get.
	testEphemeralAddr = "127.0.0.1:0"

	// Combined HTTP + pod monitor test fixtures (docs/design/combined-monitor.md).
	testMyAppLabelValue        = "myapp"
	testCombinedServiceName    = "Combined"
	testCustomSelectorLabelKey = "custom-selector"
	testCustomSelectorValue    = "custom-value"
	testPodReadyOneOfOne       = "1/1 ready"

	// Preview-mode internalUrl-ignoring test fixtures.
	testInternalURLInvalid = "http://internal.invalid/"
)

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := pagev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := gatewayv1.Install(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}

func TestPollerPollOnce(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"activeTargets":[{"health":"up"}]}}`))
	}))
	defer srv.Close()

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: testSecretName, Namespace: testNamespace},
		Data:       map[string][]byte{testSecretField: []byte("s3cr3t")},
	}

	url := srv.URL
	href := "https://prometheus.example.com"
	entry := &pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: "prom", Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
			Group:        testGroup,
			Services: []pagev1alpha1.ServiceEntry{{
				Name: testServiceName,
				Href: &href,
				Widgets: []pagev1alpha1.ServiceWidget{
					{
						Type: testWidgetType,
						URL:  &url,
						Secrets: map[string]pagev1alpha1.SecretValueSource{
							testSecretField: {SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: testSecretName},
								Key:                  testSecretField,
							}},
						},
					},
				},
			}},
		},
	}

	otherDashboard := &pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: &pagev1alpha1.DashboardRef{Name: "not-main"},
			Group:        testOtherGroup,
			Services: []pagev1alpha1.ServiceEntry{{
				Name:    "Skip me",
				Widgets: []pagev1alpha1.ServiceWidget{{Type: testWidgetType, URL: &url}},
			}},
		},
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret, entry, otherDashboard).Build()

	store := NewStore()
	p := &Poller{
		Reader:        cl,
		SecretReader:  cl,
		Namespace:     testNamespace,
		DashboardName: testDashboardName,
		Interval:      time.Hour,
		HTTPClient:    srv.Client(),
		Store:         store,
	}

	p.pollOnce(t.Context())

	cards := store.Snapshot()
	if len(cards) != 1 {
		t.Fatalf("Snapshot() returned %d cards, want 1 (bound only to DashboardRef %q)", len(cards), testDashboardName)
	}

	card := cards[0]
	if card.Err != "" {
		t.Fatalf("card.Err = %q, want empty", card.Err)
	}
	if card.ServiceName != testServiceName || card.Group != testGroup {
		t.Errorf("card = %+v, want ServiceName=Prometheus Group=Monitoring", card)
	}
	if card.Href != href {
		t.Errorf("card.Href = %q, want %q", card.Href, href)
	}
	wantFields := []Field{{Label: labelStatus, Value: statusHealthy}, {Label: labelTargetsUp, Value: "1 / 1"}}
	if len(card.Fields) != len(wantFields) || card.Fields[0] != wantFields[0] || card.Fields[1] != wantFields[1] {
		t.Errorf("card.Fields = %+v, want %+v", card.Fields, wantFields)
	}
}

// TestPollerPollOnceBroadcastsCompletion verifies pollOnce publishes to
// Broadcast (when set) after every cycle, win or lose — Server.handleEvents
// relies on this to know when to recheck whether the rendered fragment/
// header content actually changed, so the signal must fire unconditionally
// rather than only on a cycle that touched the Store.
func TestPollerPollOnceBroadcastsCompletion(t *testing.T) {
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()

	broadcast := NewBroadcaster()
	ch, ok := broadcast.Subscribe()
	if !ok {
		t.Fatal("Subscribe() ok = false, want true")
	}
	p := &Poller{
		Reader:        cl,
		SecretReader:  cl,
		Namespace:     testNamespace,
		DashboardName: testDashboardName,
		Interval:      time.Hour,
		HTTPClient:    http.DefaultClient,
		Store:         NewStore(),
		Broadcast:     broadcast,
	}

	p.pollOnce(t.Context())

	select {
	case h := <-ch:
		wantFragment, wantHeader := currentHashes(t.Context(), p.Reader, p.Namespace, p.DashboardName, p.Store)
		if h.fragment != wantFragment || h.header != wantHeader {
			t.Errorf("published %+v, want {%q %q} (currentHashes computed independently against the same Store/Reader)", h, wantFragment, wantHeader)
		}
	default:
		t.Error("pollOnce did not publish to Broadcast")
	}
}

// TestPollerPollOnceNilBroadcastIsFine verifies pollOnce tolerates a nil
// Broadcast (the default for a Poller built without SSE support wired up,
// e.g. most other tests in this file).
func TestPollerPollOnceNilBroadcastIsFine(t *testing.T) {
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()

	p := &Poller{
		Reader:        cl,
		SecretReader:  cl,
		Namespace:     testNamespace,
		DashboardName: testDashboardName,
		Interval:      time.Hour,
		HTTPClient:    http.DefaultClient,
		Store:         NewStore(),
	}

	p.pollOnce(t.Context())
}

// TestPollerPollOnceToleratesBroadcastWithNoSubscribers verifies pollOnce
// runs cleanly when Broadcast is set but HasSubscribers() is false — the
// path the perf optimization in pollOnce takes to skip currentHashes (two
// full templ renders plus LoadSite) and Publish when no SSE client is
// connected. currentHashes/Publish being skipped isn't independently
// observable through Poller's public surface (Publish is itself a no-op
// with zero subscribers), so this test exercises the code path rather than
// asserting on the skip directly; TestBroadcasterHasSubscribers in
// sse_test.go covers the predicate pollOnce gates on.
func TestPollerPollOnceToleratesBroadcastWithNoSubscribers(t *testing.T) {
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()

	broadcast := NewBroadcaster()
	p := &Poller{
		Reader:        cl,
		SecretReader:  cl,
		Namespace:     testNamespace,
		DashboardName: testDashboardName,
		Interval:      time.Hour,
		HTTPClient:    http.DefaultClient,
		Store:         NewStore(),
		Broadcast:     broadcast,
	}

	p.pollOnce(t.Context())

	if broadcast.HasSubscribers() {
		t.Fatal("HasSubscribers() = true, want false (test setup never subscribed)")
	}
}

// TestPollerMultiCardFormProducesPerEntryCards exercises a multi-entry
// ServiceCard: one ServiceCard object with three entries — one
// with a widget and its own group, one with a widget that falls back to
// spec.group, and one monitor-only entry with no widget — must produce one
// Card per entry (per widget, for the widget entries), each keyed by
// namespace/name/entryIndex[/widgetIndex|monitor], with the second entry's
// Group defaulted from spec.group.
func TestPollerMultiCardFormProducesPerEntryCards(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"activeTargets":[{"health":"up"}]}}`))
	}))
	defer srv.Close()

	monSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer monSrv.Close()

	url := srv.URL
	entry := &pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: "multi", Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
			Group:        testGroup,
			Services: []pagev1alpha1.ServiceEntry{
				{
					Name:  testMultiEntryNamePlex,
					Group: testOtherGroup,
					Widgets: []pagev1alpha1.ServiceWidget{
						{Type: testWidgetType, URL: &url},
					},
				},
				{
					Name: testMultiEntryNameStash, // no own Group: falls back to spec.group (testGroup)
					Widgets: []pagev1alpha1.ServiceWidget{
						{Type: testWidgetType, URL: &url},
					},
				},
				{
					Name:    testMultiEntryNameMonitored,
					Monitor: &monSrv.URL,
				},
			},
		},
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(entry).Build()

	store := NewStore()
	p := &Poller{
		Reader: cl, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
		Interval: time.Hour, HTTPClient: srv.Client(), Store: store,
	}
	p.pollOnce(t.Context())

	cards := store.Snapshot()
	if len(cards) != 3 {
		t.Fatalf("Snapshot() = %d cards, want 3 (one per entry)", len(cards))
	}

	byKey := map[string]Card{}
	for _, c := range cards {
		byKey[c.Key] = c
	}

	plex, ok := byKey["ns/multi/0/0"]
	if !ok {
		t.Fatalf("no card for key ns/multi/0/0; got keys %v", byKey)
	}
	if plex.ServiceName != testMultiEntryNamePlex || plex.Group != testOtherGroup {
		t.Errorf("plex card = %+v, want ServiceName=Plex Group=%s (own group)", plex, testOtherGroup)
	}

	stash, ok := byKey["ns/multi/1/0"]
	if !ok {
		t.Fatalf("no card for key ns/multi/1/0; got keys %v", byKey)
	}
	if stash.ServiceName != testMultiEntryNameStash || stash.Group != testGroup {
		t.Errorf("stash card = %+v, want ServiceName=Stash Group=%s (defaulted from spec.group)", stash, testGroup)
	}

	monitored, ok := byKey["ns/multi/2/monitor"]
	if !ok {
		t.Fatalf("no card for key ns/multi/2/monitor; got keys %v", byKey)
	}
	if monitored.ServiceName != testMultiEntryNameMonitored || monitored.Status != "Up" {
		t.Errorf("monitored card = %+v, want ServiceName=Monitored Status=Up", monitored)
	}

	// A second poll cycle after removing the "Stash" entry must prune its
	// card from Store, the same way a deleted ServiceCard's cards are
	// pruned — Store.Prune has no per-entry special casing.
	entry.Spec.Services = []pagev1alpha1.ServiceEntry{entry.Spec.Services[0], entry.Spec.Services[2]}
	if err := cl.Update(t.Context(), entry); err != nil {
		t.Fatalf("updating ServiceCard: %v", err)
	}
	p.pollOnce(t.Context())

	cards = store.Snapshot()
	if len(cards) != 2 {
		t.Fatalf("Snapshot() after removing an entry = %d cards, want 2", len(cards))
	}
	for _, c := range cards {
		if c.ServiceName == testMultiEntryNameStash {
			t.Errorf("card for removed entry Stash still present: %+v", c)
		}
	}
}

// TestPollerRunPollsOnIntervalAndStopsOnCancel exercises Run's previously-0%-
// covered ticker loop deterministically: synctest's fake clock advances only
// once every goroutine in the bubble is durably blocked, so sleeping exactly
// p.Interval is guaranteed to let the ticker fire (no real-time flakiness).
func TestPollerRunPollsOnIntervalAndStopsOnCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		scheme := testScheme(t)
		cl := fake.NewClientBuilder().WithScheme(scheme).Build()

		var listCalls atomic.Int32
		counting := errInjectingReader{
			Reader: cl,
			failList: func(client.ObjectList) bool {
				listCalls.Add(1)
				return false
			},
		}

		store := NewStore()
		ctx, cancel := context.WithCancel(t.Context())
		p := &Poller{
			Reader: counting, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
			Interval: 10 * time.Second, HTTPClient: http.DefaultClient, Store: store,
		}

		done := make(chan struct{})
		go func() {
			p.Run(ctx)
			close(done)
		}()

		synctest.Wait()
		// pollOnce Gets the Dashboard (site-wide defaults, not counted
		// here) and lists Dashboards (namespaceDashboardCount, for
		// defaulting an unset dashboardRef), ServiceCards, and InfoWidgets
		// once each.
		if n := listCalls.Load(); n != 3 {
			t.Fatalf("after the immediate poll, List was called %d times, want 3", n)
		}

		time.Sleep(10 * time.Second)
		synctest.Wait()
		if n := listCalls.Load(); n != 6 {
			t.Fatalf("after one Interval, List was called %d times, want 6 (one more poll)", n)
		}

		cancel()
		synctest.Wait()
		select {
		case <-done:
		default:
			t.Fatal("Run did not return after ctx was canceled")
		}
	})
}

func TestPollerPollOnceListEntriesErrorLeavesStoreUntouched(t *testing.T) {
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	failing := errInjectingReader{
		Reader: cl,
		failList: func(list client.ObjectList) bool {
			_, ok := list.(*pagev1alpha1.ServiceCardList)
			return ok
		},
	}

	store := NewStore()
	store.Set(Card{Key: "stale", ServiceName: "Stale"})

	p := &Poller{
		Reader: failing, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
		Interval: time.Hour, HTTPClient: http.DefaultClient, Store: store,
	}
	p.pollOnce(t.Context())

	cards := store.Snapshot()
	if len(cards) != 1 || cards[0].Key != "stale" {
		t.Fatalf("Snapshot() = %+v, want the stale card untouched (pollOnce returns before pruning on a ServiceCard List error)", cards)
	}
}

func TestPollerPollOnceListInfoWidgetsErrorStillPolicsEntriesAndPrunes(t *testing.T) {
	url := testUnreachableAddr
	entry := &pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: testSvcName, Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
			Group:        "G",
			Services: []pagev1alpha1.ServiceEntry{{
				Name:    testSvcDisplayName,
				Widgets: []pagev1alpha1.ServiceWidget{{Type: testWidgetType, URL: &url}},
			}},
		},
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(entry).Build()
	failing := errInjectingReader{
		Reader: cl,
		failList: func(list client.ObjectList) bool {
			_, ok := list.(*pagev1alpha1.InfoWidgetList)
			return ok
		},
	}

	store := NewStore()
	store.Set(Card{Key: "header/stale", Header: true, ServiceName: "StaleHeader"})

	p := &Poller{
		Reader: failing, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
		Interval: time.Hour, HTTPClient: http.DefaultClient, Store: store,
	}
	p.pollOnce(t.Context())

	cards := store.Snapshot()
	if len(cards) != 1 || cards[0].Key != "ns/svc/0/0" {
		t.Fatalf("Snapshot() = %+v, want only the ServiceCard card: an InfoWidget List error logs and continues "+
			"(rather than returning early), so the stale header card should still be pruned", cards)
	}
}

func TestPollerUnsupportedWidgetType(t *testing.T) {
	url := testExampleURL
	entry := &pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: "mystery", Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
			Group:        "G",
			Services: []pagev1alpha1.ServiceEntry{{
				Name:    "Mystery",
				Widgets: []pagev1alpha1.ServiceWidget{{Type: testDoesNotExist, URL: &url}},
			}},
		},
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(entry).Build()

	store := NewStore()
	p := &Poller{
		Reader: cl, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
		Interval: time.Hour, HTTPClient: http.DefaultClient, Store: store,
	}
	p.pollOnce(t.Context())

	cards := store.Snapshot()
	if len(cards) != 1 || cards[0].Err == "" {
		t.Fatalf("Snapshot() = %+v, want one card with a non-empty Err", cards)
	}
}

func TestPollerMonitorOnlyEntry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	style := testStatusBasic
	entry := &pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: "mon", Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
			Group:        "G",
			Services: []pagev1alpha1.ServiceEntry{{
				Name:        "Monitored",
				Monitor:     &srv.URL,
				StatusStyle: &style,
			}},
		},
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(entry).Build()
	store := NewStore()
	p := &Poller{
		Reader: cl, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
		Interval: time.Hour, HTTPClient: srv.Client(), Store: store,
	}
	p.pollOnce(t.Context())

	cards := store.Snapshot()
	if len(cards) != 1 {
		t.Fatalf("Snapshot() = %d cards, want 1 monitor-only card", len(cards))
	}
	c := cards[0]
	if c.Status != "Up" {
		t.Errorf("card.Status = %q, want Up", c.Status)
	}
	if c.StatusStyle != testStatusBasic {
		t.Errorf("card.StatusStyle = %q, want basic", c.StatusStyle)
	}
	if c.WidgetType != "" {
		t.Errorf("card.WidgetType = %q, want empty for monitor-only", c.WidgetType)
	}
}

func TestPollerMonitorURLOnlyEntry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	entry := pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: "mon-url", Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
			Group:        "G",
			Services: []pagev1alpha1.ServiceEntry{{
				Name:    "Monitored",
				Monitor: &srv.URL,
			}},
		},
	}

	p := &Poller{HTTPClient: srv.Client()}
	m := p.monitor(t.Context(), entry.Namespace, entry.Name, entry.Spec.Entries()[0], statusStyleDot, nil, defaultClusterDomain, func(string, string) {})
	if m.status != "Up" {
		t.Errorf("monitor(Monitor) status = %q, want Up", m.status)
	}
	if m.statusStyle != statusStyleDot {
		t.Errorf("monitor(Monitor) style = %q, want default dot", m.statusStyle)
	}
	if m.latency == "" {
		t.Errorf("monitor(Monitor) latency = empty, want non-empty")
	}
}

// TestPollerMonitorSelfResolution covers the "monitor: self" sentinel: the
// probe goes to the entry's own base URL — internalUrl when set (the probe
// runs from the pod, so the in-cluster URL wins), else href — and an entry
// whose base URL resolves empty probes nothing at all.
func TestPollerMonitorSelfResolution(t *testing.T) {
	probed := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case probed <- r.Host:
		default:
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	unreachable := "http://browser-facing.invalid/"
	monitorSelf := pagev1alpha1.MonitorSelf

	t.Run("internalUrl wins over href", func(t *testing.T) {
		se := pagev1alpha1.ServiceEntry{
			Name:        testSvcDisplayName,
			Href:        &unreachable,
			InternalURL: &srv.URL,
			Monitor:     &monitorSelf,
		}
		p := &Poller{HTTPClient: srv.Client()}
		m := p.monitor(t.Context(), testNamespace, "self-internal", se, statusStyleDot, nil, defaultClusterDomain, func(string, string) {})
		if m.status != "Up" {
			t.Errorf("monitor(self with internalUrl) status = %q, want Up (probe of internalUrl)", m.status)
		}
		select {
		case <-probed:
		default:
			t.Error("monitor(self with internalUrl) never probed internalUrl")
		}
	})

	t.Run("falls back to href", func(t *testing.T) {
		se := pagev1alpha1.ServiceEntry{
			Name:    testSvcDisplayName,
			Href:    &srv.URL,
			Monitor: &monitorSelf,
		}
		p := &Poller{HTTPClient: srv.Client()}
		m := p.monitor(t.Context(), testNamespace, "self-href", se, statusStyleDot, nil, defaultClusterDomain, func(string, string) {})
		if m.status != "Up" {
			t.Errorf("monitor(self with href) status = %q, want Up (probe of href)", m.status)
		}
	})

	t.Run("no base URL probes nothing", func(t *testing.T) {
		se := pagev1alpha1.ServiceEntry{
			Name:    testSvcDisplayName,
			Monitor: &monitorSelf,
		}
		p := &Poller{HTTPClient: srv.Client()}
		m := p.monitor(t.Context(), testNamespace, "self-none", se, statusStyleDot, nil, defaultClusterDomain, func(string, string) {})
		if m.status != "" {
			t.Errorf("monitor(self without base URL) status = %q, want empty (no probe)", m.status)
		}
	})
}

func TestPollerPodStatusInvalidSelector(t *testing.T) {
	entry := pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: testPodSvcName, Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			Services: []pagev1alpha1.ServiceEntry{{
				PodSelector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{{Key: testAppLabelKey, Operator: testBogusWhen, Values: []string{"x"}}},
				},
			}},
		},
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	p := &Poller{
		Reader: cl, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
		Interval: time.Hour, HTTPClient: http.DefaultClient, Store: NewStore(),
	}

	se := entry.Spec.Entries()[0]
	status, text := p.podStatus(t.Context(), p.Reader, entry.Namespace, se.PodSelector, se.Name)
	if status != statusDown || text != "" {
		t.Errorf("podStatus(invalid selector) = (%q, %q), want (%q, \"\")", status, text, statusDown)
	}
}

func TestPollerPodStatusListPodsError(t *testing.T) {
	entry := pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: testPodSvcName, Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			Services: []pagev1alpha1.ServiceEntry{{
				PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{testAppLabelKey: testAppLabelValue}},
			}},
		},
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	failing := errInjectingReader{
		Reader: cl,
		failList: func(list client.ObjectList) bool {
			_, ok := list.(*corev1.PodList)
			return ok
		},
	}
	p := &Poller{
		Reader: failing, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
		Interval: time.Hour, HTTPClient: http.DefaultClient, Store: NewStore(),
	}

	se := entry.Spec.Entries()[0]
	status, text := p.podStatus(t.Context(), p.Reader, entry.Namespace, se.PodSelector, se.Name)
	if status != statusDown || text != "" {
		t.Errorf("podStatus(List error) = (%q, %q), want (%q, \"\")", status, text, statusDown)
	}
}

func TestPollerPodSelector(t *testing.T) {
	readyPod := func(name string, ready bool) *corev1.Pod {
		status := corev1.ConditionFalse
		if ready {
			status = corev1.ConditionTrue
		}
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace, Labels: map[string]string{testAppLabelKey: testAppLabelValue}},
			Status:     corev1.PodStatus{Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: status}}},
		}
	}
	// noReadyConditionPod has Conditions but none of type PodReady at all —
	// distinct from readyPod(false), which sets PodReady=False. Exercises
	// podReady's final "no Ready condition found" return false.
	noReadyConditionPod := func(name string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace, Labels: map[string]string{testAppLabelKey: testAppLabelValue}},
			Status:     corev1.PodStatus{Conditions: []corev1.PodCondition{{Type: corev1.PodScheduled, Status: corev1.ConditionTrue}}},
		}
	}

	cases := []struct {
		name       string
		pods       []client.Object
		wantStatus string
		wantText   string
	}{
		{"no matching pods", nil, statusDown, noMatchedPodsReadyText},
		{"one ready pod", []client.Object{readyPod("p1", true)}, "Up", testPodReadyOneOfOne},
		{"one not-ready pod", []client.Object{readyPod("p1", false)}, statusDown, "0/1 ready"},
		{"mixed readiness reports Partial", []client.Object{readyPod("p1", false), readyPod("p2", true)}, statusPartial, "1/2 ready"},
		{"pod with no Ready condition at all", []client.Object{noReadyConditionPod("p1")}, statusDown, "0/1 ready"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			selector := &metav1.LabelSelector{MatchLabels: map[string]string{testAppLabelKey: testAppLabelValue}}
			style := testStatusBasic
			entry := &pagev1alpha1.ServiceCard{
				ObjectMeta: metav1.ObjectMeta{Name: testPodSvcName, Namespace: testNamespace},
				Spec: pagev1alpha1.ServiceCardSpec{
					DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
					Group:        "G",
					Services: []pagev1alpha1.ServiceEntry{{
						Name:        "PodService",
						PodSelector: selector,
						StatusStyle: &style,
					}},
				},
			}

			scheme := testScheme(t)
			objs := append([]client.Object{entry}, tc.pods...)
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
			store := NewStore()
			p := &Poller{
				Reader: cl, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
				Interval: time.Hour, HTTPClient: http.DefaultClient, Store: store,
			}
			p.pollOnce(t.Context())

			cards := store.Snapshot()
			if len(cards) != 1 {
				t.Fatalf("Snapshot() = %d cards, want 1", len(cards))
			}
			if cards[0].PodStatus != tc.wantStatus {
				t.Errorf("card.PodStatus = %q, want %q", cards[0].PodStatus, tc.wantStatus)
			}
			if cards[0].PodReadyText != tc.wantText {
				t.Errorf("card.PodReadyText = %q, want %q", cards[0].PodReadyText, tc.wantText)
			}
		})
	}
}

func TestPollerShowStatsAndHideErrors(t *testing.T) {
	// Upstream that errors (non-JSON) so the widget would normally set Err,
	// and would set Fields on success — neither should appear given the flags.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	showStats := false
	hideErrors := false
	url := srv.URL
	entry := &pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: "flags", Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
			Group:        "G",
			Services: []pagev1alpha1.ServiceEntry{{
				Name:         "Flags",
				ShowStats:    &showStats,
				ErrorDisplay: &hideErrors,
				Widgets:      []pagev1alpha1.ServiceWidget{{Type: testWidgetType, URL: &url}},
			}},
		},
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(entry).Build()
	store := NewStore()
	p := &Poller{
		Reader: cl, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
		Interval: time.Hour, HTTPClient: srv.Client(), Store: store,
	}
	p.pollOnce(t.Context())

	cards := store.Snapshot()
	if len(cards) != 1 {
		t.Fatalf("Snapshot() = %d cards, want 1", len(cards))
	}
	if cards[0].Err != "" {
		t.Errorf("card.Err = %q, want empty (HideErrors)", cards[0].Err)
	}
	if len(cards[0].Fields) != 0 {
		t.Errorf("card.Fields = %+v, want none (ShowStats=false)", cards[0].Fields)
	}
}

func TestPollerInfoWidgetHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"current_weather":{"temperature":9,"weathercode":0}}`))
	}))
	defer srv.Close()

	// The openmeteo header widget reads its API base from the entry's typed
	// URL field, which keeps this test hermetic against the httptest server.
	iw := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: testHeaderWeather, Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{{
				Type:   testOpenMeteoType,
				URL:    &srv.URL,
				Config: &apiextensionsv1.JSON{Raw: []byte(`{"latitude":51.5,"longitude":-0.12}`)},
			}},
		},
	}
	// A datetime InfoWidget carries no registered widget, so it must NOT
	// produce a polled card.
	dt := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: testClockName, Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{{
				Type: headerTypeDatetime,
			}},
		},
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(iw, dt).Build()
	store := NewStore()
	p := &Poller{
		Reader: cl, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
		Interval: time.Hour, HTTPClient: srv.Client(), Store: store,
	}
	p.pollOnce(t.Context())

	cards := store.Snapshot()
	if len(cards) != 1 {
		t.Fatalf("Snapshot() = %d cards, want 1 (openmeteo header only; datetime is static)", len(cards))
	}
	if !cards[0].Header {
		t.Errorf("card.Header = false, want true for InfoWidget-sourced card")
	}
	if cards[0].ServiceName != testHeaderWeather {
		t.Errorf("card.ServiceName = %q, want weather (InfoWidget object name)", cards[0].ServiceName)
	}
}

// TestPollerPollOnceBoundsConcurrency verifies widget polls within a single
// pollOnce run concurrently (so one slow upstream doesn't push every other
// card a full cycle behind), but stay within maxConcurrentPolls in flight at
// once.
func TestPollerPollOnceBoundsConcurrency(t *testing.T) {
	const (
		numEntries = 20
		perRequest = 30 * time.Millisecond
	)

	var (
		current atomic.Int32
		mu      sync.Mutex
		maxSeen int32
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := current.Add(1)
		mu.Lock()
		if n > maxSeen {
			maxSeen = n
		}
		mu.Unlock()
		time.Sleep(perRequest)
		current.Add(-1)
		_, _ = w.Write([]byte(`{"status":"success","data":{"activeTargets":[{"health":"up"}]}}`))
	}))
	defer srv.Close()

	url := srv.URL
	objs := make([]client.Object, 0, numEntries)
	for i := range numEntries {
		objs = append(objs, &pagev1alpha1.ServiceCard{
			ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("svc-%d", i), Namespace: testNamespace},
			Spec: pagev1alpha1.ServiceCardSpec{
				DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
				Group:        testGroup,
				Services: []pagev1alpha1.ServiceEntry{{
					Name:    fmt.Sprintf("Service %d", i),
					Widgets: []pagev1alpha1.ServiceWidget{{Type: testWidgetType, URL: &url}},
				}},
			},
		})
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()

	store := NewStore()
	p := &Poller{
		Reader: cl, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
		Interval: time.Hour, HTTPClient: srv.Client(), Store: store,
	}

	start := time.Now()
	p.pollOnce(t.Context())
	elapsed := time.Since(start)

	cards := store.Snapshot()
	if len(cards) != numEntries {
		t.Fatalf("Snapshot() = %d cards, want %d", len(cards), numEntries)
	}

	mu.Lock()
	seen := maxSeen
	mu.Unlock()
	if seen <= 1 {
		t.Errorf("max concurrent in-flight requests = %d, want >1 (polls should overlap)", seen)
	}
	if seen > maxConcurrentPolls {
		t.Errorf("max concurrent in-flight requests = %d, want <= maxConcurrentPolls (%d)", seen, maxConcurrentPolls)
	}

	// Sequential polling would take roughly numEntries*perRequest; bounded
	// concurrency should finish well inside that.
	sequential := time.Duration(numEntries) * perRequest
	if elapsed >= sequential {
		t.Errorf("pollOnce took %s, want well under the sequential bound %s", elapsed, sequential)
	}
}

func TestPollerMissingSecret(t *testing.T) {
	url := testExampleURL
	entry := &pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: testSvcName, Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
			Group:        "G",
			Services: []pagev1alpha1.ServiceEntry{{
				Name: testSvcDisplayName,
				Widgets: []pagev1alpha1.ServiceWidget{{
					Type: testWidgetType,
					URL:  &url,
					Secrets: map[string]pagev1alpha1.SecretValueSource{
						testSecretField: {SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: testSecretMissingName},
							Key:                  testSecretField,
						}},
					},
				}},
			}},
		},
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(entry).Build()

	store := NewStore()
	p := &Poller{
		Reader: cl, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
		Interval: time.Hour, HTTPClient: http.DefaultClient, Store: store,
	}
	p.pollOnce(t.Context())

	cards := store.Snapshot()
	if len(cards) != 1 || cards[0].Err == "" {
		t.Fatalf("Snapshot() = %+v, want one card with a non-empty Err for missing Secret", cards)
	}
}

// TestPollerWidgetDefaultsSuppliesMissingSecret drives a full pollOnce with a
// ServiceCard "plex" widget that sets no secrets of its own and a Dashboard
// carrying spec.widgetDefaults for "plex": proves the poller actually merges
// the default into the widget's resolved secrets (not just that no error
// occurs, which a broken/unwired merge would also produce) by having the
// stub upstream assert the X-Plex-Token header it receives matches the
// secret supplied only via widgetDefaults.
func TestPollerWidgetDefaultsSuppliesMissingSecret(t *testing.T) {
	const wantToken = "shared-plex-token"

	var gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Plex-Token")
		_, _ = w.Write([]byte(`{"MediaContainer":{"size":0}}`))
	}))
	defer srv.Close()

	url := srv.URL
	entry := &pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: testSvcName, Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
			Group:        "G",
			Services: []pagev1alpha1.ServiceEntry{{
				Name: testSvcDisplayName,
				Widgets: []pagev1alpha1.ServiceWidget{{
					Type: testWidgetTypePlex,
					URL:  &url,
				}},
			}},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: testPlexSecretRefName, Namespace: testNamespace},
		Data:       map[string][]byte{testSecretField: []byte(wantToken)},
	}
	instance := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardSpec{
			WidgetDefaults: map[string]pagev1alpha1.WidgetDefaultsEntry{
				testWidgetTypePlex: {Secrets: map[string]pagev1alpha1.SecretValueSource{
					testSecretField: {SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: testPlexSecretRefName},
						Key:                  testSecretField,
					}},
				}},
			},
		},
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(entry, secret, instance).Build()

	store := NewStore()
	p := &Poller{
		Reader: cl, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
		Interval: time.Hour, HTTPClient: srv.Client(), Store: store,
	}
	p.pollOnce(t.Context())

	cards := store.Snapshot()
	if len(cards) != 1 || cards[0].Err != "" {
		t.Fatalf("Snapshot() = %+v, want one card with no Err", cards)
	}
	if gotToken != wantToken {
		t.Errorf("upstream saw X-Plex-Token = %q, want %q (widgetDefaults secret should have reached the widget)", gotToken, wantToken)
	}
}

// TestPollerSecretRefSuppliesFields drives a full pollOnce with a widget that
// sets SecretRef (and no explicit Secrets of its own): proves the poller
// expands every key of the named Secret into a resolved secret field, by
// having the stub upstream assert the X-Plex-Token header it receives
// matches the "token" key of the SecretRef'd Secret.
func TestPollerSecretRefSuppliesFields(t *testing.T) {
	const wantToken = "secret-ref-token"
	const secretRefName = "plex-credentials"

	var gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Plex-Token")
		_, _ = w.Write([]byte(`{"MediaContainer":{"size":0}}`))
	}))
	defer srv.Close()

	url := srv.URL
	secretRef := secretRefName
	entry := &pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: testSvcName, Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
			Group:        "G",
			Services: []pagev1alpha1.ServiceEntry{{
				Name: testSvcDisplayName,
				Widgets: []pagev1alpha1.ServiceWidget{{
					Type:      testWidgetTypePlex,
					URL:       &url,
					SecretRef: &secretRef,
				}},
			}},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretRefName, Namespace: testNamespace},
		Data:       map[string][]byte{testSecretField: []byte(wantToken)},
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(entry, secret).Build()

	store := NewStore()
	p := &Poller{
		Reader: cl, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
		Interval: time.Hour, HTTPClient: srv.Client(), Store: store,
	}
	p.pollOnce(t.Context())

	cards := store.Snapshot()
	if len(cards) != 1 || cards[0].Err != "" {
		t.Fatalf("Snapshot() = %+v, want one card with no Err", cards)
	}
	if gotToken != wantToken {
		t.Errorf("upstream saw X-Plex-Token = %q, want %q (secretRef's key should have reached the widget)", gotToken, wantToken)
	}
}

// TestPollerSecretRefPerKeyOverrideWins proves that a widget's own Secrets
// map wins, per key, over a field of the same name supplied by SecretRef —
// the documented precedence (see ServiceWidget.SecretRef's doc comment).
func TestPollerSecretRefPerKeyOverrideWins(t *testing.T) {
	const refToken = "from-secret-ref"
	const overrideToken = "explicit-override"
	const secretRefName = "plex-credentials"

	var gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Plex-Token")
		_, _ = w.Write([]byte(`{"MediaContainer":{"size":0}}`))
	}))
	defer srv.Close()

	url := srv.URL
	secretRef := secretRefName
	entry := &pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: testSvcName, Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
			Group:        "G",
			Services: []pagev1alpha1.ServiceEntry{{
				Name: testSvcDisplayName,
				Widgets: []pagev1alpha1.ServiceWidget{{
					Type:      testWidgetTypePlex,
					URL:       &url,
					SecretRef: &secretRef,
					Secrets: map[string]pagev1alpha1.SecretValueSource{
						testSecretField: *ptrSVS(overrideToken),
					},
				}},
			}},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretRefName, Namespace: testNamespace},
		Data:       map[string][]byte{testSecretField: []byte(refToken)},
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(entry, secret).Build()

	store := NewStore()
	p := &Poller{
		Reader: cl, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
		Interval: time.Hour, HTTPClient: srv.Client(), Store: store,
	}
	p.pollOnce(t.Context())

	cards := store.Snapshot()
	if len(cards) != 1 || cards[0].Err != "" {
		t.Fatalf("Snapshot() = %+v, want one card with no Err", cards)
	}
	if gotToken != overrideToken {
		t.Errorf("upstream saw X-Plex-Token = %q, want %q (widget's own Secrets should win over SecretRef)", gotToken, overrideToken)
	}
}

// TestPollerSecretRefDenied simulates spec.secretPolicy: Labeled denying the
// dashboard pod's RBAC access to a SecretRef'd Secret (Get fails, mirroring
// what an actual RBAC-scoped client returns for a Secret the Role doesn't
// list — see internal/controller/dashboard_rbac.go's referencedSecretNames/
// filterLabeledSecrets), proving the card surfaces a clear error rather than
// silently polling with no fields.
func TestPollerSecretRefDenied(t *testing.T) {
	const secretRefName = "unlabeled-secret"

	url := testExampleURL
	secretRef := secretRefName
	entry := &pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: testSvcName, Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
			Group:        "G",
			Services: []pagev1alpha1.ServiceEntry{{
				Name: testSvcDisplayName,
				Widgets: []pagev1alpha1.ServiceWidget{{
					Type:      testWidgetTypePlex,
					URL:       &url,
					SecretRef: &secretRef,
				}},
			}},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretRefName, Namespace: testNamespace},
		Data:       map[string][]byte{testSecretField: []byte("s3cr3t")},
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(entry, secret).Build()
	denied := errInjectingReader{
		Reader: cl,
		failGet: func(key client.ObjectKey, obj client.Object) bool {
			_, ok := obj.(*corev1.Secret)
			return ok && key.Name == secretRefName
		},
	}

	store := NewStore()
	p := &Poller{
		Reader: cl, SecretReader: denied, Namespace: testNamespace, DashboardName: testDashboardName,
		Interval: time.Hour, HTTPClient: http.DefaultClient, Store: store,
	}
	p.pollOnce(t.Context())

	cards := store.Snapshot()
	if len(cards) != 1 || cards[0].Err == "" {
		t.Fatalf("Snapshot() = %+v, want one card with a non-empty Err for a denied SecretRef Get", cards)
	}
}

// TestPollerSecretRefMissingSecret exercises resolveSecretRefFields'
// apierrors.IsNotFound branch directly (as opposed to
// TestPollerSecretRefDenied's generic Get-error/RBAC-denial simulation): a
// widget's SecretRef names a Secret that simply doesn't exist.
func TestPollerSecretRefMissingSecret(t *testing.T) {
	const secretRefName = "does-not-exist"

	url := testExampleURL
	secretRef := secretRefName
	entry := &pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: testSvcName, Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
			Group:        "G",
			Services: []pagev1alpha1.ServiceEntry{{
				Name: testSvcDisplayName,
				Widgets: []pagev1alpha1.ServiceWidget{{
					Type:      testWidgetTypePlex,
					URL:       &url,
					SecretRef: &secretRef,
				}},
			}},
		},
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(entry).Build()

	store := NewStore()
	p := &Poller{
		Reader: cl, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
		Interval: time.Hour, HTTPClient: http.DefaultClient, Store: store,
	}
	p.pollOnce(t.Context())

	cards := store.Snapshot()
	if len(cards) != 1 || cards[0].Err == "" {
		t.Fatalf("Snapshot() = %+v, want one card with a non-empty Err for a nonexistent SecretRef Secret", cards)
	}
}

// TestPollerWidgetDefaultsMissingSecretSetsCardErr exercises
// resolveWidgetSecrets' widgetDefaults-resolution error branch: a widget
// sets neither Secrets nor SecretRef of its own, but the Dashboard's
// widgetDefaults entry for its type points at a Secret that doesn't exist.
func TestPollerWidgetDefaultsMissingSecretSetsCardErr(t *testing.T) {
	url := testExampleURL
	entry := &pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: testSvcName, Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
			Group:        "G",
			Services: []pagev1alpha1.ServiceEntry{{
				Name: testSvcDisplayName,
				Widgets: []pagev1alpha1.ServiceWidget{{
					Type: testWidgetTypePlex,
					URL:  &url,
				}},
			}},
		},
	}
	instance := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardSpec{
			WidgetDefaults: map[string]pagev1alpha1.WidgetDefaultsEntry{
				testWidgetTypePlex: {Secrets: map[string]pagev1alpha1.SecretValueSource{
					testSecretField: {SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: testSecretMissingName},
						Key:                  testSecretField,
					}},
				}},
			},
		},
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(entry, instance).Build()

	store := NewStore()
	p := &Poller{
		Reader: cl, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
		Interval: time.Hour, HTTPClient: http.DefaultClient, Store: store,
	}
	p.pollOnce(t.Context())

	cards := store.Snapshot()
	if len(cards) != 1 || cards[0].Err == "" {
		t.Fatalf("Snapshot() = %+v, want one card with a non-empty Err for a widgetDefaults Secret that doesn't exist", cards)
	}
}

// TestPollerInfoWidgetSecretRefSuppliesFields is TestPollerSecretRefSuppliesFields'
// InfoWidget counterpart: proves pollInfoWidget's SecretRef branch expands
// every key of the named Secret into a resolved secret field too, by having
// the stub upstream assert the "appid" query parameter (openweathermap's API
// key) matches the SecretRef'd Secret's "apiKey" key.
func TestPollerInfoWidgetSecretRefSuppliesFields(t *testing.T) {
	const wantAPIKey = "owm-secret-ref-key"
	const secretRefName = "owm-credentials"

	var gotAPIKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.URL.Query().Get("appid")
		_, _ = w.Write([]byte(`{"main":{"temp":10},"weather":[{"main":"Clouds"}]}`))
	}))
	defer srv.Close()

	url := srv.URL
	secretRef := secretRefName
	iw := pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: testHeaderWeather, Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{{
				Type:      widgetTypeOpenWeatherMap,
				URL:       &url,
				SecretRef: &secretRef,
				Config:    &apiextensionsv1.JSON{Raw: []byte(`{"latitude":51.5,"longitude":-0.12}`)},
			}},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretRefName, Namespace: testNamespace},
		Data:       map[string][]byte{openWeatherMapSecretAPIKey: []byte(wantAPIKey)},
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&iw, secret).Build()

	store := NewStore()
	p := &Poller{
		Reader: cl, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
		Interval: time.Hour, HTTPClient: srv.Client(), Store: store,
	}
	p.pollOnce(t.Context())

	cards := store.Snapshot()
	if len(cards) != 1 || cards[0].Err != "" {
		t.Fatalf("Snapshot() = %+v, want one card with no Err", cards)
	}
	if gotAPIKey != wantAPIKey {
		t.Errorf("upstream saw appid = %q, want %q (InfoWidget secretRef's key should have reached the widget)", gotAPIKey, wantAPIKey)
	}
}

// TestPollerSecretRefWinsOverWidgetDefaults proves the documented precedence
// (ServiceWidget.SecretRef's doc comment): a widget-level SecretRef field
// wins over the same field name supplied by a dashboard-wide widgetDefaults
// entry, even though the widget sets no explicit Secrets of its own.
func TestPollerSecretRefWinsOverWidgetDefaults(t *testing.T) {
	const defaultsToken = "from-widget-defaults"
	const refToken = "from-secret-ref"
	const secretRefName = "plex-credentials"
	const defaultsSecretName = "shared-plex-defaults"

	var gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Plex-Token")
		_, _ = w.Write([]byte(`{"MediaContainer":{"size":0}}`))
	}))
	defer srv.Close()

	url := srv.URL
	secretRef := secretRefName
	entry := &pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: testSvcName, Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
			Group:        "G",
			Services: []pagev1alpha1.ServiceEntry{{
				Name: testSvcDisplayName,
				Widgets: []pagev1alpha1.ServiceWidget{{
					Type:      testWidgetTypePlex,
					URL:       &url,
					SecretRef: &secretRef,
				}},
			}},
		},
	}
	refSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretRefName, Namespace: testNamespace},
		Data:       map[string][]byte{testSecretField: []byte(refToken)},
	}
	defaultsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: defaultsSecretName, Namespace: testNamespace},
		Data:       map[string][]byte{testSecretField: []byte(defaultsToken)},
	}
	instance := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardSpec{
			WidgetDefaults: map[string]pagev1alpha1.WidgetDefaultsEntry{
				testWidgetTypePlex: {Secrets: map[string]pagev1alpha1.SecretValueSource{
					testSecretField: {SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: defaultsSecretName},
						Key:                  testSecretField,
					}},
				}},
			},
		},
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(entry, refSecret, defaultsSecret, instance).Build()

	store := NewStore()
	p := &Poller{
		Reader: cl, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
		Interval: time.Hour, HTTPClient: srv.Client(), Store: store,
	}
	p.pollOnce(t.Context())

	cards := store.Snapshot()
	if len(cards) != 1 || cards[0].Err != "" {
		t.Fatalf("Snapshot() = %+v, want one card with no Err", cards)
	}
	if gotToken != refToken {
		t.Errorf("upstream saw X-Plex-Token = %q, want %q (widget-level SecretRef should win over dashboard-wide widgetDefaults)", gotToken, refToken)
	}
}

// TestPollerPollWidgetCopiesDescriptionTargetAndConfig drives pollWidget
// directly to exercise three of its field-copy branches in one pass:
// entry.Spec.Description/Target onto the Card, and widget.Config.Raw into
// WidgetConfig.Config (proven by prometheusmetric's config-driven label
// reaching the rendered Field).
func TestPollerPollWidgetCopiesDescriptionTargetAndConfig(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"result":[{"value":[0,"1"]}]}}`))
	}))
	defer srv.Close()

	url := srv.URL
	description := "a description"
	target := targetSelf
	entry := pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: testSvcName, Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			Group: testGroup,
			Services: []pagev1alpha1.ServiceEntry{{
				Name:        testSvcAName,
				Description: &description,
				Target:      &target,
			}},
		},
	}
	widget := &pagev1alpha1.ServiceWidget{
		Type:   "prometheusmetric",
		URL:    &url,
		Config: &apiextensionsv1.JSON{Raw: []byte(`{"query":"up","label":"Custom"}`)},
	}

	store := NewStore()
	p := &Poller{HTTPClient: srv.Client(), Store: store}
	p.pollWidget(t.Context(), testCardKeyA, entry.Namespace, entry.Spec.Entries()[0], widget, monitorProbeResult{}, false, nil)

	cards := store.Snapshot()
	if len(cards) != 1 {
		t.Fatalf("Snapshot() = %d cards, want 1", len(cards))
	}
	card := cards[0]
	if card.Description != description {
		t.Errorf("card.Description = %q, want %q", card.Description, description)
	}
	if card.Target != target {
		t.Errorf("card.Target = %q, want %q", card.Target, target)
	}
	if len(card.Fields) != 1 || card.Fields[0].Label != testCustomName {
		t.Errorf("card.Fields = %+v, want a single Custom-labeled field (proves widget.Config.Raw reached the widget)", card.Fields)
	}
}

// TestPollerPollWidgetHonorsPollIntervalSeconds exercises the
// PollIntervalSeconds skip path: a widget whose override hasn't elapsed yet
// must not be re-polled (leaving Fields from the last widget poll
// untouched), but the entry's monitor result — probed every cycle
// regardless of the widget's own interval — must still be merged into the
// existing card so its Status doesn't go stale. Once the override has
// elapsed, the widget must be polled as normal again.
func TestPollerPollWidgetHonorsPollIntervalSeconds(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"status":"success","data":{"activeTargets":[{"health":"up"}]}}`))
	}))
	defer srv.Close()

	url := srv.URL
	overrideSeconds := int32(100)
	entry := pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: testSvcName, Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			Group: testGroup,
			Services: []pagev1alpha1.ServiceEntry{{
				Name: testSvcAName,
			}},
		},
	}
	widget := &pagev1alpha1.ServiceWidget{Type: testWidgetType, URL: &url, PollIntervalSeconds: &overrideSeconds}

	store := NewStore()
	p := &Poller{HTTPClient: srv.Client(), Store: store, Interval: time.Second}

	// First poll of key is always due, regardless of override.
	p.pollWidget(t.Context(), testCardKeyA, entry.Namespace, entry.Spec.Entries()[0], widget, monitorProbeResult{status: "Up"}, false, nil)
	if n := hits.Load(); n != 1 {
		t.Fatalf("after first poll, upstream hit %d times, want 1", n)
	}
	cards := store.Snapshot()
	if len(cards) != 1 {
		t.Fatalf("Snapshot() = %d cards, want 1", len(cards))
	}
	if len(cards[0].Fields) == 0 {
		t.Fatalf("after first poll, card.Fields is empty, want widget fields populated")
	}
	wantFields := cards[0].Fields

	// Immediately polling again is within the 100s override: the widget is
	// not re-polled, but a changed monitor status must still land on the
	// stored card.
	p.pollWidget(t.Context(), testCardKeyA, entry.Namespace, entry.Spec.Entries()[0], widget, monitorProbeResult{status: statusDown}, false, nil)
	if n := hits.Load(); n != 1 {
		t.Errorf("after second (not-yet-due) poll, upstream hit %d times, want still 1", n)
	}
	cards = store.Snapshot()
	if len(cards) != 1 {
		t.Fatalf("Snapshot() = %d cards, want 1", len(cards))
	}
	if cards[0].Status != statusDown {
		t.Errorf("after second (not-yet-due) poll, card.Status = %q, want %q (fresh monitor result must not go stale)", cards[0].Status, statusDown)
	}
	if !slices.Equal(cards[0].Fields, wantFields) {
		t.Errorf("after second (not-yet-due) poll, card.Fields = %+v, want unchanged %+v", cards[0].Fields, wantFields)
	}

	// Back-date the last-polled time past the override: due again.
	p.widgetLastPolledMu.Lock()
	p.widgetLastPolled[testCardKeyA] = time.Now().Add(-101 * time.Second)
	p.widgetLastPolledMu.Unlock()
	p.pollWidget(t.Context(), testCardKeyA, entry.Namespace, entry.Spec.Entries()[0], widget, monitorProbeResult{status: "Up"}, false, nil)
	if n := hits.Load(); n != 2 {
		t.Errorf("after third (due) poll, upstream hit %d times, want 2", n)
	}
}

// TestPollerMergeMonitorIntoStoredCardFirstCycle covers pollWidget's skip
// path when no card has been stored yet for the key (the widget's very
// first cycle happens to already be past due for some other reason, or the
// override skip races the very first poll): the merge must still produce a
// card carrying the monitor result, with no fields.
func TestPollerMergeMonitorIntoStoredCardFirstCycle(t *testing.T) {
	overrideSeconds := int32(100)
	entry := pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: testSvcName, Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			Group: testGroup,
			Services: []pagev1alpha1.ServiceEntry{{
				Name: testSvcAName,
			}},
		},
	}
	url := "http://example.invalid"
	widget := &pagev1alpha1.ServiceWidget{Type: testWidgetType, URL: &url, PollIntervalSeconds: &overrideSeconds}

	store := NewStore()
	p := &Poller{Store: store, Interval: time.Second}
	// Pre-seed widgetLastPolled so duePoll reports not-due even on the
	// first call, exercising the "no previous card" branch of the merge.
	p.widgetLastPolled = map[string]time.Time{testCardKeyA: time.Now()}

	p.pollWidget(t.Context(), testCardKeyA, entry.Namespace, entry.Spec.Entries()[0], widget, monitorProbeResult{status: "Up"}, false, nil)

	cards := store.Snapshot()
	if len(cards) != 1 {
		t.Fatalf("Snapshot() = %d cards, want 1", len(cards))
	}
	if cards[0].Status != "Up" {
		t.Errorf("card.Status = %q, want %q", cards[0].Status, "Up")
	}
	if len(cards[0].Fields) != 0 {
		t.Errorf("card.Fields = %+v, want empty (widget never polled yet)", cards[0].Fields)
	}
}

// TestPollerPollInfoWidgetHonorsPollIntervalSeconds is the InfoWidget analog
// of TestPollerPollWidgetHonorsPollIntervalSeconds.
func TestPollerPollInfoWidgetHonorsPollIntervalSeconds(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"current_weather":{"temperature":9,"weathercode":0}}`))
	}))
	defer srv.Close()

	overrideSeconds := int32(100)
	iw := pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: testHeaderWeather, Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{{
				Type:                testOpenMeteoType,
				URL:                 &srv.URL,
				Config:              &apiextensionsv1.JSON{Raw: []byte(`{"latitude":51.5,"longitude":-0.12}`)},
				PollIntervalSeconds: &overrideSeconds,
			}},
		},
	}

	const key = "header/" + testHeaderWeather
	store := NewStore()
	p := &Poller{HTTPClient: srv.Client(), Store: store, Interval: time.Second}

	p.pollInfoWidget(t.Context(), key, iw, iw.Spec.Entries()[0], nil)
	if n := hits.Load(); n != 1 {
		t.Fatalf("after first poll, upstream hit %d times, want 1", n)
	}

	p.pollInfoWidget(t.Context(), key, iw, iw.Spec.Entries()[0], nil)
	if n := hits.Load(); n != 1 {
		t.Errorf("after second (not-yet-due) poll, upstream hit %d times, want still 1", n)
	}

	p.widgetLastPolledMu.Lock()
	p.widgetLastPolled[key] = time.Now().Add(-101 * time.Second)
	p.widgetLastPolledMu.Unlock()
	p.pollInfoWidget(t.Context(), key, iw, iw.Spec.Entries()[0], nil)
	if n := hits.Load(); n != 2 {
		t.Errorf("after third (due) poll, upstream hit %d times, want 2", n)
	}
}

// TestPollerDuePollFloorClampedToInterval verifies an override shorter than
// the poller's own Interval has no effect: the poller only ever runs once
// per Interval, so a widget can't poll more often than that regardless of a
// smaller PollIntervalSeconds.
func TestPollerDuePollFloorClampedToInterval(t *testing.T) {
	overrideSeconds := int32(1)
	p := &Poller{Interval: time.Hour}

	if !p.duePoll("k", &overrideSeconds) {
		t.Fatal("duePoll() = false on first call, want true")
	}
	if p.duePoll("k", &overrideSeconds) {
		t.Error("duePoll() = true immediately after, want false (clamped to the hour-long Interval, not the 1s override)")
	}
}

// TestPollerDuePollNilOrZeroOverrideAlwaysDue verifies the common case (no
// override) never gets gated, and is never tracked in widgetLastPolled.
func TestPollerDuePollNilOrZeroOverrideAlwaysDue(t *testing.T) {
	p := &Poller{Interval: time.Hour}
	zero := int32(0)

	for range 3 {
		if !p.duePoll("k", nil) {
			t.Error("duePoll(nil) = false, want true every time")
		}
		if !p.duePoll("k", &zero) {
			t.Error("duePoll(&0) = false, want true every time")
		}
	}
	if len(p.widgetLastPolled) != 0 {
		t.Errorf("widgetLastPolled = %v, want empty (nil/zero overrides aren't tracked)", p.widgetLastPolled)
	}
}

// pruneTestKeepKey/pruneTestDropKey name the two bookkeeping-map entries
// TestPollerPruneWidgetLastPolledRemovesUnkept and
// TestPollerPruneWidgetLastPolledPrunesClampWarned exercise pruning against.
const (
	pruneTestKeepKey = "keep"
	pruneTestDropKey = "drop"
)

// TestPollerPruneWidgetLastPolledRemovesUnkept verifies pruneWidgetLastPolled
// mirrors Store.Prune: an entry not in this cycle's keep set is dropped, so
// a deleted or edited-away-from-an-override widget's bookkeeping doesn't
// accumulate forever.
func TestPollerPruneWidgetLastPolledRemovesUnkept(t *testing.T) {
	p := &Poller{
		widgetLastPolled: map[string]time.Time{pruneTestKeepKey: time.Now(), pruneTestDropKey: time.Now()},
	}
	p.pruneWidgetLastPolled(map[string]bool{pruneTestKeepKey: true})

	if _, ok := p.widgetLastPolled[pruneTestKeepKey]; !ok {
		t.Error("pruneWidgetLastPolled removed a kept key")
	}
	if _, ok := p.widgetLastPolled[pruneTestDropKey]; ok {
		t.Error("pruneWidgetLastPolled did not remove an unkept key")
	}
}

// TestPollerPruneWidgetLastPolledPrunesClampWarned verifies
// pruneWidgetLastPolled also prunes clampWarned alongside widgetLastPolled,
// so a deleted widget's "clamp already logged" bookkeeping doesn't leak
// forever either.
func TestPollerPruneWidgetLastPolledPrunesClampWarned(t *testing.T) {
	p := &Poller{
		clampWarned: map[string]bool{pruneTestKeepKey: true, pruneTestDropKey: true},
	}
	p.pruneWidgetLastPolled(map[string]bool{pruneTestKeepKey: true})

	if !p.clampWarned[pruneTestKeepKey] {
		t.Error("pruneWidgetLastPolled removed a kept clampWarned key")
	}
	if p.clampWarned[pruneTestDropKey] {
		t.Error("pruneWidgetLastPolled did not remove an unkept clampWarned key")
	}
}

// TestPollerDuePollWarnsAboutClampOncePerKey verifies duePoll records a
// widget key in clampWarned the first time its override is actually clamped
// by the poller's own Interval, and doesn't need to re-log on subsequent
// calls for the same key (tested directly against the once-per-key set,
// since asserting on log output isn't practical here).
func TestPollerDuePollWarnsAboutClampOncePerKey(t *testing.T) {
	overrideSeconds := int32(1)
	p := &Poller{Interval: time.Hour}

	p.duePoll("k", &overrideSeconds)
	if !p.clampWarned["k"] {
		t.Fatal("duePoll() did not record key in clampWarned after a clamping override")
	}

	// A second call for the same key (even though not-due) must not reset
	// or otherwise misbehave — the key just stays marked as already warned.
	p.duePoll("k", &overrideSeconds)
	if !p.clampWarned["k"] {
		t.Error("duePoll() clampWarned entry disappeared on a subsequent call for the same key")
	}
	if len(p.clampWarned) != 1 {
		t.Errorf("clampWarned = %v, want exactly one entry", p.clampWarned)
	}
}

// TestPollerDuePollNoClampWarningWhenOverrideNotShorter verifies an override
// that's >= the poller's Interval (so the clamp is a no-op) never gets
// recorded in clampWarned — there's nothing to warn about.
func TestPollerDuePollNoClampWarningWhenOverrideNotShorter(t *testing.T) {
	overrideSeconds := int32(30)
	p := &Poller{Interval: 15 * time.Second}

	p.duePoll("k", &overrideSeconds)
	if p.clampWarned["k"] {
		t.Error("duePoll() recorded clampWarned for an override that wasn't actually shortened")
	}
}

func TestMetricErrTreatsUnreachableAndHTTPStatusAsError(t *testing.T) {
	cases := []struct {
		name    string
		fields  []Field
		wantErr bool
	}{
		{"unreachable status", []Field{{Label: labelStatus, Value: statusUnreach}}, true},
		{"http error status", []Field{{Label: labelStatus, Value: testHTTP500}}, true},
		{"healthy status is not an error", []Field{{Label: labelStatus, Value: statusHealthy}}, false},
		{"down tunnel status is not an error", []Field{{Label: labelStatus, Value: statusDown}}, false},
		{"inactive tunnel status is not an error", []Field{{Label: labelStatus, Value: statusInactive}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := metricErr(nil, tc.fields)
			if (err != nil) != tc.wantErr {
				t.Errorf("metricErr(nil, %+v) = %v, want error presence %v", tc.fields, err, tc.wantErr)
			}
		})
	}
}

func TestPollerInfoWidgetSecretErrorSetsCardErr(t *testing.T) {
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()

	iw := pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: testHeaderWeather, Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{{
				Type: testOpenMeteoType,
				Secrets: map[string]pagev1alpha1.SecretValueSource{
					testSecretField: {SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: testSecretMissingName},
						Key:                  testSecretField,
					}},
				},
			}},
		},
	}

	store := NewStore()
	p := &Poller{SecretReader: cl, Store: store}
	p.pollInfoWidget(t.Context(), "header/weather", iw, iw.Spec.Entries()[0], nil)

	cards := store.Snapshot()
	if len(cards) != 1 || cards[0].Err == "" {
		t.Fatalf("Snapshot() = %+v, want one card with a non-empty Err for missing Secret", cards)
	}
}

// TestPollerInfoWidgetClusterWidgetUsesKubeReader exercises pollInfoWidget's
// ClusterWidget branch: a kubemetrics-typed InfoWidget is polled via
// PollCluster against KubeReader instead of Poll against HTTPClient.
func TestPollerInfoWidgetClusterWidgetUsesKubeReader(t *testing.T) {
	scheme := testScheme(t)
	if err := metricsv1beta1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	kubeCl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		node("n1", "2", "4Gi"),
		nodeMetrics("n1", "1", "1Gi"),
	).Build()

	iw := pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{{
				Type: testKubeMetricsType,
			}},
		},
	}

	store := NewStore()
	p := &Poller{KubeReader: kubeCl, Store: store}
	p.pollInfoWidget(t.Context(), "header/cluster", iw, iw.Spec.Entries()[0], nil)

	cards := store.Snapshot()
	if len(cards) != 1 {
		t.Fatalf("Snapshot() = %d cards, want 1", len(cards))
	}
	if cards[0].Err != "" {
		t.Fatalf("card.Err = %q, want empty", cards[0].Err)
	}
	if len(cards[0].Fields) == 0 {
		t.Errorf("card.Fields = empty, want CPU/Memory fields polled via PollCluster against KubeReader")
	}
}

// TestPollerInfoWidgetPollErrorSetsCardErr drives pollInfoWidget directly
// with a registered widget type whose Poll returns a real Go error (rather
// than a Status field) to exercise the "err != nil" branch independent of
// any Secrets resolution.
func TestPollerInfoWidgetPollErrorSetsCardErr(t *testing.T) {
	iw := pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: "metric", Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{{
				Type: "prometheusmetric",
			}},
		},
	}

	store := NewStore()
	p := &Poller{HTTPClient: http.DefaultClient, Store: store}
	p.pollInfoWidget(t.Context(), "header/metric", iw, iw.Spec.Entries()[0], nil)

	cards := store.Snapshot()
	if len(cards) != 1 || cards[0].Err == "" {
		t.Fatalf("Snapshot() = %+v, want one card with a non-empty Err (prometheusmetric requires a URL)", cards)
	}
}

// TestPollerInfoWidgetUsesTypedURLField verifies InfoWidgetEntry.URL resolves
// the widget's base URL.
func TestPollerInfoWidgetUsesTypedURLField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"current_weather":{"temperature":9,"weathercode":0}}`))
	}))
	defer srv.Close()

	entryURL := srv.URL
	iw := pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: testHeaderWeather, Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{{
				Type:   testOpenMeteoType,
				URL:    &entryURL,
				Config: &apiextensionsv1.JSON{Raw: []byte(`{"latitude":51.5,"longitude":-0.12}`)},
			}},
		},
	}

	store := NewStore()
	p := &Poller{HTTPClient: srv.Client(), Store: store}
	p.pollInfoWidget(t.Context(), "header/weather", iw, iw.Spec.Entries()[0], nil)

	cards := store.Snapshot()
	if len(cards) != 1 {
		t.Fatalf("Snapshot() = %d cards, want 1", len(cards))
	}
	if cards[0].Err != "" {
		t.Errorf("card.Err = %q, want empty (entry.URL should have resolved the widget's base URL)", cards[0].Err)
	}
}

// TestPollerMultiWidgetFormProducesPerEntryCards is the InfoWidget
// multi-widget-form analog of TestPollerMultiCardFormProducesPerEntryCards:
// one InfoWidget object's spec.widgets yields one Card per entry, each keyed
// by a composite header/<name>/<index> key rather than colliding on the
// shared object name (see poller.go's pollInfoWidget and server.go's
// buildHeader doc comments for why the correlation must go through Key).
func TestPollerMultiWidgetFormProducesPerEntryCards(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"current_weather":{"temperature":9,"weathercode":0}}`))
	}))
	defer srv.Close()

	const multiName = "multi-header"
	iw := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: multiName, Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{
				{
					Type:   testOpenMeteoType,
					URL:    &srv.URL,
					Config: &apiextensionsv1.JSON{Raw: []byte(`{"latitude":51.5,"longitude":-0.12}`)},
				},
				{
					Type:   testOpenMeteoType,
					URL:    &srv.URL,
					Config: &apiextensionsv1.JSON{Raw: []byte(`{"latitude":40.7,"longitude":-74.0}`)},
				},
			},
		},
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(iw).Build()

	store := NewStore()
	p := &Poller{
		Reader: cl, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
		Interval: time.Hour, HTTPClient: srv.Client(), Store: store,
	}
	p.pollOnce(t.Context())

	cards := store.Snapshot()
	if len(cards) != 2 {
		t.Fatalf("Snapshot() = %d cards, want 2 (one per widgets entry)", len(cards))
	}

	byKey := map[string]Card{}
	for _, c := range cards {
		byKey[c.Key] = c
	}

	first, ok := byKey["header/"+multiName+"/0"]
	if !ok {
		t.Fatalf("no card for key header/%s/0; got keys %v", multiName, byKey)
	}
	second, ok := byKey["header/"+multiName+"/1"]
	if !ok {
		t.Fatalf("no card for key header/%s/1; got keys %v", multiName, byKey)
	}
	if first.ServiceName != multiName || second.ServiceName != multiName {
		t.Errorf("both entries' ServiceName = %q/%q, want both %q (they share one InfoWidget object name)", first.ServiceName, second.ServiceName, multiName)
	}
	if first.Key == second.Key {
		t.Errorf("first.Key == second.Key (%q); entries sharing an object name must still get distinct composite Keys", first.Key)
	}

	// A second poll cycle after removing the second entry must prune its
	// card from Store, mirroring the multi-card ServiceCard form's pruning.
	iw.Spec.Widgets = iw.Spec.Widgets[:1]
	if err := cl.Update(t.Context(), iw); err != nil {
		t.Fatalf("updating InfoWidget: %v", err)
	}
	p.pollOnce(t.Context())

	cards = store.Snapshot()
	if len(cards) != 1 {
		t.Fatalf("Snapshot() after removing an entry = %d cards, want 1", len(cards))
	}
	if cards[0].Key != "header/"+multiName+"/0" {
		t.Errorf("remaining card.Key = %q, want header/%s/0", cards[0].Key, multiName)
	}
}

// TestPollerMultiWidgetFormPerEntrySecretsAndPollInterval verifies each
// spec.widgets entry resolves its own Secrets and honors its own
// PollIntervalSeconds independently, keyed by its own composite key — one
// entry's secret-resolution error must not affect a sibling entry's card,
// and a due/not-due decision for one entry's key must not affect another's.
func TestPollerMultiWidgetFormPerEntrySecretsAndPollInterval(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"current_weather":{"temperature":9,"weathercode":0}}`))
	}))
	defer srv.Close()

	overrideSeconds := int32(100)
	const multiName = "multi-secrets"
	iw := pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: multiName, Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{
				{
					// Entry 0: a Secrets reference to a Secret that doesn't
					// exist, so its card gets a resolution error.
					Type: testOpenMeteoType,
					Secrets: map[string]pagev1alpha1.SecretValueSource{
						testSecretField: {SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: testSecretMissingName},
							Key:                  testSecretField,
						}},
					},
				},
				{
					// Entry 1: no Secrets, but its own PollIntervalSeconds
					// override.
					Type:                testOpenMeteoType,
					URL:                 &srv.URL,
					Config:              &apiextensionsv1.JSON{Raw: []byte(`{"latitude":51.5,"longitude":-0.12}`)},
					PollIntervalSeconds: &overrideSeconds,
				},
			},
		},
	}
	entries := iw.Spec.Entries()

	store := NewStore()
	p := &Poller{SecretReader: fake.NewClientBuilder().WithScheme(testScheme(t)).Build(), HTTPClient: srv.Client(), Store: store, Interval: time.Second}

	key0 := "header/" + multiName + "/0"
	key1 := "header/" + multiName + "/1"

	p.pollInfoWidget(t.Context(), key0, iw, entries[0], nil)
	p.pollInfoWidget(t.Context(), key1, iw, entries[1], nil)

	cards := map[string]Card{}
	for _, c := range store.Snapshot() {
		cards[c.Key] = c
	}
	if cards[key0].Err == "" {
		t.Errorf("entry 0 card.Err = empty, want a secret-resolution error")
	}
	if cards[key1].Err != "" {
		t.Errorf("entry 1 card.Err = %q, want empty (its own Secrets are unset)", cards[key1].Err)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("after first poll of entry 1, upstream hit %d times, want 1", n)
	}

	// entry 1's PollIntervalSeconds override isn't due yet; a second poll of
	// its own key must not hit the upstream again, independent of entry 0.
	p.pollInfoWidget(t.Context(), key1, iw, entries[1], nil)
	if n := hits.Load(); n != 1 {
		t.Errorf("after second (not-yet-due) poll of entry 1, upstream hit %d times, want still 1", n)
	}
}

func TestResolveSecretLiteralValue(t *testing.T) {
	p := &Poller{}
	value := "literal"
	got, err := p.resolveSecret(t.Context(), testNamespace, pagev1alpha1.SecretValueSource{Value: &value})
	if err != nil || got != value {
		t.Fatalf("resolveSecret(literal) = (%q, %v), want (%q, nil)", got, err, value)
	}
}

func TestResolveSecretNeitherValueNorRefSet(t *testing.T) {
	p := &Poller{}
	_, err := p.resolveSecret(t.Context(), testNamespace, pagev1alpha1.SecretValueSource{})
	if err == nil {
		t.Fatal("resolveSecret(neither set) = nil error, want non-nil")
	}
}

func TestResolveSecretKeyMissing(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: testSecretName, Namespace: testNamespace},
		Data:       map[string][]byte{"other": []byte("x")},
	}
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
	p := &Poller{SecretReader: cl}

	_, err := p.resolveSecret(t.Context(), testNamespace, pagev1alpha1.SecretValueSource{
		SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: testSecretName}, Key: testSecretField},
	})
	if err == nil {
		t.Fatal("resolveSecret(missing key) = nil error, want non-nil")
	}
}

func TestResolveSecretGetError(t *testing.T) {
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	failing := errInjectingReader{
		Reader:  cl,
		failGet: func(client.ObjectKey, client.Object) bool { return true },
	}
	p := &Poller{SecretReader: failing}

	_, err := p.resolveSecret(t.Context(), testNamespace, pagev1alpha1.SecretValueSource{
		SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: testSecretName}, Key: testSecretField},
	})
	if err == nil {
		t.Fatal("resolveSecret(Get error) = nil error, want non-nil")
	}
	if strings.Contains(err.Error(), "does not exist") {
		t.Errorf("resolveSecret(Get error) = %q, want the generic getting-Secret wrap, not the NotFound message (errBoom isn't an apierrors.IsNotFound error)", err.Error())
	}
}

func TestPollerSiteDefaultsAppliesStyle(t *testing.T) {
	scheme := testScheme(t)
	style := statusStyleBasic
	hide := false
	cfg := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardSpec{
			Style: &pagev1alpha1.StyleSpec{
				StatusStyle:  &style,
				ErrorDisplay: &hide,
			},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cfg).Build()
	p := &Poller{Reader: cl, Namespace: testNamespace, DashboardName: testDashboardName}

	statusStyle, hideErrors := p.siteDefaults(t.Context())
	if statusStyle != statusStyleBasic || !hideErrors {
		t.Errorf("siteDefaults() = (%q, %v), want (%q, true)", statusStyle, hideErrors, statusStyleBasic)
	}
}

func TestPollerSiteDefaultsNoStyle(t *testing.T) {
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	p := &Poller{Reader: cl, Namespace: testNamespace, DashboardName: testDashboardName}

	statusStyle, hideErrors := p.siteDefaults(t.Context())
	if statusStyle != statusStyleDot || hideErrors {
		t.Errorf("siteDefaults() = (%q, %v), want (%q, false)", statusStyle, hideErrors, statusStyleDot)
	}
}

func TestPollerSiteDefaultsGetError(t *testing.T) {
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	failing := errInjectingReader{
		Reader: cl,
		failGet: func(_ client.ObjectKey, obj client.Object) bool {
			_, ok := obj.(*pagev1alpha1.Dashboard)
			return ok
		},
	}
	p := &Poller{Reader: failing, Namespace: testNamespace, DashboardName: testDashboardName}

	statusStyle, hideErrors := p.siteDefaults(t.Context())
	if statusStyle != statusStyleDot || hideErrors {
		t.Errorf("siteDefaults() = (%q, %v), want (%q, false) on a Dashboard get error", statusStyle, hideErrors, statusStyleDot)
	}
}

func TestPollerMonitorUsesSiteDefaultStatusStyle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()

	entry := pagev1alpha1.ServiceCard{
		Spec: pagev1alpha1.ServiceCardSpec{
			Services: []pagev1alpha1.ServiceEntry{{
				Monitor: &srv.URL,
			}},
		},
	}
	p := &Poller{HTTPClient: srv.Client()}
	m := p.monitor(t.Context(), entry.Namespace, entry.Name, entry.Spec.Entries()[0], testStatusBasic, nil, defaultClusterDomain, func(string, string) {})
	if m.statusStyle != testStatusBasic {
		t.Errorf("monitor() style = %q, want the passed-in default %q when ServiceCard.StatusStyle is unset", m.statusStyle, testStatusBasic)
	}
}

func TestPollerPollWidgetUsesSiteDefaultHideErrors(t *testing.T) {
	entry := pagev1alpha1.ServiceCard{Spec: pagev1alpha1.ServiceCardSpec{
		Group: testGroup,
		Services: []pagev1alpha1.ServiceEntry{{
			Name: testSvcAName,
		}},
	}}
	widget := &pagev1alpha1.ServiceWidget{Type: testDoesNotExist}

	store := NewStore()
	p := &Poller{HTTPClient: http.DefaultClient, Store: store}
	p.pollWidget(t.Context(), testCardKeyA, entry.Namespace, entry.Spec.Entries()[0], widget, monitorProbeResult{}, true, nil)

	card := store.Snapshot()[0]
	if card.Err != "" {
		t.Errorf("card.Err = %q, want empty when the site-wide HideErrors default is true", card.Err)
	}
}

func TestPollerDiscoverySpecDisabledByDefault(t *testing.T) {
	scheme := testScheme(t)
	instance := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	p := &Poller{Reader: cl, Namespace: testNamespace, DashboardName: testDashboardName}

	if _, ok := p.discoverySpec(t.Context()); ok {
		t.Error("discoverySpec() ok = true, want false when Dashboard.Spec.Discovery is unset")
	}
}

func TestPollerDiscoverySpecEnabled(t *testing.T) {
	scheme := testScheme(t)
	instance := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardSpec{
			Discovery: &pagev1alpha1.DiscoverySpec{Enabled: true},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	p := &Poller{Reader: cl, Namespace: testNamespace, DashboardName: testDashboardName}

	spec, ok := p.discoverySpec(t.Context())
	if !ok || spec.Enabled != true {
		t.Errorf("discoverySpec() = (%+v, %v), want an enabled DiscoverySpec", spec, ok)
	}
}

func TestPollerDiscoverySpecMissingDashboard(t *testing.T) {
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	p := &Poller{Reader: cl, Namespace: testNamespace, DashboardName: testDashboardName}

	if _, ok := p.discoverySpec(t.Context()); ok {
		t.Error("discoverySpec() ok = true, want false when the Dashboard can't be read")
	}
}

func TestPollerWidgetDefaultsUnset(t *testing.T) {
	scheme := testScheme(t)
	instance := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	p := &Poller{Reader: cl, Namespace: testNamespace, DashboardName: testDashboardName}

	if defaults := p.widgetDefaults(t.Context()); defaults != nil {
		t.Errorf("widgetDefaults() = %+v, want nil when Dashboard.Spec.WidgetDefaults is unset", defaults)
	}
}

func TestPollerWidgetDefaultsSet(t *testing.T) {
	scheme := testScheme(t)
	instance := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardSpec{
			WidgetDefaults: map[string]pagev1alpha1.WidgetDefaultsEntry{
				testWidgetTypePlex: {Secrets: map[string]pagev1alpha1.SecretValueSource{testSecretField: *ptrSVS("x")}},
			},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	p := &Poller{Reader: cl, Namespace: testNamespace, DashboardName: testDashboardName}

	defaults := p.widgetDefaults(t.Context())
	if _, ok := defaults[testWidgetTypePlex]; !ok {
		t.Errorf("widgetDefaults() = %+v, want a \"plex\" entry", defaults)
	}
}

func TestPollerWidgetDefaultsMissingDashboard(t *testing.T) {
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	p := &Poller{Reader: cl, Namespace: testNamespace, DashboardName: testDashboardName}

	if defaults := p.widgetDefaults(t.Context()); defaults != nil {
		t.Errorf("widgetDefaults() = %+v, want nil when the Dashboard can't be read", defaults)
	}
}

func TestPollerMonitorNamespacesUnset(t *testing.T) {
	scheme := testScheme(t)
	instance := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	p := &Poller{Reader: cl, Namespace: testNamespace, DashboardName: testDashboardName}

	spec, ok := p.dashboardSpecForPoll(t.Context())
	if !ok {
		t.Fatal("dashboardSpecForPoll() ok = false, want true for an existing Dashboard")
	}
	if got := spec.MonitorNamespaces; got != nil {
		t.Errorf("dashboardSpecForPoll().MonitorNamespaces = %v, want nil when unset", got)
	}
}

func TestPollerMonitorNamespacesSet(t *testing.T) {
	scheme := testScheme(t)
	instance := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec:       pagev1alpha1.DashboardSpec{MonitorNamespaces: []string{"allowed-ns"}},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	p := &Poller{Reader: cl, Namespace: testNamespace, DashboardName: testDashboardName}

	spec, ok := p.dashboardSpecForPoll(t.Context())
	if !ok {
		t.Fatal("dashboardSpecForPoll() ok = false, want true for an existing Dashboard")
	}
	if got := spec.MonitorNamespaces; !slices.Equal(got, []string{"allowed-ns"}) {
		t.Errorf("dashboardSpecForPoll().MonitorNamespaces = %v, want [allowed-ns]", got)
	}
}

func TestPollerMonitorNamespacesMissingDashboard(t *testing.T) {
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	p := &Poller{Reader: cl, Namespace: testNamespace, DashboardName: testDashboardName}

	spec, ok := p.dashboardSpecForPoll(t.Context())
	if ok {
		t.Fatal("dashboardSpecForPoll() ok = true, want false when the Dashboard can't be read")
	}
	if got := spec.MonitorNamespaces; got != nil {
		t.Errorf("dashboardSpecForPoll().MonitorNamespaces = %v, want nil when the Dashboard can't be read", got)
	}
}

func TestPollerPollDiscoveredServiceStoresCard(t *testing.T) {
	svc := discoveredService{Key: "discovery/ns/app", Group: testDiscoveryGroup, Name: testDiscoveredAppName, Href: "https://app.invalid"}
	store := NewStore()
	p := &Poller{HTTPClient: http.DefaultClient, Store: store}
	p.pollDiscoveredService(t.Context(), svc, func(string, string) {})

	cards := store.Snapshot()
	if len(cards) != 1 || cards[0].ServiceName != testDiscoveredAppName || cards[0].Group != testDiscoveryGroup || cards[0].Status != "" {
		t.Fatalf("Snapshot() = %+v, want an unmonitored App card (monitor unset)", cards)
	}
}

func TestPollerPollDiscoveredServiceWithMonitorSetsStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()

	svc := discoveredService{Key: "discovery/ns/app", Group: testDiscoveryGroup, Name: testDiscoveredAppName, Href: srv.URL, Monitor: true}
	store := NewStore()
	p := &Poller{HTTPClient: srv.Client(), Store: store}

	var recorded string
	p.pollDiscoveredService(t.Context(), svc, func(label, _ string) { recorded = label })

	card := store.Snapshot()[0]
	if card.Status != "Up" || card.StatusStyle != statusStyleDot {
		t.Errorf("card = %+v, want Status=Up StatusStyle=dot", card)
	}
	if recorded == "" {
		t.Error("pollDiscoveredService() did not record a monitor label for a monitor-enabled discovered service")
	}
}

// failingRoundTripper fails the test immediately if RoundTrip is ever
// called, proving SampleData mode makes no real network calls.
type failingRoundTripper struct{ t *testing.T }

func (f failingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	f.t.Fatalf("unexpected network request in SampleData mode: %s %s", req.Method, req.URL)
	return nil, nil
}

// TestPollerSampleDataSkipsNetworkAndSecrets is SampleData mode's core
// contract test: a ServiceCard with a monitor, a widget referencing a
// nonexistent Secret, and an InfoWidget of a ClusterWidget type (kubemetrics,
// with KubeReader deliberately nil) must all render populated, error-free
// cards without ever dialing the network, resolving a secret, or touching
// KubeReader — see Poller.SampleData's doc comment.
func TestPollerSampleDataSkipsNetworkAndSecrets(t *testing.T) {
	monitorURL := testUnreachableAddr
	widgetURL := testUnreachableAddr
	entry := &pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: testSvcName, Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
			Group:        testGroup,
			Services: []pagev1alpha1.ServiceEntry{{
				Name:    testSvcDisplayName,
				Monitor: &monitorURL,
				Widgets: []pagev1alpha1.ServiceWidget{{
					Type: testWidgetType,
					URL:  &widgetURL,
					Secrets: map[string]pagev1alpha1.SecretValueSource{
						testSecretField: {SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "does-not-exist"},
							Key:                  testSecretField,
						}},
					},
				}},
			}},
		},
	}
	iw := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{{
				Type: testKubeMetricsType,
			}},
		},
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(entry, iw).Build()

	store := NewStore()
	p := &Poller{
		Reader: cl, SecretReader: cl, KubeReader: nil,
		Namespace: testNamespace, DashboardName: testDashboardName,
		Interval:   time.Hour,
		HTTPClient: &http.Client{Transport: failingRoundTripper{t: t}},
		Store:      store,
		SampleData: true,
	}
	p.pollOnce(t.Context())

	cards := store.Snapshot()
	if len(cards) != 2 {
		t.Fatalf("Snapshot() = %d cards, want 2 (one ServiceCard widget, one InfoWidget)", len(cards))
	}

	var serviceCard, headerCard Card
	for _, c := range cards {
		if c.Header {
			headerCard = c
		} else {
			serviceCard = c
		}
	}

	if serviceCard.Err != "" {
		t.Errorf("serviceCard.Err = %q, want empty (no real secret resolution attempted)", serviceCard.Err)
	}
	if serviceCard.Status != "Up" || serviceCard.Latency != sampleMonitorLatency {
		t.Errorf("serviceCard monitor = (%q, %q), want (Up, %q)", serviceCard.Status, serviceCard.Latency, sampleMonitorLatency)
	}
	if len(serviceCard.Fields) == 0 {
		t.Error("serviceCard.Fields = empty, want the prometheus widget's Sample output")
	}

	if headerCard.Err != "" {
		t.Errorf("headerCard.Err = %q, want empty (KubeReader is nil but never touched)", headerCard.Err)
	}
	if len(headerCard.Fields) == 0 {
		t.Error("headerCard.Fields = empty, want kubemetrics' Sample output")
	}
}

// TestPollerProbePodMonitorSampleData verifies probePodMonitor's SampleData
// branch: a fabricated "Up" status with no Reader/pod list at all, proving
// preview mode never needs pod RBAC for a PodSelector/App-monitored
// ServiceCard.
func TestPollerProbePodMonitorSampleData(t *testing.T) {
	entry := pagev1alpha1.ServiceCard{
		Spec: pagev1alpha1.ServiceCardSpec{
			Services: []pagev1alpha1.ServiceEntry{{
				PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{testAppLabelKey: testAppLabelValue}},
			}},
		},
	}
	p := &Poller{SampleData: true}
	se := entry.Spec.Entries()[0]

	status, text, cardErr := p.probePodMonitor(t.Context(), entry.Namespace, se, podMonitorSelector(se), nil)
	if status != "Up" || text != sampleMonitorReadyText || cardErr != "" {
		t.Errorf("probePodMonitor() = (%q, %q, %q), want (Up, %q, \"\")", status, text, cardErr, sampleMonitorReadyText)
	}
}

// TestPollerPollWidgetSampleUnsupportedType exercises pollWidgetSample's
// defensive "impl doesn't implement Sampler" branch — unreachable for any
// real registered widget (TestEveryRegisteredWidgetHasASample guarantees
// that), but still a real code path worth a direct test, mirroring
// TestPollerUnsupportedWidgetType's coverage of the analogous real-Poll
// branch in pollWidget.
func TestPollerPollWidgetSampleUnsupportedType(t *testing.T) {
	const stubType = "test-stub-no-sampler"
	Register(stubType, stubWidget{})
	t.Cleanup(func() { delete(registry, stubType) })

	url := testExampleURL
	entry := pagev1alpha1.ServiceCard{Spec: pagev1alpha1.ServiceCardSpec{
		Group: testGroup,
		Services: []pagev1alpha1.ServiceEntry{{
			Name: testSvcAName,
		}},
	}}
	widget := &pagev1alpha1.ServiceWidget{Type: stubType, URL: &url}

	store := NewStore()
	p := &Poller{Store: store, SampleData: true}
	p.pollWidget(t.Context(), testCardKeyA, entry.Namespace, entry.Spec.Entries()[0], widget, monitorProbeResult{}, false, nil)

	cards := store.Snapshot()
	if len(cards) != 1 || cards[0].Err == "" {
		t.Fatalf("Snapshot() = %+v, want one card with a non-empty Err for a widget type with no Sample method", cards)
	}
}

// TestPollerPollInfoWidgetSampleUnsupportedType is
// TestPollerPollWidgetSampleUnsupportedType's pollInfoWidget analog.
func TestPollerPollInfoWidgetSampleUnsupportedType(t *testing.T) {
	const stubType = "test-stub-no-sampler-info"
	Register(stubType, stubWidget{})
	t.Cleanup(func() { delete(registry, stubType) })

	iw := pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: "stub", Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{{
				Type: stubType,
			}},
		},
	}

	store := NewStore()
	p := &Poller{Store: store, SampleData: true}
	p.pollInfoWidget(t.Context(), "header/stub", iw, iw.Spec.Entries()[0], nil)

	cards := store.Snapshot()
	if len(cards) != 1 || cards[0].Err == "" {
		t.Fatalf("Snapshot() = %+v, want one card with a non-empty Err for a widget type with no Sample method", cards)
	}
}

func TestPollerPollOnceDiscoversIngresses(t *testing.T) {
	scheme := testScheme(t)
	instance := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardSpec{
			Discovery: &pagev1alpha1.DiscoverySpec{Enabled: true},
		},
	}
	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name: testDiscoveredAppKey, Namespace: testNamespace,
			Annotations: map[string]string{testDiscoveryEnabledAnnotation: annotationValueTrue, testKubepageNameAnnotation: testDiscoveredAppCardName},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, ing).Build()
	store := NewStore()
	p := &Poller{
		Reader: cl, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
		Interval: time.Hour, HTTPClient: http.DefaultClient, Store: store,
	}
	p.pollOnce(t.Context())

	cards := store.Snapshot()
	found := false
	for _, c := range cards {
		if c.ServiceName == testDiscoveredAppCardName {
			found = true
		}
	}
	if !found {
		t.Fatalf("Snapshot() = %+v, want a card for the annotated Ingress", cards)
	}
}

// TestPollerPollOnceDiscoveredServiceSampleDataSkipsNetwork closes the gap
// found in review: pollDiscoveredService is a separate poll path from
// pollWidget/pollInfoWidget/monitor, so it needs its own SampleData check.
// A monitor-enabled discovered Ingress must render a fabricated "Up" status
// without ever dialing the network or touching the monitorUp metric under
// SampleData — see probeURL's doc comment for the same guarantee on
// monitor-based probes.
func TestPollerPollOnceDiscoveredServiceSampleDataSkipsNetwork(t *testing.T) {
	scheme := testScheme(t)
	instance := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardSpec{
			Discovery: &pagev1alpha1.DiscoverySpec{Enabled: true},
		},
	}
	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name: testDiscoveredAppKey, Namespace: testNamespace,
			Annotations: map[string]string{
				testDiscoveryEnabledAnnotation:               annotationValueTrue,
				testKubepageNameAnnotation:                   testDiscoveredAppCardName,
				defaultDiscoveryPrefix + discoveryAnnHref:    testUnreachableAddr,
				defaultDiscoveryPrefix + discoveryAnnMonitor: annotationValueTrue,
			},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, ing).Build()
	store := NewStore()
	p := &Poller{
		Reader: cl, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
		Interval: time.Hour, HTTPClient: &http.Client{Transport: failingRoundTripper{t: t}}, Store: store,
		SampleData: true,
	}
	p.pollOnce(t.Context())

	var found *Card
	for _, c := range store.Snapshot() {
		if c.ServiceName == testDiscoveredAppCardName {
			c := c
			found = &c
		}
	}
	if found == nil {
		t.Fatalf("Snapshot() = %+v, want a card for the annotated Ingress", store.Snapshot())
	}
	if found.Status != "Up" || found.Latency != sampleMonitorLatency {
		t.Errorf("discovered card monitor = (%q, %q), want (Up, %q)", found.Status, found.Latency, sampleMonitorLatency)
	}
}

// TestPollerPollOnceDiscoversHTTPRoutesWhenGatewayAPIEnabled verifies
// pollOnce also discovers annotated HTTPRoutes when GatewayAPIEnabled is
// set (gap-analysis §4.7), reusing the same discovery-enabled Dashboard as
// Ingress discovery.
func TestPollerPollOnceDiscoversHTTPRoutesWhenGatewayAPIEnabled(t *testing.T) {
	scheme := testScheme(t)
	instance := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardSpec{
			Discovery: &pagev1alpha1.DiscoverySpec{
				Enabled: true,
				Sources: []string{pagev1alpha1.DiscoverySourceIngress, pagev1alpha1.DiscoverySourceHTTPRoute},
			},
		},
	}
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name: testDiscoveredAppKey, Namespace: testNamespace,
			Annotations: map[string]string{testDiscoveryEnabledAnnotation: annotationValueTrue, testKubepageNameAnnotation: testDiscoveredRouteCardName},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, route).Build()
	store := NewStore()
	p := &Poller{
		Reader: cl, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
		Interval: time.Hour, HTTPClient: http.DefaultClient, Store: store, GatewayAPIEnabled: true,
	}
	p.pollOnce(t.Context())

	found := false
	for _, c := range store.Snapshot() {
		if c.ServiceName == testDiscoveredRouteCardName {
			found = true
		}
	}
	if !found {
		t.Fatalf("Snapshot() = %+v, want a card for the annotated HTTPRoute", store.Snapshot())
	}
}

// TestPollerPollOnceSkipsHTTPRoutesWhenGatewayAPIDisabled verifies pollOnce
// never attempts HTTPRoute discovery when GatewayAPIEnabled is false, even
// though the Dashboard opts into the HTTPRoute source — a List against a
// nonexistent Kind would fail outright, so this must be a hard gate, not
// just missing RBAC.
func TestPollerPollOnceSkipsHTTPRoutesWhenGatewayAPIDisabled(t *testing.T) {
	scheme := testScheme(t)
	instance := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardSpec{
			Discovery: &pagev1alpha1.DiscoverySpec{
				Enabled: true,
				Sources: []string{pagev1alpha1.DiscoverySourceIngress, pagev1alpha1.DiscoverySourceHTTPRoute},
			},
		},
	}
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name: testDiscoveredAppKey, Namespace: testNamespace,
			Annotations: map[string]string{testDiscoveryEnabledAnnotation: annotationValueTrue, testKubepageNameAnnotation: testDiscoveredRouteCardName},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, route).Build()
	store := NewStore()
	p := &Poller{
		Reader: cl, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
		Interval: time.Hour, HTTPClient: http.DefaultClient, Store: store,
	}
	p.pollOnce(t.Context())

	for _, c := range store.Snapshot() {
		if c.ServiceName == testDiscoveredRouteCardName {
			t.Fatalf("Snapshot() = %+v, want no HTTPRoute card when GatewayAPIEnabled is false", store.Snapshot())
		}
	}
}

// TestPollerPollOnceSkipsHTTPRoutesWhenSourcesUnset verifies the default
// discovery.sources (unset, meaning ["Ingress"] only) never discovers
// HTTPRoutes even when GatewayAPIEnabled is true — issue #108's requirement
// that default behavior stays byte-identical to Ingress-only discovery
// unless a Dashboard explicitly opts into the HTTPRoute source.
func TestPollerPollOnceSkipsHTTPRoutesWhenSourcesUnset(t *testing.T) {
	scheme := testScheme(t)
	instance := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardSpec{
			Discovery: &pagev1alpha1.DiscoverySpec{Enabled: true},
		},
	}
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name: testDiscoveredAppKey, Namespace: testNamespace,
			Annotations: map[string]string{testDiscoveryEnabledAnnotation: annotationValueTrue, testKubepageNameAnnotation: testDiscoveredRouteCardName},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance, route).Build()
	store := NewStore()
	p := &Poller{
		Reader: cl, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
		Interval: time.Hour, HTTPClient: http.DefaultClient, Store: store, GatewayAPIEnabled: true,
	}
	p.pollOnce(t.Context())

	for _, c := range store.Snapshot() {
		if c.ServiceName == testDiscoveredRouteCardName {
			t.Fatalf("Snapshot() = %+v, want no HTTPRoute card when discovery.sources is unset", store.Snapshot())
		}
	}
}

// --- Combined HTTP + pod monitor tests (docs/design/combined-monitor.md) ---

// TestPollerCombinedMonitorFillsBothSlots verifies a services entry
// configuring both an HTTP monitor (monitor) and a pod monitor (app)
// populates Card.Status/Latency from the HTTP probe and Card.PodStatus/
// PodReadyText from the pod probe independently, in one poll cycle.
func TestPollerCombinedMonitorFillsBothSlots(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: testNamespace, Labels: map[string]string{podMonitorLabel: testAppLabelValue}},
		Status:     corev1.PodStatus{Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}},
	}
	app := testAppLabelValue
	entry := &pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: "combined", Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
			Group:        testGroup,
			Services: []pagev1alpha1.ServiceEntry{{
				Name:    testCombinedServiceName,
				Monitor: &srv.URL,
				App:     &app,
			}},
		},
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(entry, pod).Build()
	store := NewStore()
	p := &Poller{
		Reader: cl, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
		Interval: time.Hour, HTTPClient: srv.Client(), Store: store,
	}
	p.pollOnce(t.Context())

	cards := store.Snapshot()
	if len(cards) != 1 {
		t.Fatalf("Snapshot() = %d cards, want 1", len(cards))
	}
	c := cards[0]
	if c.Status != "Up" || c.Latency == "" {
		t.Errorf("card.Status/Latency = %q/%q, want Up/non-empty", c.Status, c.Latency)
	}
	if c.PodStatus != "Up" || c.PodReadyText != testPodReadyOneOfOne {
		t.Errorf("card.PodStatus/PodReadyText = %q/%q, want Up/%q", c.PodStatus, c.PodReadyText, testPodReadyOneOfOne)
	}
}

// TestPollerAppSelectorDerivation verifies podMonitorSelector derives the
// standard app.kubernetes.io/name=<app> selector from se.App when
// se.PodSelector is unset.
func TestPollerAppSelectorDerivation(t *testing.T) {
	app := testMyAppLabelValue
	se := pagev1alpha1.ServiceEntry{App: &app}
	sel := podMonitorSelector(se)
	if sel == nil {
		t.Fatal("podMonitorSelector(App set) = nil, want a derived selector")
	}
	if sel.MatchLabels[podMonitorLabel] != testMyAppLabelValue {
		t.Errorf("podMonitorSelector(App) = %+v, want app.kubernetes.io/name=%s", sel, testMyAppLabelValue)
	}
}

// TestPollerPodSelectorOverridesApp verifies podMonitorSelector prefers
// se.PodSelector over se.App when both are set (homepage's documented
// override semantics).
func TestPollerPodSelectorOverridesApp(t *testing.T) {
	app := testMyAppLabelValue
	explicit := &metav1.LabelSelector{MatchLabels: map[string]string{testCustomSelectorLabelKey: testCustomSelectorValue}}
	se := pagev1alpha1.ServiceEntry{App: &app, PodSelector: explicit}
	sel := podMonitorSelector(se)
	if sel != explicit {
		t.Errorf("podMonitorSelector(App+PodSelector) = %+v, want the explicit PodSelector to win", sel)
	}
}

// TestPollerPodMonitorForeignNamespaceAllowed verifies a pod monitor entry
// naming a namespace other than the ServiceCard's own succeeds, reading
// through KubeReader, when that namespace is listed in the Dashboard's
// spec.monitorNamespaces.
func TestPollerPodMonitorForeignNamespaceAllowed(t *testing.T) {
	const foreignNS = "other-ns"
	app := testAppLabelValue
	foreignNamespace := foreignNS
	se := pagev1alpha1.ServiceEntry{Name: "Foreign", App: &app, Namespace: &foreignNamespace}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: foreignNS, Labels: map[string]string{podMonitorLabel: testAppLabelValue}},
		Status:     corev1.PodStatus{Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}},
	}
	scheme := testScheme(t)
	kubeCl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
	p := &Poller{KubeReader: kubeCl}

	status, text, cardErr := p.probePodMonitor(t.Context(), testNamespace, se, podMonitorSelector(se), []string{foreignNS})
	if cardErr != "" {
		t.Errorf("probePodMonitor(allowed foreign namespace) cardErr = %q, want empty", cardErr)
	}
	if status != "Up" || text != testPodReadyOneOfOne {
		t.Errorf("probePodMonitor(allowed foreign namespace) = (%q, %q), want (Up, %q)", status, text, testPodReadyOneOfOne)
	}
}

// TestPollerPodMonitorForeignNamespaceDisallowed verifies a pod monitor
// entry naming a namespace not present in spec.monitorNamespaces
// short-circuits to Down with a card error naming the fix, never attempting
// an uncached List (and never surfacing a raw RBAC-forbidden error).
func TestPollerPodMonitorForeignNamespaceDisallowed(t *testing.T) {
	const foreignNS = "other-ns"
	app := testAppLabelValue
	foreignNamespace := foreignNS
	se := pagev1alpha1.ServiceEntry{Name: "Foreign", App: &app, Namespace: &foreignNamespace}

	p := &Poller{KubeReader: nil} // proves probePodMonitor never touches KubeReader here

	status, text, cardErr := p.probePodMonitor(t.Context(), testNamespace, se, podMonitorSelector(se), nil)
	if status != statusDown {
		t.Errorf("probePodMonitor(disallowed foreign namespace) status = %q, want %q", status, statusDown)
	}
	if text != noMatchedPodsReadyText {
		t.Errorf("probePodMonitor(disallowed foreign namespace) text = %q, want %q", text, noMatchedPodsReadyText)
	}
	if cardErr == "" || !strings.Contains(cardErr, foreignNS) || !strings.Contains(cardErr, "monitorNamespaces") {
		t.Errorf("probePodMonitor(disallowed foreign namespace) cardErr = %q, want it to name the namespace and spec.monitorNamespaces", cardErr)
	}
}

// TestPollerMonitorMetricsPerSourceAndPruning verifies monitor() records a
// distinct monitorUp series per (label, source) pair for a combined entry,
// and pruneMonitorMetrics deletes only the series a source that's since been
// removed reported, leaving the other source's series intact.
func TestPollerMonitorMetricsPerSourceAndPruning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: testNamespace, Labels: map[string]string{podMonitorLabel: testAppLabelValue}},
		Status:     corev1.PodStatus{Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}},
	}
	app := testAppLabelValue
	entry := &pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: "combined", Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
			Group:        testGroup,
			Services: []pagev1alpha1.ServiceEntry{{
				Name:    testCombinedServiceName,
				Monitor: &srv.URL,
				App:     &app,
			}},
		},
	}
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(entry, pod).Build()
	store := NewStore()
	p := &Poller{
		Reader: cl, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
		Interval: time.Hour, HTTPClient: srv.Client(), Store: store,
	}
	p.pollOnce(t.Context())

	label := testNamespace + "/combined/Combined"
	wantKeys := []string{monitorLabelKey(label, monitorSourceHTTP), monitorLabelKey(label, monitorSourcePods)}
	for _, k := range wantKeys {
		if !p.monitorLabels[k] {
			t.Errorf("monitorLabels[%q] = false after combined poll, want true", k)
		}
	}

	// Remove the pod monitor (App) from the entry, leaving only monitor,
	// and re-fetch it from the fake client so the update actually persists.
	var updated pagev1alpha1.ServiceCard
	if err := cl.Get(t.Context(), client.ObjectKeyFromObject(entry), &updated); err != nil {
		t.Fatalf("Get() updated ServiceCard: %v", err)
	}
	updated.Spec.Services[0].App = nil
	if err := cl.Update(t.Context(), &updated); err != nil {
		t.Fatalf("Update() ServiceCard: %v", err)
	}
	p.pollOnce(t.Context())

	if p.monitorLabels[monitorLabelKey(label, monitorSourcePods)] {
		t.Error("monitorLabels still tracks the pods source after App was removed, want it pruned")
	}
	if !p.monitorLabels[monitorLabelKey(label, monitorSourceHTTP)] {
		t.Error("monitorLabels dropped the http source, want it to remain tracked")
	}
}

// TestPollerWidgetURLInheritance covers a widget's base URL resolution
// precedence: an explicit widgets[].url wins, else the entry's internalUrl,
// else the entry's href (see ServiceEntry.BaseURL and pollWidget).
func TestPollerWidgetURLInheritance(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"activeTargets":[{"health":"up"}]}}`))
	}))
	defer srv.Close()

	unreachable := "http://unreachable.invalid/"
	cases := []struct {
		name  string
		entry pagev1alpha1.ServiceEntry
	}{
		{
			name: "explicit widget url wins over internalUrl and href",
			entry: pagev1alpha1.ServiceEntry{
				Name:        testSvcDisplayName,
				Href:        &unreachable,
				InternalURL: &unreachable,
				Widgets:     []pagev1alpha1.ServiceWidget{{Type: testWidgetType, URL: &srv.URL}},
			},
		},
		{
			name: "internalUrl inherited over href",
			entry: pagev1alpha1.ServiceEntry{
				Name:        testSvcDisplayName,
				Href:        &unreachable,
				InternalURL: &srv.URL,
				Widgets:     []pagev1alpha1.ServiceWidget{{Type: testWidgetType}},
			},
		},
		{
			name: "href inherited when nothing else is set",
			entry: pagev1alpha1.ServiceEntry{
				Name:    testSvcDisplayName,
				Href:    &srv.URL,
				Widgets: []pagev1alpha1.ServiceWidget{{Type: testWidgetType}},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry := &pagev1alpha1.ServiceCard{
				ObjectMeta: metav1.ObjectMeta{Name: "inherit", Namespace: testNamespace},
				Spec: pagev1alpha1.ServiceCardSpec{
					DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
					Group:        testGroup,
					Services:     []pagev1alpha1.ServiceEntry{tc.entry},
				},
			}
			scheme := testScheme(t)
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(entry).Build()
			store := NewStore()
			p := &Poller{
				Reader: cl, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
				Interval: time.Hour, HTTPClient: srv.Client(), Store: store,
			}
			p.pollOnce(t.Context())

			cards := store.Snapshot()
			if len(cards) != 1 {
				t.Fatalf("Snapshot() = %d cards, want 1", len(cards))
			}
			if cards[0].Err != "" {
				t.Errorf("card.Err = %q, want empty (widget should have polled the reachable URL)", cards[0].Err)
			}
			if len(cards[0].Fields) == 0 {
				t.Errorf("card.Fields empty, want fields from the resolved base URL poll")
			}
		})
	}
}

// TestPollerResolveAutoInternalURL covers "internalUrl: auto"'s Service
// lookup and URL derivation (see resolveAutoInternalURL/lookupAutoService):
// a named-Service hit, the label-selector fallback, zero/multiple-match card
// errors, named-port-vs-first-port selection, and a missing app.
func TestPollerResolveAutoInternalURL(t *testing.T) {
	auto := pagev1alpha1.InternalURLAuto
	app := testAppLabelValue

	namedSvc := func(name string, ports ...corev1.ServicePort) *corev1.Service {
		return &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
			Spec:       corev1.ServiceSpec{Ports: ports},
		}
	}
	labeledSvc := func(name string, ports ...corev1.ServicePort) *corev1.Service {
		svc := namedSvc(name, ports...)
		svc.Labels = map[string]string{podMonitorLabel: testAppLabelValue}
		return svc
	}

	cases := []struct {
		name     string
		se       pagev1alpha1.ServiceEntry
		objs     []client.Object
		wantURL  string
		wantErrs []string
	}{
		{
			name:    "named Service hit, first port when unnamed",
			se:      pagev1alpha1.ServiceEntry{Name: testSvcDisplayName, App: &app, InternalURL: &auto},
			objs:    []client.Object{namedSvc(testAppLabelValue, corev1.ServicePort{Port: 9090})},
			wantURL: fmt.Sprintf("http://%s.%s.svc.%s:9090", testAppLabelValue, testNamespace, defaultClusterDomain),
		},
		{
			name: "named port wins over first port",
			se:   pagev1alpha1.ServiceEntry{Name: testSvcDisplayName, App: &app, InternalURL: &auto},
			objs: []client.Object{namedSvc(testAppLabelValue,
				corev1.ServicePort{Name: "metrics", Port: 9100},
				corev1.ServicePort{Name: "http", Port: 8080},
			)},
			wantURL: fmt.Sprintf("http://%s.%s.svc.%s:8080", testAppLabelValue, testNamespace, defaultClusterDomain),
		},
		{
			name:    "label-selector fallback when no Service is named after app",
			se:      pagev1alpha1.ServiceEntry{Name: testSvcDisplayName, App: &app, InternalURL: &auto},
			objs:    []client.Object{labeledSvc("actual-svc-name", corev1.ServicePort{Port: 32400})},
			wantURL: fmt.Sprintf("http://actual-svc-name.%s.svc.%s:32400", testNamespace, defaultClusterDomain),
		},
		{
			name:     "zero matches renders a card error",
			se:       pagev1alpha1.ServiceEntry{Name: testSvcDisplayName, App: &app, InternalURL: &auto},
			objs:     nil,
			wantErrs: []string{"no Service", testAppLabelValue},
		},
		{
			name: "multiple label matches renders a card error naming the candidates",
			se:   pagev1alpha1.ServiceEntry{Name: testSvcDisplayName, App: &app, InternalURL: &auto},
			objs: []client.Object{
				labeledSvc("candidate-a", corev1.ServicePort{Port: 80}),
				labeledSvc("candidate-b", corev1.ServicePort{Port: 80}),
			},
			wantErrs: []string{"multiple", "candidate-a", "candidate-b"},
		},
		{
			name:     "no ports renders a card error",
			se:       pagev1alpha1.ServiceEntry{Name: testSvcDisplayName, App: &app, InternalURL: &auto},
			objs:     []client.Object{namedSvc(testAppLabelValue)},
			wantErrs: []string{"no ports"},
		},
		{
			name:     "missing app renders a card error",
			se:       pagev1alpha1.ServiceEntry{Name: testSvcDisplayName, InternalURL: &auto},
			objs:     []client.Object{namedSvc(testAppLabelValue, corev1.ServicePort{Port: 80})},
			wantErrs: []string{"requires app"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scheme := testScheme(t)
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.objs...).Build()
			p := &Poller{Reader: cl}

			url, cardErr := p.resolveBaseURL(t.Context(), testNamespace, tc.se, nil, defaultClusterDomain)
			if len(tc.wantErrs) > 0 {
				if url != "" {
					t.Errorf("resolveBaseURL() url = %q, want empty alongside a card error", url)
				}
				for _, want := range tc.wantErrs {
					if !strings.Contains(cardErr, want) {
						t.Errorf("resolveBaseURL() cardErr = %q, want it to contain %q", cardErr, want)
					}
				}
				return
			}
			if cardErr != "" {
				t.Errorf("resolveBaseURL() cardErr = %q, want empty", cardErr)
			}
			if url != tc.wantURL {
				t.Errorf("resolveBaseURL() url = %q, want %q", url, tc.wantURL)
			}
		})
	}
}

// TestPollerResolveAutoInternalURLCustomClusterDomain verifies a Dashboard's
// spec.clusterDomain overrides the "cluster.local" default in the resolved
// FQDN.
func TestPollerResolveAutoInternalURLCustomClusterDomain(t *testing.T) {
	app := testAppLabelValue
	auto := pagev1alpha1.InternalURLAuto
	se := pagev1alpha1.ServiceEntry{Name: testSvcDisplayName, App: &app, InternalURL: &auto}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: testAppLabelValue, Namespace: testNamespace},
			Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 32400}}},
		},
	).Build()
	p := &Poller{Reader: cl}

	const customDomain = "custom.internal"
	url, cardErr := p.resolveBaseURL(t.Context(), testNamespace, se, nil, customDomain)
	if cardErr != "" {
		t.Fatalf("resolveBaseURL() cardErr = %q, want empty", cardErr)
	}
	want := fmt.Sprintf("http://%s.%s.svc.%s:32400", testAppLabelValue, testNamespace, customDomain)
	if url != want {
		t.Errorf("resolveBaseURL() url = %q, want %q", url, want)
	}
}

// TestPollerResolveAutoInternalURLCrossNamespace covers "internalUrl: auto"
// under se.Namespace naming a namespace other than the ServiceCard's own:
// allowed (via spec.monitorNamespaces) resolves through KubeReader, and
// disallowed short-circuits to a card error, mirroring probePodMonitor's own
// cross-namespace gate exactly.
func TestPollerResolveAutoInternalURLCrossNamespace(t *testing.T) {
	const foreignNS = "other-ns"
	app := testAppLabelValue
	auto := pagev1alpha1.InternalURLAuto
	foreignNamespace := foreignNS
	se := pagev1alpha1.ServiceEntry{Name: testSvcDisplayName, App: &app, Namespace: &foreignNamespace, InternalURL: &auto}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: testAppLabelValue, Namespace: foreignNS},
		Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 32400}}},
	}

	t.Run("allowed foreign namespace resolves through KubeReader", func(t *testing.T) {
		scheme := testScheme(t)
		kubeCl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(svc).Build()
		p := &Poller{KubeReader: kubeCl}

		url, cardErr := p.resolveBaseURL(t.Context(), testNamespace, se, []string{foreignNS}, defaultClusterDomain)
		if cardErr != "" {
			t.Errorf("resolveBaseURL(allowed foreign namespace) cardErr = %q, want empty", cardErr)
		}
		want := fmt.Sprintf("http://%s.%s.svc.%s:32400", testAppLabelValue, foreignNS, defaultClusterDomain)
		if url != want {
			t.Errorf("resolveBaseURL(allowed foreign namespace) url = %q, want %q", url, want)
		}
	})

	t.Run("disallowed foreign namespace renders a card error", func(t *testing.T) {
		p := &Poller{KubeReader: nil} // proves resolveBaseURL never touches KubeReader here

		url, cardErr := p.resolveBaseURL(t.Context(), testNamespace, se, nil, defaultClusterDomain)
		if url != "" {
			t.Errorf("resolveBaseURL(disallowed foreign namespace) url = %q, want empty", url)
		}
		if cardErr == "" || !strings.Contains(cardErr, foreignNS) || !strings.Contains(cardErr, "monitorNamespaces") {
			t.Errorf("resolveBaseURL(disallowed foreign namespace) cardErr = %q, want it to name the namespace and spec.monitorNamespaces", cardErr)
		}
	})
}

// TestPollerPreviewResolveBaseURLIgnoresInternalURL covers Preview mode's
// core behavior change: internalUrl (explicit or the auto sentinel) is
// unreachable from a laptop, so resolveBaseURL ignores it entirely and falls
// back to href — without even attempting the "auto" Service lookup (proven
// here by a nil Reader/KubeReader that would panic if touched).
func TestPollerPreviewResolveBaseURLIgnoresInternalURL(t *testing.T) {
	href := "http://href.invalid/"
	internal := testInternalURLInvalid
	auto := pagev1alpha1.InternalURLAuto
	app := testAppLabelValue

	t.Run("explicit internalUrl ignored, falls back to href", func(t *testing.T) {
		se := pagev1alpha1.ServiceEntry{Name: testSvcDisplayName, Href: &href, InternalURL: &internal}
		p := &Poller{Preview: true}
		url, cardErr := p.resolveBaseURL(t.Context(), testNamespace, se, nil, defaultClusterDomain)
		if cardErr != "" {
			t.Errorf("resolveBaseURL(Preview, explicit internalUrl) cardErr = %q, want empty", cardErr)
		}
		if url != href {
			t.Errorf("resolveBaseURL(Preview, explicit internalUrl) = %q, want href %q", url, href)
		}
	})

	t.Run("auto sentinel ignored, no Service lookup attempted, falls back to href", func(t *testing.T) {
		se := pagev1alpha1.ServiceEntry{Name: testSvcDisplayName, Href: &href, InternalURL: &auto, App: &app}
		p := &Poller{Preview: true} // nil Reader/KubeReader: would panic if resolveAutoInternalURL ran
		url, cardErr := p.resolveBaseURL(t.Context(), testNamespace, se, nil, defaultClusterDomain)
		if cardErr != "" {
			t.Errorf("resolveBaseURL(Preview, auto internalUrl) cardErr = %q, want empty (no card error from ignored auto resolution)", cardErr)
		}
		if url != href {
			t.Errorf("resolveBaseURL(Preview, auto internalUrl) = %q, want href %q", url, href)
		}
	})

	t.Run("internalUrl ignored and no href resolves empty", func(t *testing.T) {
		se := pagev1alpha1.ServiceEntry{Name: testSvcDisplayName, InternalURL: &internal}
		p := &Poller{Preview: true}
		url, cardErr := p.resolveBaseURL(t.Context(), testNamespace, se, nil, defaultClusterDomain)
		if cardErr != "" || url != "" {
			t.Errorf("resolveBaseURL(Preview, internalUrl only) = (%q, %q), want (\"\", \"\")", url, cardErr)
		}
	})

	t.Run("non-Preview still resolves internalUrl normally", func(t *testing.T) {
		se := pagev1alpha1.ServiceEntry{Name: testSvcDisplayName, Href: &href, InternalURL: &internal}
		p := &Poller{}
		url, cardErr := p.resolveBaseURL(t.Context(), testNamespace, se, nil, defaultClusterDomain)
		if cardErr != "" || url != internal {
			t.Errorf("resolveBaseURL(non-Preview) = (%q, %q), want (%q, \"\")", url, cardErr, internal)
		}
	})
}

// TestPollerPreviewMonitorSelfFabricatesWhenInternalURLOnly covers the HTTP
// monitor gap Preview mode's internalUrl-ignoring leaves: "monitor: self"
// with only an internalUrl (no href) resolves its base URL to "" in Preview
// mode, so there's nothing reachable to probe — monitor fabricates the same
// "Up" result SampleData mode would, rather than skipping the card's status
// or probing an empty URL. An explicit monitor URL, or "self" with a usable
// href, still gets a real probe.
func TestPollerPreviewMonitorSelfFabricatesWhenInternalURLOnly(t *testing.T) {
	probed := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case probed <- struct{}{}:
		default:
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	monitorSelf := pagev1alpha1.MonitorSelf
	internal := testInternalURLInvalid

	t.Run("self with internalUrl only fabricates sample result", func(t *testing.T) {
		se := pagev1alpha1.ServiceEntry{Name: testSvcDisplayName, InternalURL: &internal, Monitor: &monitorSelf}
		p := &Poller{Preview: true} // no HTTPClient: would panic if a real probe were attempted
		m := p.monitor(t.Context(), testNamespace, "self-internal-only", se, statusStyleDot, nil, defaultClusterDomain, func(string, string) {})
		if m.status != "Up" || m.latency != sampleMonitorLatency {
			t.Errorf("monitor(Preview, self w/ internalUrl only) = (%q, %q), want (Up, %q)", m.status, m.latency, sampleMonitorLatency)
		}
	})

	t.Run("self with href still probes for real", func(t *testing.T) {
		se := pagev1alpha1.ServiceEntry{Name: testSvcDisplayName, Href: &srv.URL, Monitor: &monitorSelf}
		p := &Poller{Preview: true, HTTPClient: srv.Client()}
		m := p.monitor(t.Context(), testNamespace, "self-href", se, statusStyleDot, nil, defaultClusterDomain, func(string, string) {})
		if m.status != "Up" {
			t.Errorf("monitor(Preview, self w/ href) status = %q, want Up", m.status)
		}
		select {
		case <-probed:
		default:
			t.Error("monitor(Preview, self w/ href) never made a real probe")
		}
	})

	t.Run("explicit monitor URL still probes for real even with internalUrl-only base", func(t *testing.T) {
		se := pagev1alpha1.ServiceEntry{Name: testSvcDisplayName, InternalURL: &internal, Monitor: &srv.URL}
		p := &Poller{Preview: true, HTTPClient: srv.Client()}
		m := p.monitor(t.Context(), testNamespace, "explicit-monitor", se, statusStyleDot, nil, defaultClusterDomain, func(string, string) {})
		if m.status != "Up" {
			t.Errorf("monitor(Preview, explicit monitor URL) status = %q, want Up", m.status)
		}
		select {
		case <-probed:
		default:
			t.Error("monitor(Preview, explicit monitor URL) never made a real probe")
		}
	})
}

// TestPollerPreviewPodMonitorFabricatesSample covers Preview mode's pod
// monitor handling: unconditionally fabricated (unlike the HTTP monitor,
// this doesn't depend on internalUrl at all — preview simply has no cluster
// to list pods from), proven by a nil Reader that would panic if
// probePodMonitor's real pod-listing path ran.
func TestPollerPreviewPodMonitorFabricatesSample(t *testing.T) {
	app := testAppLabelValue
	se := pagev1alpha1.ServiceEntry{Name: testSvcDisplayName, App: &app}
	p := &Poller{Preview: true} // nil Reader: would panic if podStatus() ran
	m := p.monitor(t.Context(), testNamespace, "pod-preview", se, statusStyleDot, nil, defaultClusterDomain, func(string, string) {})
	if m.podStatus != "Up" || m.podReadyText != sampleMonitorReadyText {
		t.Errorf("monitor(Preview, pod monitor) = (%q, %q), want (Up, %q)", m.podStatus, m.podReadyText, sampleMonitorReadyText)
	}
}

// TestPollerPreviewMonitorDoesNotRecordFabricatedMetrics verifies fabricated
// Preview-mode monitor results (both the HTTP self-with-no-base-URL case and
// the always-fabricated pod monitor) don't get recorded into monitorLabels/
// the monitorUp gauge — mirroring SampleData's identical exclusion, since
// neither reflects an observed result.
func TestPollerPreviewMonitorDoesNotRecordFabricatedMetrics(t *testing.T) {
	monitorSelf := pagev1alpha1.MonitorSelf
	internal := testInternalURLInvalid
	app := testAppLabelValue
	entry := &pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: "preview-metrics", Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
			Group:        testGroup,
			Services: []pagev1alpha1.ServiceEntry{{
				Name:        testCombinedServiceName,
				InternalURL: &internal,
				Monitor:     &monitorSelf,
				App:         &app,
			}},
		},
	}
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(entry).Build()
	store := NewStore()
	p := &Poller{
		Reader: cl, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
		Interval: time.Hour, Store: store, Preview: true,
	}
	p.pollOnce(t.Context())

	cards := store.Snapshot()
	if len(cards) != 1 {
		t.Fatalf("Snapshot() = %d cards, want 1", len(cards))
	}
	if cards[0].Status != "Up" || cards[0].PodStatus != "Up" {
		t.Fatalf("card = %+v, want fabricated Up status for both monitor sources", cards[0])
	}

	label := testNamespace + "/preview-metrics/" + testCombinedServiceName
	for _, source := range []string{monitorSourceHTTP, monitorSourcePods} {
		if p.monitorLabels[monitorLabelKey(label, source)] {
			t.Errorf("monitorLabels[%q/%s] = true, want false (fabricated result shouldn't be recorded)", label, source)
		}
	}
}

// TestPollerPreviewWidgetFallsBackToSampleWhenNoURL covers the widget-poll
// gap Preview mode's internalUrl-ignoring leaves: a widget with no explicit
// url, on an entry whose only URL was the ignored internalUrl, has nothing
// reachable to poll — pollWidget falls back to the widget's Sample output
// instead of a doomed real request. A widget with an explicit url, or an
// entry with a usable href, still polls for real.
func TestPollerPreviewWidgetFallsBackToSampleWhenNoURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"activeTargets":[{"health":"up"}]}}`))
	}))
	defer srv.Close()

	internal := testInternalURLInvalid

	t.Run("internalUrl-only entry falls back to sample data", func(t *testing.T) {
		entry := &pagev1alpha1.ServiceCard{
			ObjectMeta: metav1.ObjectMeta{Name: "preview-widget-sample", Namespace: testNamespace},
			Spec: pagev1alpha1.ServiceCardSpec{
				DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
				Group:        testGroup,
				Services: []pagev1alpha1.ServiceEntry{{
					Name:        testSvcDisplayName,
					InternalURL: &internal,
					Widgets:     []pagev1alpha1.ServiceWidget{{Type: testWidgetType}},
				}},
			},
		}
		scheme := testScheme(t)
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(entry).Build()
		store := NewStore()
		p := &Poller{
			Reader: cl, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
			Interval:   time.Hour,
			HTTPClient: &http.Client{Transport: failingRoundTripper{t: t}},
			Store:      store,
			Preview:    true,
		}
		p.pollOnce(t.Context())

		cards := store.Snapshot()
		if len(cards) != 1 {
			t.Fatalf("Snapshot() = %d cards, want 1", len(cards))
		}
		if cards[0].Err != "" {
			t.Errorf("card.Err = %q, want empty (sample fallback, no real poll attempted)", cards[0].Err)
		}
		if len(cards[0].Fields) == 0 {
			t.Error("card.Fields = empty, want the widget's Sample output")
		}
	})

	t.Run("widget with explicit url still polls for real", func(t *testing.T) {
		entry := &pagev1alpha1.ServiceCard{
			ObjectMeta: metav1.ObjectMeta{Name: "preview-widget-real", Namespace: testNamespace},
			Spec: pagev1alpha1.ServiceCardSpec{
				DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
				Group:        testGroup,
				Services: []pagev1alpha1.ServiceEntry{{
					Name:        testSvcDisplayName,
					InternalURL: &internal,
					Widgets:     []pagev1alpha1.ServiceWidget{{Type: testWidgetType, URL: &srv.URL}},
				}},
			},
		}
		scheme := testScheme(t)
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(entry).Build()
		store := NewStore()
		p := &Poller{
			Reader: cl, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
			Interval: time.Hour, HTTPClient: srv.Client(), Store: store, Preview: true,
		}
		p.pollOnce(t.Context())

		cards := store.Snapshot()
		if len(cards) != 1 {
			t.Fatalf("Snapshot() = %d cards, want 1", len(cards))
		}
		if cards[0].Err != "" {
			t.Errorf("card.Err = %q, want empty (real poll of the explicit url should succeed)", cards[0].Err)
		}
		if len(cards[0].Fields) == 0 {
			t.Error("card.Fields = empty, want fields from the real poll")
		}
	})

	t.Run("href-derived base still polls for real", func(t *testing.T) {
		entry := &pagev1alpha1.ServiceCard{
			ObjectMeta: metav1.ObjectMeta{Name: "preview-widget-href", Namespace: testNamespace},
			Spec: pagev1alpha1.ServiceCardSpec{
				DashboardRef: &pagev1alpha1.DashboardRef{Name: testDashboardName},
				Group:        testGroup,
				Services: []pagev1alpha1.ServiceEntry{{
					Name:        testSvcDisplayName,
					InternalURL: &internal,
					Href:        &srv.URL,
					Widgets:     []pagev1alpha1.ServiceWidget{{Type: testWidgetType}},
				}},
			},
		}
		scheme := testScheme(t)
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(entry).Build()
		store := NewStore()
		p := &Poller{
			Reader: cl, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
			Interval: time.Hour, HTTPClient: srv.Client(), Store: store, Preview: true,
		}
		p.pollOnce(t.Context())

		cards := store.Snapshot()
		if len(cards) != 1 {
			t.Fatalf("Snapshot() = %d cards, want 1", len(cards))
		}
		if cards[0].Err != "" {
			t.Errorf("card.Err = %q, want empty (href-derived base should poll for real)", cards[0].Err)
		}
		if len(cards[0].Fields) == 0 {
			t.Error("card.Fields = empty, want fields from the real poll")
		}
	})
}

// TestPollerNonPreviewUnaffectedByPreviewLogic is a control: with Preview
// unset (the in-cluster dashboard's zero value), internalUrl still resolves
// and drives real polls exactly as before this feature — proving Preview's
// new branches are gated correctly and don't leak into the default path.
func TestPollerNonPreviewUnaffectedByPreviewLogic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	monitorSelf := pagev1alpha1.MonitorSelf
	se := pagev1alpha1.ServiceEntry{Name: testSvcDisplayName, InternalURL: &srv.URL, Monitor: &monitorSelf}
	p := &Poller{HTTPClient: srv.Client()} // Preview: false (zero value)
	m := p.monitor(t.Context(), testNamespace, "non-preview", se, statusStyleDot, nil, defaultClusterDomain, func(string, string) {})
	if m.status != "Up" {
		t.Errorf("monitor(non-Preview, self w/ internalUrl) status = %q, want Up (internalUrl should resolve and be probed for real)", m.status)
	}
}
