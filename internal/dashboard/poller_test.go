package dashboard

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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

	// testEphemeralAddr requests an OS-assigned port, for tests that don't
	// care which one they get.
	testEphemeralAddr = "127.0.0.1:0"
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
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
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
			DashboardRef: pagev1alpha1.DashboardRef{Name: "not-main"},
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
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
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
					Name:        testMultiEntryNameMonitored,
					SiteMonitor: &monSrv.URL,
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
		// pollOnce Gets the DashboardStyle (site-wide defaults, not counted
		// here) and lists ServiceCards and InfoWidgets once each.
		if n := listCalls.Load(); n != 2 {
			t.Fatalf("after the immediate poll, List was called %d times, want 2", n)
		}

		time.Sleep(10 * time.Second)
		synctest.Wait()
		if n := listCalls.Load(); n != 4 {
			t.Fatalf("after one Interval, List was called %d times, want 4 (one more poll)", n)
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
		ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
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
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
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
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Group:        "G",
			Services: []pagev1alpha1.ServiceEntry{{
				Name:        "Monitored",
				SiteMonitor: &srv.URL,
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

func TestPollerMonitorPingOnlyEntry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	entry := pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: "ping", Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Group:        "G",
			Services: []pagev1alpha1.ServiceEntry{{
				Name: "Pinged",
				Ping: &srv.URL,
			}},
		},
	}

	p := &Poller{HTTPClient: srv.Client()}
	status, style, latency := p.monitor(t.Context(), entry.Namespace, entry.Name, entry.Spec.Entries()[0], statusStyleDot, func(string) {})
	if status != "Up" {
		t.Errorf("monitor(Ping) status = %q, want Up", status)
	}
	if style != statusStyleDot {
		t.Errorf("monitor(Ping) style = %q, want default dot", style)
	}
	if latency == "" {
		t.Errorf("monitor(Ping) latency = empty, want non-empty")
	}
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

	status, text := p.podStatus(t.Context(), entry.Namespace, entry.Spec.Entries()[0])
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

	status, text := p.podStatus(t.Context(), entry.Namespace, entry.Spec.Entries()[0])
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
		{"no matching pods", nil, statusDown, "0/0 ready"},
		{"one ready pod", []client.Object{readyPod("p1", true)}, "Up", "1/1 ready"},
		{"one not-ready pod", []client.Object{readyPod("p1", false)}, statusDown, "0/1 ready"},
		{"mixed readiness reports any-ready as Up", []client.Object{readyPod("p1", false), readyPod("p2", true)}, "Up", "1/2 ready"},
		{"pod with no Ready condition at all", []client.Object{noReadyConditionPod("p1")}, statusDown, "0/1 ready"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			selector := &metav1.LabelSelector{MatchLabels: map[string]string{testAppLabelKey: testAppLabelValue}}
			style := testStatusBasic
			entry := &pagev1alpha1.ServiceCard{
				ObjectMeta: metav1.ObjectMeta{Name: testPodSvcName, Namespace: testNamespace},
				Spec: pagev1alpha1.ServiceCardSpec{
					DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
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
			if cards[0].Status != tc.wantStatus {
				t.Errorf("card.Status = %q, want %q", cards[0].Status, tc.wantStatus)
			}
			if cards[0].Latency != tc.wantText {
				t.Errorf("card.Latency = %q, want %q", cards[0].Latency, tc.wantText)
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

	showStats := pagev1alpha1.StatsHide
	hideErrors := pagev1alpha1.ErrorDisplayHidden
	url := srv.URL
	entry := &pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: "flags", Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
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

	// InfoWidget has no URL field; the openmeteo header widget reads its API
	// base from an Options "url" key (handled by pollInfoWidget), which keeps
	// this test hermetic against the httptest server.
	iw := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: testHeaderWeather, Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{{
				Type:    testOpenMeteoType,
				Options: &apiextensionsv1.JSON{Raw: []byte(`{"latitude":51.5,"longitude":-0.12,"url":"` + srv.URL + `"}`)},
			}},
		},
	}
	// A datetime InfoWidget carries no registered widget, so it must NOT
	// produce a polled card.
	dt := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: testClockName, Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
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
				DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
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
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
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
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
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
		ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: testNamespace},
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
	p.pollWidget(t.Context(), testCardKeyA, entry.Namespace, entry.Spec.Entries()[0], widget, "", "", "", false, nil)

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
// must not be polled (and must leave any existing card untouched), while one
// whose override has elapsed must be polled as normal.
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
	p.pollWidget(t.Context(), testCardKeyA, entry.Namespace, entry.Spec.Entries()[0], widget, "", "", "", false, nil)
	if n := hits.Load(); n != 1 {
		t.Fatalf("after first poll, upstream hit %d times, want 1", n)
	}
	if len(store.Snapshot()) != 1 {
		t.Fatalf("Snapshot() = %d cards, want 1", len(store.Snapshot()))
	}

	// Immediately polling again is within the 100s override: skipped.
	p.pollWidget(t.Context(), testCardKeyA, entry.Namespace, entry.Spec.Entries()[0], widget, "", "", "", false, nil)
	if n := hits.Load(); n != 1 {
		t.Errorf("after second (not-yet-due) poll, upstream hit %d times, want still 1", n)
	}

	// Back-date the last-polled time past the override: due again.
	p.widgetLastPolledMu.Lock()
	p.widgetLastPolled[testCardKeyA] = time.Now().Add(-101 * time.Second)
	p.widgetLastPolledMu.Unlock()
	p.pollWidget(t.Context(), testCardKeyA, entry.Namespace, entry.Spec.Entries()[0], widget, "", "", "", false, nil)
	if n := hits.Load(); n != 2 {
		t.Errorf("after third (due) poll, upstream hit %d times, want 2", n)
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
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{{
				Type:                testOpenMeteoType,
				Options:             &apiextensionsv1.JSON{Raw: []byte(`{"latitude":51.5,"longitude":-0.12,"url":"` + srv.URL + `"}`)},
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

// TestPollerPruneWidgetLastPolledRemovesUnkept verifies pruneWidgetLastPolled
// mirrors Store.Prune: an entry not in this cycle's keep set is dropped, so
// a deleted or edited-away-from-an-override widget's bookkeeping doesn't
// accumulate forever.
func TestPollerPruneWidgetLastPolledRemovesUnkept(t *testing.T) {
	p := &Poller{
		widgetLastPolled: map[string]time.Time{"keep": time.Now(), "drop": time.Now()},
	}
	p.pruneWidgetLastPolled(map[string]bool{"keep": true})

	if _, ok := p.widgetLastPolled["keep"]; !ok {
		t.Error("pruneWidgetLastPolled removed a kept key")
	}
	if _, ok := p.widgetLastPolled["drop"]; ok {
		t.Error("pruneWidgetLastPolled did not remove an unkept key")
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
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
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
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
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
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
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

// TestPollerInfoWidgetURLFieldBeatsOptionsURL verifies InfoWidgetEntry.URL
// takes precedence over options' "url" key when both are set: the entry's
// typed URL points at a working server while options.url points at an
// address nothing listens on, so a successful poll proves the typed field
// won.
func TestPollerInfoWidgetURLFieldBeatsOptionsURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"current_weather":{"temperature":9,"weathercode":0}}`))
	}))
	defer srv.Close()

	entryURL := srv.URL
	iw := pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: testHeaderWeather, Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{{
				Type:    testOpenMeteoType,
				URL:     &entryURL,
				Options: &apiextensionsv1.JSON{Raw: []byte(`{"latitude":51.5,"longitude":-0.12,"url":"http://127.0.0.1:1"}`)},
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
		t.Errorf("card.Err = %q, want empty (entry.URL should have been used instead of the unreachable options.url)", cards[0].Err)
	}
}

// TestPollerInfoWidgetOptionsURLStillWorksAlone verifies options' "url" key
// alone (entry.URL unset) still resolves the widget's base URL, preserving
// backwards compatibility for InfoWidgets written before the typed URL field
// existed.
func TestPollerInfoWidgetOptionsURLStillWorksAlone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"current_weather":{"temperature":9,"weathercode":0}}`))
	}))
	defer srv.Close()

	iw := pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: testHeaderWeather, Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{{
				Type:    testOpenMeteoType,
				Options: &apiextensionsv1.JSON{Raw: []byte(`{"latitude":51.5,"longitude":-0.12,"url":"` + srv.URL + `"}`)},
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
		t.Errorf("card.Err = %q, want empty (options.url alone must still resolve the widget's base URL)", cards[0].Err)
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
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Widgets: []pagev1alpha1.InfoWidgetEntry{
				{
					Type:    testOpenMeteoType,
					Options: &apiextensionsv1.JSON{Raw: []byte(`{"latitude":51.5,"longitude":-0.12,"url":"` + srv.URL + `"}`)},
				},
				{
					Type:    testOpenMeteoType,
					Options: &apiextensionsv1.JSON{Raw: []byte(`{"latitude":40.7,"longitude":-74.0,"url":"` + srv.URL + `"}`)},
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
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
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
					Options:             &apiextensionsv1.JSON{Raw: []byte(`{"latitude":51.5,"longitude":-0.12,"url":"` + srv.URL + `"}`)},
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

func TestPollerSiteDefaultsAppliesDashboardStyle(t *testing.T) {
	scheme := testScheme(t)
	style := testStatusBasic
	hide := pagev1alpha1.ErrorDisplayHidden
	cfg := &pagev1alpha1.DashboardStyle{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardStyleSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			StatusStyle:  &style,
			ErrorDisplay: &hide,
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cfg).Build()
	p := &Poller{Reader: cl, Namespace: testNamespace, DashboardName: testDashboardName}

	statusStyle, hideErrors := p.siteDefaults(t.Context())
	if statusStyle != style || !hideErrors {
		t.Errorf("siteDefaults() = (%q, %v), want (%q, true)", statusStyle, hideErrors, style)
	}
}

func TestPollerSiteDefaultsNoDashboardStyle(t *testing.T) {
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
			_, ok := obj.(*pagev1alpha1.DashboardStyle)
			return ok
		},
	}
	p := &Poller{Reader: failing, Namespace: testNamespace, DashboardName: testDashboardName}

	statusStyle, hideErrors := p.siteDefaults(t.Context())
	if statusStyle != statusStyleDot || hideErrors {
		t.Errorf("siteDefaults() = (%q, %v), want (%q, false) on a DashboardStyle get error", statusStyle, hideErrors, statusStyleDot)
	}
}

func TestPollerMonitorUsesSiteDefaultStatusStyle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()

	entry := pagev1alpha1.ServiceCard{
		Spec: pagev1alpha1.ServiceCardSpec{
			Services: []pagev1alpha1.ServiceEntry{{
				Ping: &srv.URL,
			}},
		},
	}
	p := &Poller{HTTPClient: srv.Client()}
	_, style, _ := p.monitor(t.Context(), entry.Namespace, entry.Name, entry.Spec.Entries()[0], testStatusBasic, func(string) {})
	if style != testStatusBasic {
		t.Errorf("monitor() style = %q, want the passed-in default %q when ServiceCard.StatusStyle is unset", style, testStatusBasic)
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
	p.pollWidget(t.Context(), testCardKeyA, entry.Namespace, entry.Spec.Entries()[0], widget, "", "", "", true, nil)

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
			Discovery: &pagev1alpha1.DiscoverySpec{Enabled: pagev1alpha1.Enabled},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()
	p := &Poller{Reader: cl, Namespace: testNamespace, DashboardName: testDashboardName}

	spec, ok := p.discoverySpec(t.Context())
	if !ok || spec.Enabled != pagev1alpha1.Enabled {
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

func TestPollerPollDiscoveredServiceStoresCard(t *testing.T) {
	svc := discoveredService{Key: "discovery/ns/app", Group: testDiscoveryGroup, Name: testDiscoveredAppName, Href: "https://app.invalid"}
	store := NewStore()
	p := &Poller{HTTPClient: http.DefaultClient, Store: store}
	p.pollDiscoveredService(t.Context(), svc, func(string) {})

	cards := store.Snapshot()
	if len(cards) != 1 || cards[0].ServiceName != testDiscoveredAppName || cards[0].Group != testDiscoveryGroup || cards[0].Status != "" {
		t.Fatalf("Snapshot() = %+v, want an unmonitored App card (Ping unset)", cards)
	}
}

func TestPollerPollDiscoveredServiceWithPingSetsStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()

	svc := discoveredService{Key: "discovery/ns/app", Group: testDiscoveryGroup, Name: testDiscoveredAppName, Href: srv.URL, Ping: true}
	store := NewStore()
	p := &Poller{HTTPClient: srv.Client(), Store: store}

	var recorded string
	p.pollDiscoveredService(t.Context(), svc, func(label string) { recorded = label })

	card := store.Snapshot()[0]
	if card.Status != "Up" || card.StatusStyle != statusStyleDot {
		t.Errorf("card = %+v, want Status=Up StatusStyle=dot", card)
	}
	if recorded == "" {
		t.Error("pollDiscoveredService() did not record a monitor label for a Ping-enabled discovered service")
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
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Group:        testGroup,
			Services: []pagev1alpha1.ServiceEntry{{
				Name:        testSvcDisplayName,
				SiteMonitor: &monitorURL,
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
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
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

// TestPollerProbePodSelectorSampleData verifies probePodSelector's
// SampleData branch: a fabricated "Up" status with no Reader/pod list at
// all, proving preview mode never needs pod RBAC for a PodSelector-monitored
// ServiceCard.
func TestPollerProbePodSelectorSampleData(t *testing.T) {
	entry := pagev1alpha1.ServiceCard{
		Spec: pagev1alpha1.ServiceCardSpec{
			Services: []pagev1alpha1.ServiceEntry{{
				PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{testAppLabelKey: testAppLabelValue}},
			}},
		},
	}
	p := &Poller{SampleData: true}

	status, text := p.probePodSelector(t.Context(), entry.Namespace, entry.Spec.Entries()[0])
	if status != "Up" || text != sampleMonitorReadyText {
		t.Errorf("probePodSelector() = (%q, %q), want (Up, %q)", status, text, sampleMonitorReadyText)
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
	p.pollWidget(t.Context(), testCardKeyA, entry.Namespace, entry.Spec.Entries()[0], widget, "", "", "", false, nil)

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
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
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
			Discovery: &pagev1alpha1.DiscoverySpec{Enabled: pagev1alpha1.Enabled},
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
// A Ping-enabled discovered Ingress must render a fabricated "Up" status
// without ever dialing the network or touching the monitorUp metric under
// SampleData — see probeURL's doc comment for the same guarantee on
// monitor-based probes.
func TestPollerPollOnceDiscoveredServiceSampleDataSkipsNetwork(t *testing.T) {
	scheme := testScheme(t)
	instance := &pagev1alpha1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{Name: testDashboardName, Namespace: testNamespace},
		Spec: pagev1alpha1.DashboardSpec{
			Discovery: &pagev1alpha1.DiscoverySpec{Enabled: pagev1alpha1.Enabled},
		},
	}
	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name: testDiscoveredAppKey, Namespace: testNamespace,
			Annotations: map[string]string{
				testDiscoveryEnabledAnnotation:            annotationValueTrue,
				testKubepageNameAnnotation:                testDiscoveredAppCardName,
				defaultDiscoveryPrefix + discoveryAnnHref: testUnreachableAddr,
				defaultDiscoveryPrefix + discoveryAnnPing: annotationValueTrue,
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
				Enabled: pagev1alpha1.Enabled,
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
				Enabled: pagev1alpha1.Enabled,
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
			Discovery: &pagev1alpha1.DiscoverySpec{Enabled: pagev1alpha1.Enabled},
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
