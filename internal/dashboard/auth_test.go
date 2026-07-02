package dashboard

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/crypto/bcrypt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

func TestParseHtpasswdSkipsUnsupportedAndMalformedLines(t *testing.T) {
	hash := mustBcryptHash("s3cret")
	data := []byte("alice:" + string(hash) + "\n" +
		"# a comment\n" +
		"\n" +
		"bob:$apr1$legacy$notbcrypt\n" +
		"malformed-line-no-colon\n" +
		":novalue\n")

	entries := parseHtpasswd(data)
	if len(entries) != 1 {
		t.Fatalf("parseHtpasswd() = %d entries, want 1 (only alice's bcrypt line)", len(entries))
	}
	if _, ok := entries["alice"]; !ok {
		t.Errorf("parseHtpasswd() missing alice")
	}
	if _, ok := entries["bob"]; ok {
		t.Errorf("parseHtpasswd() should have skipped bob's non-bcrypt hash")
	}
}

// resetAuthCache clears the package-level authCache before a test runs, so
// tests sharing testNamespace/testInstanceName as their cache key don't leak
// loadBasicAuth results between each other (basicAuthCacheTTL otherwise
// keeps an earlier test's result around well past that test's lifetime).
func resetAuthCache(t *testing.T) {
	t.Helper()
	authCache.mu.Lock()
	authCache.entries = map[string]authCacheEntry{}
	authCache.mu.Unlock()
}

func newAuthTestServer(t *testing.T, instance *pagev1alpha1.Instance, secret *corev1.Secret) *Server {
	t.Helper()
	resetAuthCache(t)
	scheme := testScheme(t)
	objs := []client.Object{}
	if instance != nil {
		objs = append(objs, instance)
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()

	secretObjs := []client.Object{}
	if secret != nil {
		secretObjs = append(secretObjs, secret)
	}
	secretCl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secretObjs...).Build()

	return &Server{
		Store: NewStore(), Reader: cl, SecretReader: secretCl,
		Namespace: testNamespace, InstanceName: testInstanceName,
	}
}

func authTestInstance(basicAuthSecret string) *pagev1alpha1.Instance {
	inst := &pagev1alpha1.Instance{
		ObjectMeta: metav1.ObjectMeta{Name: testInstanceName, Namespace: testNamespace},
		Spec:       pagev1alpha1.InstanceSpec{ContainerPort: 8080},
	}
	if basicAuthSecret != "" {
		inst.Spec.Auth = &pagev1alpha1.AuthSpec{BasicAuthSecretRef: &corev1.LocalObjectReference{Name: basicAuthSecret}}
	}
	return inst
}

func TestBasicAuthMiddlewareNoAuthConfiguredAllowsRequest(t *testing.T) {
	srv := newAuthTestServer(t, authTestInstance(""), nil)

	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 when spec.auth is unset", rec.Code)
	}
}

func TestBasicAuthMiddlewareNoInstanceAllowsRequest(t *testing.T) {
	// No Instance object exists at all — should degrade to "no auth", not 500.
	srv := newAuthTestServer(t, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 when the Instance doesn't exist", rec.Code)
	}
}

func htpasswdSecret(username, password string) *corev1.Secret {
	hash := mustBcryptHash(password)
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "dashboard-auth", Namespace: testNamespace},
		Data:       map[string][]byte{htpasswdSecretKey: []byte(username + ":" + string(hash) + "\n")},
	}
}

func TestBasicAuthMiddlewareRejectsMissingCredentials(t *testing.T) {
	srv := newAuthTestServer(t, authTestInstance("dashboard-auth"), htpasswdSecret("alice", "hunter2"))

	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Error("missing WWW-Authenticate header on 401 response")
	}
}

func TestBasicAuthMiddlewareRejectsWrongPassword(t *testing.T) {
	srv := newAuthTestServer(t, authTestInstance("dashboard-auth"), htpasswdSecret("alice", "hunter2"))

	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	req.SetBasicAuth("alice", "wrong-password")
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestBasicAuthMiddlewareRejectsUnknownUsername(t *testing.T) {
	srv := newAuthTestServer(t, authTestInstance("dashboard-auth"), htpasswdSecret("alice", "hunter2"))

	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	req.SetBasicAuth("mallory", "hunter2")
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestBasicAuthMiddlewareAcceptsCorrectCredentials(t *testing.T) {
	srv := newAuthTestServer(t, authTestInstance("dashboard-auth"), htpasswdSecret("alice", "hunter2"))

	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	req.SetBasicAuth("alice", "hunter2")
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for correct credentials", rec.Code)
	}
}

func TestBasicAuthMiddlewareHealthzNeverRequiresAuth(t *testing.T) {
	srv := newAuthTestServer(t, authTestInstance("dashboard-auth"), htpasswdSecret("alice", "hunter2"))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200: /healthz must never require auth (liveness/readiness probes)", rec.Code)
	}
}

func TestBasicAuthMiddlewareMissingSecretIsInternalError(t *testing.T) {
	// spec.auth is set but the named Secret doesn't exist: fail closed with
	// a 500 rather than silently allowing the request through.
	srv := newAuthTestServer(t, authTestInstance("dashboard-auth"), nil)

	req := httptest.NewRequest(http.MethodGet, "/fragment", nil)
	req.SetBasicAuth("alice", "hunter2")
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 when the basic-auth Secret is missing", rec.Code)
	}
}

func TestBcryptCompareSanityCheck(t *testing.T) {
	hash := mustBcryptHash("correct-password")
	if bcrypt.CompareHashAndPassword(hash, []byte("correct-password")) != nil {
		t.Error("bcrypt comparison rejected the correct password")
	}
	if bcrypt.CompareHashAndPassword(hash, []byte("wrong-password")) == nil {
		t.Error("bcrypt comparison accepted the wrong password")
	}
}
