package dashboard

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pagev1alpha1 "github.com/maverickd650/kubepage-operator/api/v1alpha1"
)

// htpasswdSecretKey is the Secret key basicAuthMiddleware reads the
// htpasswd file from, per DashboardSpec.Auth.BasicAuthSecretRef's doc
// comment.
const htpasswdSecretKey = ".htpasswd"

// basicAuthCacheTTL bounds how long a resolved htpasswd file is cached
// before being re-fetched. Without this, every request (page load, every
// htmx poll from every open browser tab) would Get the Dashboard and Secret
// from the API server before serving anything — this mirrors unifi.go's
// session-cache pattern (don't hit a backend more than necessary per
// request) applied to the API server instead of an upstream service.
const basicAuthCacheTTL = 30 * time.Second

// authCacheEntry caches loadBasicAuth's result for one Dashboard.
type authCacheEntry struct {
	entries map[string][]byte // username -> bcrypt hash
	enabled bool
	expiry  time.Time
}

var authCache = struct {
	mu      sync.Mutex
	entries map[string]authCacheEntry
}{entries: map[string]authCacheEntry{}}

// dummyBcryptHash is compared against on every request with an unrecognized
// username, so a login attempt against a nonexistent user takes the same
// bcrypt-comparison time as one against a real user with the wrong
// password — without this, the presence/absence of a username would be a
// timing side-channel an attacker could use to enumerate valid usernames.
var dummyBcryptHash = mustBcryptHash("kubepage-dashboard-timing-safety-placeholder")

func mustBcryptHash(password string) []byte {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	return hash
}

// parseHtpasswd parses an Apache htpasswd-format file into a username ->
// bcrypt-hash map. Only bcrypt entries ($2a$/$2b$/$2y$ prefixes, e.g. from
// `htpasswd -B`) are recognized; crypt()/MD5/SHA1 entries are silently
// skipped rather than erroring the whole file, since this package has no
// verifier for those weaker/legacy formats and one unsupported line
// shouldn't lock out every other user in the file.
func parseHtpasswd(data []byte) map[string][]byte {
	entries := map[string][]byte{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		user, hash, ok := strings.Cut(line, ":")
		if !ok || user == "" {
			continue
		}
		if !strings.HasPrefix(hash, "$2a$") && !strings.HasPrefix(hash, "$2b$") && !strings.HasPrefix(hash, "$2y$") {
			continue
		}
		entries[user] = []byte(hash)
	}
	return entries
}

// loadBasicAuth returns the parsed htpasswd entries for the Dashboard named
// by namespace/dashboardName, and whether spec.auth.basicAuthSecretRef is set
// at all (enabled). reader is expected cache-backed (it only reads the
// Dashboard); secretReader is expected uncached, per this package's
// secret-handling rule (see poller.go's resolveSecret) — the htpasswd
// file's bcrypt hashes are credential material the same as any other
// Secret this package resolves.
//
// An Dashboard that doesn't exist (yet, e.g. a brief cache-warm-up race right
// after the dashboard pod starts) is treated the same as spec.auth being
// unset — enabled=false, no error — rather than failing the request: the
// dashboard pod only ever exists because its own Dashboard created it, so
// this is a transient-race case, not a security-relevant "we don't know the
// policy" case. Any other read error (API server unreachable, RBAC denied)
// returns an error instead, so basicAuthMiddleware fails closed rather than
// silently serving unauthenticated on an error it can't attribute to "no
// auth configured".
func loadBasicAuth(ctx context.Context, reader, secretReader client.Reader, namespace, dashboardName string) (entries map[string][]byte, enabled bool, err error) {
	var instance pagev1alpha1.Dashboard
	if err := reader.Get(ctx, types.NamespacedName{Name: dashboardName, Namespace: namespace}, &instance); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("getting Dashboard: %w", err)
	}
	if instance.Spec.Auth == nil || instance.Spec.Auth.BasicAuthSecretRef == nil {
		return nil, false, nil
	}
	secretName := instance.Spec.Auth.BasicAuthSecretRef.Name

	cacheKey := namespace + "/" + dashboardName
	authCache.mu.Lock()
	if cached, ok := authCache.entries[cacheKey]; ok && time.Now().Before(cached.expiry) {
		authCache.mu.Unlock()
		return cached.entries, cached.enabled, nil
	}
	authCache.mu.Unlock()

	secret := &corev1.Secret{}
	if err := secretReader.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, secret); err != nil {
		return nil, true, fmt.Errorf("getting basic auth Secret %q: %w", secretName, err)
	}
	data, ok := secret.Data[htpasswdSecretKey]
	if !ok {
		return nil, true, fmt.Errorf("basic auth Secret %q has no %q key", secretName, htpasswdSecretKey)
	}
	entries = parseHtpasswd(data)

	authCache.mu.Lock()
	authCache.entries[cacheKey] = authCacheEntry{entries: entries, enabled: true, expiry: time.Now().Add(basicAuthCacheTTL)}
	authCache.mu.Unlock()

	return entries, true, nil
}

// InvalidateAuthCache clears the cached basic-auth htpasswd entries for one
// Dashboard, so a changed Secret takes effect immediately instead of
// waiting up to basicAuthCacheTTL. internal/preview's live reload
// (Watch) calls this after every successful reload: swapping the
// underlying Reader doesn't otherwise touch this package-level cache, so an
// edited htpasswd Secret would keep enforcing pre-edit credentials for up
// to the TTL even though the reload itself already picked up the change.
func InvalidateAuthCache(namespace, dashboardName string) {
	authCache.mu.Lock()
	delete(authCache.entries, namespace+"/"+dashboardName)
	authCache.mu.Unlock()
}

// healthzPath is excluded from basicAuthMiddleware so liveness/readiness
// probes never need credentials, mirroring how the Kubernetes Deployment's
// own probes (instance_controller.go) never carry auth headers.
const healthzPath = "/healthz"

// basicAuthMiddleware wraps next with HTTP Basic authentication when the
// Dashboard's spec.auth.basicAuthSecretRef is set, checked on every request
// except /healthz. Credential comparison uses
// bcrypt.CompareHashAndPassword — the standard constant-time-with-respect-
// to-the-password primitive for this, rather than a hand-rolled comparison
// against a stored hash/plaintext.
func (s *Server) basicAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == healthzPath {
			next.ServeHTTP(w, r)
			return
		}

		entries, enabled, err := loadBasicAuth(r.Context(), s.Reader, s.SecretReader, s.Namespace, s.DashboardName)
		if err != nil {
			http.Error(w, "resolving dashboard authentication", http.StatusInternalServerError)
			return
		}
		if !enabled {
			next.ServeHTTP(w, r)
			return
		}

		if username, password, ok := r.BasicAuth(); ok {
			hash, known := entries[username]
			if !known {
				hash = dummyBcryptHash
			}
			match := bcrypt.CompareHashAndPassword(hash, []byte(password)) == nil
			if known && match {
				next.ServeHTTP(w, r)
				return
			}
		}

		w.Header().Set("WWW-Authenticate", `Basic realm="kubepage dashboard", charset="UTF-8"`)
		http.Error(w, "authentication required", http.StatusUnauthorized)
	})
}
