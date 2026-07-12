package dashboard

import (
	"net/http"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

const testCACertSecretKey = "ca.crt"

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
		ObjectMeta: metav1.ObjectMeta{Name: "ca-bundle", Namespace: testNamespace},
		Data:       map[string][]byte{testCACertSecretKey: pemBytes},
	}
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	p := &Poller{SecretReader: cl}
	base := &http.Client{Timeout: 5 * time.Second}
	caCert := &pagev1alpha1.SecretValueSource{
		SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "ca-bundle"},
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
