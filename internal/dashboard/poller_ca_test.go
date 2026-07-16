package dashboard

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

const (
	testCACertSecretKey  = "ca.crt"
	testCACertSecretName = "ca-bundle"
)

func TestCAClientCachesByPEMContentHash(t *testing.T) {
	base := &http.Client{Timeout: 5 * time.Second}
	pem := generateTestCACertPEM(t)

	c1, err := caClient(base, string(pem))
	if err != nil {
		t.Fatalf("caClient() error = %v", err)
	}
	c2, err := caClient(base, string(pem))
	if err != nil {
		t.Fatalf("caClient() error = %v", err)
	}
	if c1 != c2 {
		t.Error("caClient() built a new *http.Client for an identical PEM bundle, want the cached one reused")
	}
	if c1.Timeout != base.Timeout {
		t.Errorf("caClient().Timeout = %v, want %v (copied from base)", c1.Timeout, base.Timeout)
	}
}

func TestCAClientRejectsInvalidPEM(t *testing.T) {
	base := &http.Client{Timeout: 5 * time.Second}
	if _, err := caClient(base, "not a PEM certificate"); err == nil {
		t.Fatal("caClient() error = nil, want error for invalid PEM")
	}
}

func TestHTTPClientForCACertNilReturnsBaseUnchanged(t *testing.T) {
	p := &Poller{}
	base := &http.Client{Timeout: 5 * time.Second}

	got, err := p.httpClientForCACert(t.Context(), testNamespace, nil, base)
	if err != nil {
		t.Fatalf("httpClientForCACert() error = %v", err)
	}
	if got != base {
		t.Error("httpClientForCACert(nil caCert) did not return base unchanged")
	}
}

func TestHTTPClientForCACertResolvesSecretAndBuildsClient(t *testing.T) {
	pemBytes := generateTestCACertPEM(t)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: testCACertSecretName, Namespace: testNamespace},
		Data:       map[string][]byte{testCACertSecretKey: pemBytes},
	}
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	p := &Poller{SecretReader: cl}
	base := &http.Client{Timeout: 5 * time.Second}
	caCert := &pagev1alpha1.SecretValueSource{
		SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: testCACertSecretName},
			Key:                  testCACertSecretKey,
		},
	}

	got, err := p.httpClientForCACert(t.Context(), testNamespace, caCert, base)
	if err != nil {
		t.Fatalf("httpClientForCACert() error = %v", err)
	}
	if got == base {
		t.Error("httpClientForCACert() returned base unchanged, want a CA-trusting client")
	}
}

func TestHTTPClientForCACertSecretResolutionError(t *testing.T) {
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()

	p := &Poller{SecretReader: cl}
	base := &http.Client{Timeout: 5 * time.Second}
	caCert := &pagev1alpha1.SecretValueSource{
		SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: testDoesNotExist},
			Key:                  testCACertSecretKey,
		},
	}

	if _, err := p.httpClientForCACert(t.Context(), testNamespace, caCert, base); err == nil {
		t.Fatal("httpClientForCACert() error = nil, want error when the Secret doesn't exist")
	}
}

// TestPruneCAClientCacheDropsUnusedEntries verifies pruneCAClientCache
// evicts a caClientCache entry once a poll cycle completes without any
// widget resolving that CA bundle again — e.g. after a caCert rotation —
// mirroring pruneWidgetLastPolled's behavior for widgetLastPolled.
func TestPruneCAClientCacheDropsUnusedEntries(t *testing.T) {
	base := &http.Client{Timeout: 5 * time.Second}
	stalePEM := string(generateTestCACertPEM(t))
	freshPEM := string(generateTestCACertPEM(t))

	if _, err := caClient(base, stalePEM); err != nil {
		t.Fatalf("caClient(stale) error = %v", err)
	}
	if _, err := caClient(base, freshPEM); err != nil {
		t.Fatalf("caClient(fresh) error = %v", err)
	}
	staleKey, freshKey := caCacheKey(stalePEM), caCacheKey(freshPEM)

	p := &Poller{}
	p.markCAKeyUsed(freshKey) // simulates only freshPEM being resolved this cycle
	p.pruneCAClientCache()

	caClientCache.mu.Lock()
	_, staleStillCached := caClientCache.clients[staleKey]
	_, freshStillCached := caClientCache.clients[freshKey]
	caClientCache.mu.Unlock()

	if staleStillCached {
		t.Error("pruneCAClientCache() left an entry not used this cycle cached")
	}
	if !freshStillCached {
		t.Error("pruneCAClientCache() evicted an entry that was used this cycle")
	}
}

// TestPollWidgetPollIntervalOverrideSkipsCAKeyTracking exercises the caveat
// pruneCAClientCache's own doc comment calls out: a widget with a
// PollIntervalSeconds override that isn't due this cycle returns from
// pollWidget before ever reaching httpClientForCACert, so its CA key isn't
// marked used and pruneCAClientCache can evict its cached *http.Client
// between due polls. That's harmless — the next due poll just rebuilds a
// client trusting the same CA bundle — which this test confirms by polling
// successfully again after the eviction.
func TestPollWidgetPollIntervalOverrideSkipsCAKeyTracking(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"status":"success","data":{"activeTargets":[{"health":"up"}]}}`))
	}))
	defer upstream.Close()

	pemBytes := generateTestCACertPEM(t)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: testCACertSecretName, Namespace: testNamespace},
		Data:       map[string][]byte{testCACertSecretKey: pemBytes},
	}
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	url := upstream.URL
	overrideSeconds := int32(100)
	caCert := &pagev1alpha1.SecretValueSource{
		SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: testCACertSecretName},
			Key:                  testCACertSecretKey,
		},
	}
	widget := &pagev1alpha1.ServiceWidget{Type: testWidgetType, URL: &url, PollIntervalSeconds: &overrideSeconds, CACert: caCert}
	entry := pagev1alpha1.ServiceCard{
		ObjectMeta: metav1.ObjectMeta{Name: testSvcName, Namespace: testNamespace},
		Spec: pagev1alpha1.ServiceCardSpec{
			Group:    testGroup,
			Services: []pagev1alpha1.ServiceEntry{{Name: testSvcAName}},
		},
	}
	key := caCacheKey(string(pemBytes))

	store := NewStore()
	p := &Poller{HTTPClient: http.DefaultClient, SecretReader: cl, Store: store, Interval: time.Second}

	// First poll of key is always due: resolves the CA bundle and marks its
	// key used, so pruneCAClientCache (as pollOnce would call it at the end
	// of this cycle) keeps the cached client.
	p.pollWidget(t.Context(), testCardKeyA, entry.Namespace, entry.Spec.Entries()[0], widget, monitorProbeResult{}, false, nil)
	if n := hits.Load(); n != 1 {
		t.Fatalf("after first (due) poll, upstream hit %d times, want 1", n)
	}
	p.pruneCAClientCache()
	caClientCache.mu.Lock()
	_, stillCached := caClientCache.clients[key]
	caClientCache.mu.Unlock()
	if !stillCached {
		t.Fatal("pruneCAClientCache evicted a key resolved earlier in the same cycle")
	}

	// Simulate the next pollOnce cycle: caKeysUsed resets, and this poll is
	// within the 100s override so it's skipped before ever reaching
	// httpClientForCACert — the key is not marked used this cycle.
	p.caKeysUsedMu.Lock()
	p.caKeysUsed = map[string]bool{}
	p.caKeysUsedMu.Unlock()
	p.pollWidget(t.Context(), testCardKeyA, entry.Namespace, entry.Spec.Entries()[0], widget, monitorProbeResult{}, false, nil)
	if n := hits.Load(); n != 1 {
		t.Fatalf("after second (not-yet-due) poll, upstream hit %d times, want still 1", n)
	}
	p.pruneCAClientCache()
	caClientCache.mu.Lock()
	_, evicted := caClientCache.clients[key]
	caClientCache.mu.Unlock()
	if evicted {
		t.Fatal("pruneCAClientCache did not evict a key unused this cycle (a not-yet-due override widget), contradicting its own doc comment")
	}

	// Back-date the last-polled time past the override: due again, and must
	// still succeed even though its cached *http.Client was just evicted —
	// caClient rebuilds one trusting the same CA bundle.
	p.widgetLastPolledMu.Lock()
	p.widgetLastPolled[testCardKeyA] = time.Now().Add(-101 * time.Second)
	p.widgetLastPolledMu.Unlock()
	p.pollWidget(t.Context(), testCardKeyA, entry.Namespace, entry.Spec.Entries()[0], widget, monitorProbeResult{}, false, nil)
	if n := hits.Load(); n != 2 {
		t.Errorf("after third (due) poll, upstream hit %d times, want 2", n)
	}
	if card := store.Snapshot()[0]; card.Err != "" {
		t.Errorf("card.Err = %q after rebuild, want empty", card.Err)
	}
}
