package dashboard

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

const (
	testNamespace     = "ns"
	testInstanceName  = "main"
	testGroup         = "Monitoring"
	testServiceName   = "Prometheus"
	testWidgetType    = "prometheus"
	testSecretField   = "token"
	testBookmarkGroup = "Reading"
	testOtherGroup    = "Other"
	testTab1          = "Tab1"
	testTab2          = "Tab2"
	testInfraTab      = "Infrastructure"
	testStyleRow      = "row"
	testColor         = "blue"
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
	return scheme
}

func TestPollerPollOnce(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"activeTargets":[{"health":"up"}]}}`))
	}))
	defer srv.Close()

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "creds", Namespace: testNamespace},
		Data:       map[string][]byte{testSecretField: []byte("s3cr3t")},
	}

	url := srv.URL
	href := "https://prometheus.example.com"
	entry := &pagev1alpha1.ServiceEntry{
		ObjectMeta: metav1.ObjectMeta{Name: "prom", Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceEntrySpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Group:       testGroup,
			Name:        testServiceName,
			Href:        &href,
			Widgets: []pagev1alpha1.ServiceWidget{
				{
					Type: testWidgetType,
					URL:  &url,
					Secrets: map[string]pagev1alpha1.SecretValueSource{
						testSecretField: {SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "creds"},
							Key:                  testSecretField,
						}},
					},
				},
			},
		},
	}

	otherInstance := &pagev1alpha1.ServiceEntry{
		ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceEntrySpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: "not-main"},
			Group:       testOtherGroup,
			Name:        "Skip me",
			Widgets:     []pagev1alpha1.ServiceWidget{{Type: testWidgetType, URL: &url}},
		},
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret, entry, otherInstance).Build()

	store := NewStore()
	p := &Poller{
		Reader:       cl,
		SecretReader: cl,
		Namespace:    testNamespace,
		InstanceName: testInstanceName,
		Interval:     time.Hour,
		HTTPClient:   srv.Client(),
		Store:        store,
	}

	p.pollOnce(context.Background())

	cards := store.Snapshot()
	if len(cards) != 1 {
		t.Fatalf("Snapshot() returned %d cards, want 1 (bound only to InstanceRef %q)", len(cards), testInstanceName)
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

func TestPollerUnsupportedWidgetType(t *testing.T) {
	url := "http://example.invalid"
	entry := &pagev1alpha1.ServiceEntry{
		ObjectMeta: metav1.ObjectMeta{Name: "mystery", Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceEntrySpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Group:       "G",
			Name:        "Mystery",
			Widgets:     []pagev1alpha1.ServiceWidget{{Type: "does-not-exist", URL: &url}},
		},
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(entry).Build()

	store := NewStore()
	p := &Poller{
		Reader: cl, SecretReader: cl, Namespace: testNamespace, InstanceName: testInstanceName,
		Interval: time.Hour, HTTPClient: http.DefaultClient, Store: store,
	}
	p.pollOnce(context.Background())

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
	entry := &pagev1alpha1.ServiceEntry{
		ObjectMeta: metav1.ObjectMeta{Name: "mon", Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceEntrySpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Group:       "G",
			Name:        "Monitored",
			SiteMonitor: &srv.URL,
			StatusStyle: &style,
		},
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(entry).Build()
	store := NewStore()
	p := &Poller{
		Reader: cl, SecretReader: cl, Namespace: testNamespace, InstanceName: testInstanceName,
		Interval: time.Hour, HTTPClient: srv.Client(), Store: store,
	}
	p.pollOnce(context.Background())

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

func TestPollerShowStatsAndHideErrors(t *testing.T) {
	// Upstream that errors (non-JSON) so the widget would normally set Err,
	// and would set Fields on success — neither should appear given the flags.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	showStats := false
	hideErrors := true
	url := srv.URL
	entry := &pagev1alpha1.ServiceEntry{
		ObjectMeta: metav1.ObjectMeta{Name: "flags", Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceEntrySpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Group:       "G",
			Name:        "Flags",
			ShowStats:   &showStats,
			HideErrors:  &hideErrors,
			Widgets:     []pagev1alpha1.ServiceWidget{{Type: testWidgetType, URL: &url}},
		},
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(entry).Build()
	store := NewStore()
	p := &Poller{
		Reader: cl, SecretReader: cl, Namespace: testNamespace, InstanceName: testInstanceName,
		Interval: time.Hour, HTTPClient: srv.Client(), Store: store,
	}
	p.pollOnce(context.Background())

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
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Type:        "openmeteo",
			Options:     &apiextensionsv1.JSON{Raw: []byte(`{"latitude":51.5,"longitude":-0.12,"url":"` + srv.URL + `"}`)},
		},
	}
	// A datetime InfoWidget carries no registered widget, so it must NOT
	// produce a polled card.
	dt := &pagev1alpha1.InfoWidget{
		ObjectMeta: metav1.ObjectMeta{Name: "clock", Namespace: testNamespace},
		Spec: pagev1alpha1.InfoWidgetSpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Type:        headerTypeDatetime,
		},
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(iw, dt).Build()
	store := NewStore()
	p := &Poller{
		Reader: cl, SecretReader: cl, Namespace: testNamespace, InstanceName: testInstanceName,
		Interval: time.Hour, HTTPClient: srv.Client(), Store: store,
	}
	p.pollOnce(context.Background())

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
		objs = append(objs, &pagev1alpha1.ServiceEntry{
			ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("svc-%d", i), Namespace: testNamespace},
			Spec: pagev1alpha1.ServiceEntrySpec{
				InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
				Group:       testGroup,
				Name:        fmt.Sprintf("Service %d", i),
				Widgets:     []pagev1alpha1.ServiceWidget{{Type: testWidgetType, URL: &url}},
			},
		})
	}

	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()

	store := NewStore()
	p := &Poller{
		Reader: cl, SecretReader: cl, Namespace: testNamespace, InstanceName: testInstanceName,
		Interval: time.Hour, HTTPClient: srv.Client(), Store: store,
	}

	start := time.Now()
	p.pollOnce(context.Background())
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
	url := "http://example.invalid"
	entry := &pagev1alpha1.ServiceEntry{
		ObjectMeta: metav1.ObjectMeta{Name: testSvcName, Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceEntrySpec{
			InstanceRef: pagev1alpha1.InstanceRef{Name: testInstanceName},
			Group:       "G",
			Name:        "Svc",
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
		Reader: cl, SecretReader: cl, Namespace: testNamespace, InstanceName: testInstanceName,
		Interval: time.Hour, HTTPClient: http.DefaultClient, Store: store,
	}
	p.pollOnce(context.Background())

	cards := store.Snapshot()
	if len(cards) != 1 || cards[0].Err == "" {
		t.Fatalf("Snapshot() = %+v, want one card with a non-empty Err for missing Secret", cards)
	}
}
