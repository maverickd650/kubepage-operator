package dashboard

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

func init() {
	Register("unifi", &unifiWidget{})
}

// unifiWidget polls a UniFi Network controller's site health endpoint.
// Unlike the other ten "uniform" widgets, UniFi has no static-token auth: it
// requires a stateful username/password login that returns a session cookie
// (and, on UniFi OS / UDM hardware, a CSRF token), and the API is mounted at
// a different path depending on whether the controller is UniFi OS (UDM/
// UDM-Pro/Cloud Gateway, API behind "/proxy/network") or the classic
// software controller (API at "/api" directly). Both shapes are handled
// here so the same widget works against either.
//
// Secrets: "username", "password" — UniFi local-account credentials.
// Config: {"site": "default", "insecureTLS": false} — site defaults to
// "default" (the controller's default site name); insecureTLS is an
// explicit opt-in for self-hosted controllers using a self-signed
// certificate (common for Cloud Key / on-prem installs), since the shared
// HTTP client passed to every other widget verifies certificates normally.
type unifiWidget struct{}

const (
	unifiSecretUsername = "username"
	unifiSecretPassword = "password"
)

type unifiConfig struct {
	Site        string `json:"site"`
	InsecureTLS bool   `json:"insecureTLS"`
}

// unifiSession holds what's needed to make authenticated requests without
// logging in again: the cookies UniFi issued on login, the CSRF token
// UniFi OS controllers return alongside them, and which controller shape
// responded so the right API base path is reused on every subsequent poll.
type unifiSession struct {
	cookies   []*http.Cookie
	csrfToken string
	isUDM     bool

	// lastUsed drives unifiSessionCache's TTL eviction (see
	// pruneUnifiSessions): unlike Store, this package-level cache has no
	// per-poll-cycle Prune to remove an entry whose ServiceEntry was
	// deleted or had its URL/username edited, so it would otherwise grow
	// forever.
	lastUsed time.Time
}

// unifiSessionTTL bounds how long a cached session survives without being
// reused. See unifiSession.lastUsed.
const unifiSessionTTL = 30 * time.Minute

// unifiSessionCache keeps one session per (URL, username) so a stateful
// login only happens once per target, not on every poll interval — UniFi
// controllers rate-limit repeated logins, and the whole point of the
// session/cookie dance is to avoid paying it more than necessary.
var unifiSessionCache = struct {
	mu       sync.Mutex
	sessions map[string]*unifiSession
}{sessions: map[string]*unifiSession{}}

func unifiSessionKey(url, username string) string {
	return url + "|" + username
}

// pruneUnifiSessions evicts any cached session unused for longer than
// unifiSessionTTL, called once per Poll so a ServiceEntry that's deleted or
// changes URL/username doesn't leave its old session cached indefinitely.
func pruneUnifiSessions(now time.Time) {
	unifiSessionCache.mu.Lock()
	defer unifiSessionCache.mu.Unlock()
	for k, s := range unifiSessionCache.sessions {
		if now.Sub(s.lastUsed) > unifiSessionTTL {
			delete(unifiSessionCache.sessions, k)
		}
	}
}

// storeUnifiSession caches session under key, stamped with the current time
// so pruneUnifiSessions won't immediately evict it.
func storeUnifiSession(key string, session *unifiSession) {
	session.lastUsed = time.Now()
	unifiSessionCache.mu.Lock()
	unifiSessionCache.sessions[key] = session
	unifiSessionCache.mu.Unlock()
}

// loadUnifiSession returns the cached session for key, if any, touching its
// lastUsed time so an actively-polled target's session survives
// pruneUnifiSessions regardless of unifiSessionTTL.
func loadUnifiSession(key string) *unifiSession {
	unifiSessionCache.mu.Lock()
	defer unifiSessionCache.mu.Unlock()
	session := unifiSessionCache.sessions[key]
	if session != nil {
		session.lastUsed = time.Now()
	}
	return session
}

type unifiHealthResponse struct {
	Data []struct {
		Subsystem string `json:"subsystem"`
		Status    string `json:"status"`
		NumUser   int    `json:"num_user"`
	} `json:"data"`
}

func (unifiWidget) Poll(ctx context.Context, httpClient *http.Client, cfg WidgetConfig) ([]Field, error) {
	if cfg.URL == "" {
		return nil, errors.New("unifi widget: url is required")
	}
	username := cfg.Secrets[unifiSecretUsername]
	password := cfg.Secrets[unifiSecretPassword]
	if username == "" || password == "" {
		return nil, errors.New("unifi widget: secrets.username and secrets.password are required")
	}

	var unifiCfg unifiConfig
	if len(cfg.Config) > 0 {
		if err := json.Unmarshal(cfg.Config, &unifiCfg); err != nil {
			return nil, fmt.Errorf("decoding widget config: %w", err)
		}
	}
	site := unifiCfg.Site
	if site == "" {
		site = "default"
	}

	baseURL := strings.TrimRight(cfg.URL, "/")
	client := unifiHTTPClient(httpClient, baseURL, unifiCfg.InsecureTLS)
	key := unifiSessionKey(baseURL, username)

	pruneUnifiSessions(time.Now())
	session := loadUnifiSession(key)

	if session == nil {
		var err error
		session, err = unifiLogin(ctx, client, baseURL, username, password)
		if err != nil {
			return []Field{{Label: labelStatus, Value: statusUnreach}}, nil
		}
		storeUnifiSession(key, session)
	}

	resp, err := unifiFetchHealth(ctx, client, baseURL, site, session)
	if err != nil {
		return []Field{{Label: labelStatus, Value: statusUnreach}}, nil
	}

	if resp.StatusCode == http.StatusUnauthorized {
		// Session expired server-side; log in fresh and retry exactly once.
		_ = resp.Body.Close()
		session, err = unifiLogin(ctx, client, baseURL, username, password)
		if err != nil {
			return []Field{{Label: labelStatus, Value: statusUnreach}}, nil
		}
		storeUnifiSession(key, session)

		resp, err = unifiFetchHealth(ctx, client, baseURL, site, session)
		if err != nil {
			return []Field{{Label: labelStatus, Value: statusUnreach}}, nil
		}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return []Field{{Label: labelStatus, Value: fmt.Sprintf("HTTP %d", resp.StatusCode)}}, nil
	}

	var parsed unifiHealthResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxWidgetResponseBytes)).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decoding health response: %w", err)
	}

	status := statusHealthy
	clients := 0
	for _, subsystem := range parsed.Data {
		if subsystem.Status != "ok" {
			status = statusDegraded
		}
		clients += subsystem.NumUser
	}
	if len(parsed.Data) == 0 {
		status = statusUnknown
	}

	return []Field{
		{Label: labelStatus, Value: status},
		{Label: labelClients, Value: fmt.Sprintf("%d", clients)},
	}, nil
}

// unifiInsecureClientCache holds one *http.Client per baseURL for
// insecureTLS controllers, so unifiHTTPClient builds (and keeps open
// connections in) a fresh *http.Transport once per target rather than on
// every poll — an insecureTLS controller was otherwise paying full TLS
// handshake cost each cycle instead of reusing keep-alive connections like
// every other widget.
var unifiInsecureClientCache = struct {
	mu      sync.Mutex
	clients map[string]*http.Client
}{clients: map[string]*http.Client{}}

// unifiHTTPClient returns client unchanged unless insecureTLS is set, in
// which case it returns a cached (or newly built and cached) client for
// baseURL with certificate verification disabled — separate from client
// since that's the single *http.Client shared by every widget poll and must
// stay safe for the controllers that don't need this opt-out. The returned
// client still goes through newGuardedTransport's link-local dial guard,
// same as client itself.
func unifiHTTPClient(client *http.Client, baseURL string, insecureTLS bool) *http.Client {
	if !insecureTLS {
		return client
	}

	unifiInsecureClientCache.mu.Lock()
	defer unifiInsecureClientCache.mu.Unlock()
	if c, ok := unifiInsecureClientCache.clients[baseURL]; ok {
		return c
	}

	transport := newGuardedTransport(nil)
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit per-widget opt-in, see unifiConfig.InsecureTLS
	c := &http.Client{Timeout: client.Timeout, Transport: transport}
	unifiInsecureClientCache.clients[baseURL] = c
	return c
}

// unifiLogin authenticates against baseURL, trying the UniFi OS (UDM/UDM-Pro
// /Cloud Gateway) login endpoint first and falling back to the classic
// software-controller endpoint if that path doesn't exist. The two
// controller shapes use different login paths, different API mount points,
// and only UniFi OS returns a CSRF token — which one responds determines
// every subsequent request this session makes.
func unifiLogin(ctx context.Context, client *http.Client, baseURL, username, password string) (*unifiSession, error) {
	body, err := json.Marshal(map[string]string{unifiSecretUsername: username, unifiSecretPassword: password})
	if err != nil {
		return nil, fmt.Errorf("encoding login request: %w", err)
	}

	resp, err := unifiPostLogin(ctx, client, baseURL+"/api/auth/login", body)
	if err == nil && resp.StatusCode == http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		return &unifiSession{
			cookies:   resp.Cookies(),
			csrfToken: resp.Header.Get("X-Csrf-Token"),
			isUDM:     true,
		}, nil
	}
	if resp != nil {
		_ = resp.Body.Close()
	}

	resp, err = unifiPostLogin(ctx, client, baseURL+"/api/login", body)
	if err != nil {
		return nil, fmt.Errorf("logging in: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("logging in: HTTP %d", resp.StatusCode)
	}

	return &unifiSession{
		cookies:   resp.Cookies(),
		csrfToken: resp.Header.Get("X-Csrf-Token"),
		isUDM:     false,
	}, nil
}

func unifiPostLogin(ctx context.Context, client *http.Client, url string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return client.Do(req)
}

// unifiFetchHealth requests the site health summary, which is mounted at a
// different path on UniFi OS than on the classic controller.
func unifiFetchHealth(ctx context.Context, client *http.Client, baseURL, site string, session *unifiSession) (*http.Response, error) {
	var endpoint string
	if session.isUDM {
		endpoint = fmt.Sprintf("%s/proxy/network/api/s/%s/stat/health", baseURL, site)
	} else {
		endpoint = fmt.Sprintf("%s/api/s/%s/stat/health", baseURL, site)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building health request: %w", err)
	}
	for _, c := range session.cookies {
		req.AddCookie(c)
	}
	if session.csrfToken != "" {
		req.Header.Set("X-CSRF-Token", session.csrfToken)
	}

	return client.Do(req)
}
