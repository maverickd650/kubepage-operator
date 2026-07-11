// Minimal offline app shell: caches only genuinely static, content-stable
// responses (fonts, vendored JS, the SVG icon, manifest.json) plus the last
// successfully fetched page shell ("/"), so a repeat visit or a flaky
// connection still gets an instant/offline-capable shell. Note the page
// shell itself isn't purely static — GET / server-renders the initial card
// grid and header strip straight into the response (see handleIndex's
// Fragment field) — so a genuinely offline visit does show whatever widget
// data was live at the last successful fetch, not a blank shell, until the
// browser is back online (interval polling/SSE then refreshes it in place,
// same as any other reconnect). That's expected/inherent to any offline
// cache of a page carrying live data, not a bug: what this worker actually
// guarantees is narrower and absolute — /fragment, /header, and /events
// (every route a *loaded* page repolls after that first paint) are never
// intercepted or served from a cache, only ever the network, so a page that
// is online never shows anything but this poll cycle's real data. This
// worker is also never registered at all on a password-protected Dashboard
// (see index.templ's registration script) — Cache Storage isn't itself
// gated by HTTP Basic Auth, so caching that authenticated response would
// let anyone with local access to the browser profile read its last-cached
// contents offline with no credential check.
//
// The page shell embeds a per-request CSP nonce (see server.go's
// securityHeaders/generateNonce) in both the Content-Security-Policy
// response header and every inline <script nonce="...">/<style nonce="...">
// tag in the body. Those two must always agree, or the browser refuses to
// run/apply the inline tags. cache.put/cache.match always store and replay
// a whole Response object — headers and body together — so a cached shell
// response is self-consistent by construction: its cached CSP header is the
// exact one that was generated alongside the nonces baked into its cached
// body. This worker never reconstructs a Response by mixing a cached body
// with a different (e.g. live) header, which is the only way that
// consistency could break.
const SHELL_CACHE = "kubepage-shell-v1";
const STATIC_CACHE = "kubepage-static-v1";
const CURRENT_CACHES = [SHELL_CACHE, STATIC_CACHE];

// Paths a service worker must never intern-cache: each one carries data
// that's only correct at the moment it was fetched (polled widget output,
// a live event stream, a liveness probe).
const NEVER_CACHE_PATHS = new Set(["/fragment", "/header", "/events", "/healthz"]);

self.addEventListener("install", (event) => {
	// Activate this worker (and start controlling clients, via the
	// "activate" listener's clients.claim()) as soon as it's installed,
	// rather than waiting for every open tab of the old worker to close —
	// there's no versioned API response shape here that an old tab could be
	// broken by.
	self.skipWaiting();
});

self.addEventListener("activate", (event) => {
	event.waitUntil(
		(async () => {
			const names = await caches.keys();
			await Promise.all(
				names.filter((name) => !CURRENT_CACHES.includes(name)).map((name) => caches.delete(name)),
			);
			await self.clients.claim();
		})(),
	);
});

self.addEventListener("fetch", (event) => {
	const request = event.request;
	// Only GET is ever safe/idempotent to serve from a cache; every other
	// method (and any cross-origin request, e.g. a widget's remote icon) is
	// left to the browser's normal network handling.
	if (request.method !== "GET") {
		return;
	}
	const url = new URL(request.url);
	if (url.origin !== self.location.origin) {
		return;
	}
	if (NEVER_CACHE_PATHS.has(url.pathname)) {
		return;
	}

	if (url.pathname.startsWith("/assets/") || url.pathname === "/manifest.json") {
		event.respondWith(cacheFirst(request, STATIC_CACHE));
		return;
	}

	if (url.pathname === "/") {
		event.respondWith(networkFirstShell(request));
	}
});

// cacheFirst serves a cached static asset immediately when present, only
// hitting the network on a cache miss (assets are content-stable — see
// handleAsset's own Cache-Control: immutable — so there's nothing to
// revalidate).
async function cacheFirst(request, cacheName) {
	const cache = await caches.open(cacheName);
	const cached = await cache.match(request);
	if (cached) {
		return cached;
	}
	const response = await fetch(request);
	if (response.ok) {
		await cache.put(request, response.clone());
	}
	return response;
}

// networkFirstShell always prefers a live "/" response (so an online visit
// never shows a stale shell), caching each successful one as the new
// offline fallback; only a failed fetch (offline, upstream unreachable)
// falls back to whatever shell was last cached.
async function networkFirstShell(request) {
	const cache = await caches.open(SHELL_CACHE);
	try {
		const response = await fetch(request);
		if (response.ok) {
			await cache.put(request, response.clone());
		}
		return response;
	} catch (err) {
		const cached = await cache.match(request);
		if (cached) {
			return cached;
		}
		throw err;
	}
}
