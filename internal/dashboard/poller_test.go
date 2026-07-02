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
	testNamespace     = "ns"
	testDashboardName = "main"
	testGroup         = "Monitoring"
	testServiceName   = "Prometheus"
	testSvcAName      = "Svc A"
	testCardKeyA      = "ns/a/0"
	testWidgetType    = "prometheus"
	testSecretField   = "token"
	testBookmarkGroup = "Reading"
	testOtherGroup    = "Other"
	testTab1          = "Tab1"
	testTab2          = "Tab2"
	testInfraTab      = "Infrastructure"
	testStyleRow      = "row"
	testColor         = "blue"
	testDoesNotExist  = "does-not-exist"
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
			Name:         testServiceName,
			Href:         &href,
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
		},
	}

	otherDashboard := &pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: "not-main"},
			Group:        testOtherGroup,
			Name:         "Skip me",
			Widgets:      []pagev1alpha1.ServiceWidget{{Type: testWidgetType, URL: &url}},
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
		ctx, cancel := context.WithCancel(context.Background())
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
			Name:         testSvcDisplayName,
			Widgets:      []pagev1alpha1.ServiceWidget{{Type: testWidgetType, URL: &url}},
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
	if len(cards) != 1 || cards[0].Key != "ns/svc/0" {
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
			Name:         "Mystery",
			Widgets:      []pagev1alpha1.ServiceWidget{{Type: testDoesNotExist, URL: &url}},
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
			Name:         "Monitored",
			SiteMonitor:  &srv.URL,
			StatusStyle:  &style,
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
			Name:         "Pinged",
			Ping:         &srv.URL,
		},
	}

	p := &Poller{HTTPClient: srv.Client()}
	status, style, latency := p.monitor(t.Context(), entry, statusStyleDot, func(string) {})
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
			PodSelector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{{Key: testAppLabelKey, Operator: testBogusWhen, Values: []string{"x"}}},
			},
		},
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	p := &Poller{
		Reader: cl, SecretReader: cl, Namespace: testNamespace, DashboardName: testDashboardName,
		Interval: time.Hour, HTTPClient: http.DefaultClient, Store: NewStore(),
	}

	status, text := p.podStatus(t.Context(), entry)
	if status != statusDown || text != "" {
		t.Errorf("podStatus(invalid selector) = (%q, %q), want (%q, \"\")", status, text, statusDown)
	}
}

func TestPollerPodStatusListPodsError(t *testing.T) {
	entry := pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: testPodSvcName, Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{testAppLabelKey: testAppLabelValue}},
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

	status, text := p.podStatus(t.Context(), entry)
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
					Name:         "PodService",
					PodSelector:  selector,
					StatusStyle:  &style,
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
			Name:         "Flags",
			ShowStats:    &showStats,
			ErrorDisplay: &hideErrors,
			Widgets:      []pagev1alpha1.ServiceWidget{{Type: testWidgetType, URL: &url}},
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
			Type:         testOpenMeteoType,
			Options:      &apiextensionsv1.JSON{Raw: []byte(`{"latitude":51.5,"longitude":-0.12,"url":"` + srv.URL + `"}`)},
		},
	}
	// A datetime InfoWidget carries no registered widget, so it must NOT
	// produce a polled card.
	dt := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: testClockName, Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			DashboardRef: pagev1alpha1.DashboardRef{Name: testDashboardName},
			Type:         headerTypeDatetime,
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
				Name:         fmt.Sprintf("Service %d", i),
				Widgets:      []pagev1alpha1.ServiceWidget{{Type: testWidgetType, URL: &url}},
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
			Name:         testSvcDisplayName,
			Widgets: []pagev1alpha1.ServiceWidget{{
				Type: testWidgetType,
				URL:  &url,
				Secrets: map[string]pagev1alpha1.SecretValueSource{
					testSecretField: {SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "missing"},
						Key:                  testSecretField,
					}},
				},
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
			Group:       testGroup,
			Name:        testSvcAName,
			Description: &description,
			Target:      &target,
		},
	}
	widget := &pagev1alpha1.ServiceWidget{
		Type:   "prometheusmetric",
		URL:    &url,
		Config: &apiextensionsv1.JSON{Raw: []byte(`{"query":"up","label":"Custom"}`)},
	}

	store := NewStore()
	p := &Poller{HTTPClient: srv.Client(), Store: store}
	p.pollWidget(t.Context(), testCardKeyA, entry, widget, "", "", "", false)

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
		Spec:       pagev1alpha1.ServiceCardSpec{Group: testGroup, Name: testSvcAName},
	}
	widget := &pagev1alpha1.ServiceWidget{Type: testWidgetType, URL: &url, PollIntervalSeconds: &overrideSeconds}

	store := NewStore()
	p := &Poller{HTTPClient: srv.Client(), Store: store, Interval: time.Second}

	// First poll of key is always due, regardless of override.
	p.pollWidget(t.Context(), testCardKeyA, entry, widget, "", "", "", false)
	if n := hits.Load(); n != 1 {
		t.Fatalf("after first poll, upstream hit %d times, want 1", n)
	}
	if len(store.Snapshot()) != 1 {
		t.Fatalf("Snapshot() = %d cards, want 1", len(store.Snapshot()))
	}

	// Immediately polling again is within the 100s override: skipped.
	p.pollWidget(t.Context(), testCardKeyA, entry, widget, "", "", "", false)
	if n := hits.Load(); n != 1 {
		t.Errorf("after second (not-yet-due) poll, upstream hit %d times, want still 1", n)
	}

	// Back-date the last-polled time past the override: due again.
	p.widgetLastPolledMu.Lock()
	p.widgetLastPolled[testCardKeyA] = time.Now().Add(-101 * time.Second)
	p.widgetLastPolledMu.Unlock()
	p.pollWidget(t.Context(), testCardKeyA, entry, widget, "", "", "", false)
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
			DashboardRef:        pagev1alpha1.DashboardRef{Name: testDashboardName},
			Type:                testOpenMeteoType,
			Options:             &apiextensionsv1.JSON{Raw: []byte(`{"latitude":51.5,"longitude":-0.12,"url":"` + srv.URL + `"}`)},
			PollIntervalSeconds: &overrideSeconds,
		},
	}

	const key = "header/" + testHeaderWeather
	store := NewStore()
	p := &Poller{HTTPClient: srv.Client(), Store: store, Interval: time.Second}

	p.pollInfoWidget(t.Context(), key, iw)
	if n := hits.Load(); n != 1 {
		t.Fatalf("after first poll, upstream hit %d times, want 1", n)
	}

	p.pollInfoWidget(t.Context(), key, iw)
	if n := hits.Load(); n != 1 {
		t.Errorf("after second (not-yet-due) poll, upstream hit %d times, want still 1", n)
	}

	p.widgetLastPolledMu.Lock()
	p.widgetLastPolled[key] = time.Now().Add(-101 * time.Second)
	p.widgetLastPolledMu.Unlock()
	p.pollInfoWidget(t.Context(), key, iw)
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
		name   string
		fields []Field
	}{
		{"unreachable status", []Field{{Label: labelStatus, Value: statusUnreach}}},
		{"http error status", []Field{{Label: labelStatus, Value: testHTTP500}}},
		{"healthy status is not an error", []Field{{Label: labelStatus, Value: statusHealthy}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := metricErr(nil, tc.fields)
			wantErr := tc.name != "healthy status is not an error"
			if (err != nil) != wantErr {
				t.Errorf("metricErr(nil, %+v) = %v, want error presence %v", tc.fields, err, wantErr)
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
			Type:         testOpenMeteoType,
			Secrets: map[string]pagev1alpha1.SecretValueSource{
				testSecretField: {SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "missing"},
					Key:                  testSecretField,
				}},
			},
		},
	}

	store := NewStore()
	p := &Poller{SecretReader: cl, Store: store}
	p.pollInfoWidget(t.Context(), "header/weather", iw)

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
			Type:         testKubeMetricsType,
		},
	}

	store := NewStore()
	p := &Poller{KubeReader: kubeCl, Store: store}
	p.pollInfoWidget(t.Context(), "header/cluster", iw)

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
			Type:         "prometheusmetric",
		},
	}

	store := NewStore()
	p := &Poller{HTTPClient: http.DefaultClient, Store: store}
	p.pollInfoWidget(t.Context(), "header/metric", iw)

	cards := store.Snapshot()
	if len(cards) != 1 || cards[0].Err == "" {
		t.Fatalf("Snapshot() = %+v, want one card with a non-empty Err (prometheusmetric requires a URL)", cards)
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
		Spec: pagev1alpha1.ServiceCardSpec{Ping: &srv.URL},
	}
	p := &Poller{HTTPClient: srv.Client()}
	_, style, _ := p.monitor(t.Context(), entry, testStatusBasic, func(string) {})
	if style != testStatusBasic {
		t.Errorf("monitor() style = %q, want the passed-in default %q when ServiceCard.StatusStyle is unset", style, testStatusBasic)
	}
}

func TestPollerPollWidgetUsesSiteDefaultHideErrors(t *testing.T) {
	entry := pagev1alpha1.ServiceCard{Spec: pagev1alpha1.ServiceCardSpec{Group: testGroup, Name: testSvcAName}}
	widget := &pagev1alpha1.ServiceWidget{Type: testDoesNotExist}

	store := NewStore()
	p := &Poller{HTTPClient: http.DefaultClient, Store: store}
	p.pollWidget(t.Context(), testCardKeyA, entry, widget, "", "", "", true)

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

// TestPollerPollOnceDiscoversHTTPRoutesWhenGatewayAPIEnabled verifies
// pollOnce also discovers annotated HTTPRoutes when GatewayAPIEnabled is
// set (gap-analysis §4.7), reusing the same discovery-enabled Dashboard as
// Ingress discovery.
func TestPollerPollOnceDiscoversHTTPRoutesWhenGatewayAPIEnabled(t *testing.T) {
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
// never attempts HTTPRoute discovery when GatewayAPIEnabled is false (the
// default) — a List against a nonexistent Kind would fail outright, so this
// must be a hard gate, not just missing RBAC.
func TestPollerPollOnceSkipsHTTPRoutesWhenGatewayAPIDisabled(t *testing.T) {
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
		Interval: time.Hour, HTTPClient: http.DefaultClient, Store: store,
	}
	p.pollOnce(t.Context())

	for _, c := range store.Snapshot() {
		if c.ServiceName == testDiscoveredRouteCardName {
			t.Fatalf("Snapshot() = %+v, want no HTTPRoute card when GatewayAPIEnabled is false", store.Snapshot())
		}
	}
}
