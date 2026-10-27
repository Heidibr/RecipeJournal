// Recipe Journal service worker.
//
// Two caches:
//   shell-v5   — app shell (CSS, JS, fonts, manifest). Precached on install.
//   recipes-v1 — per-recipe pages the user has explicitly "downloaded for
//                offline" from the recipe list.
//
// Strategy:
//   * Same-origin GET to a URL in recipes-v1 → network-first, fall back to
//     cache. Keeps the cache fresh while online, still works offline.
//   * Anything in the shell cache → stale-while-revalidate: serve the cached
//     copy at once (so an offline launch still paints), then refresh it in
//     the background so a changed asset is live on the next load. This was
//     cache-first, which never re-fetched and so made every CSS/JS deploy
//     invisible to anyone who had already opened the app.
//   * Everything else → network. A page navigation that fails (offline, and
//     the page isn't saved) gets the precached /offline page, which lists
//     the saved recipes. Without it, opening the app offline landed on the
//     browser's own error page and the saved recipes were unreachable.
//
// SHELL_CACHE carries a version. Bump it whenever the shell asset *list*
// changes; routine content changes no longer need a bump, because
// stale-while-revalidate picks those up on its own.
//
// Coordination with the page happens via postMessage:
//   { type: 'cache',  url }    — fetch and store a recipe URL
//   { type: 'uncache', url }   — drop a stored recipe URL
//   { type: 'clear' }          — wipe everything (called on logout)

const SHELL_CACHE = "shell-v6";
const OFFLINE_URL = "/offline";
const RECIPES_CACHE = "recipes-v1";

const SHELL_URLS = [
  "/static/js/htmx.min.js",
  "/static/js/offline.js",
  "/static/img/logo-en-linje.svg",
  "/static/img/logo-to-linjer.svg",
  "/static/img/logo-merke.svg",
  "/static/img/logo-stablet.svg",
  "/static/img/favicon.svg",
  "/manifest.webmanifest",
  OFFLINE_URL,
];

self.addEventListener("install", (e) => {
  // Add each URL on its own. cache.addAll() rejects the whole install if a
  // single request 404s, which once left an old worker active indefinitely --
  // serving a stale stylesheet against freshly rendered markup, so text went
  // invisible wherever a class name had changed. One missing file should cost
  // us that file, nothing more.
  e.waitUntil(
    caches.open(SHELL_CACHE).then((c) =>
      Promise.all(
        SHELL_URLS.map((u) =>
          c.add(u).catch((err) => console.warn("[sw] skipped", u, err)),
        ),
      ),
    ),
  );
  self.skipWaiting();
});

self.addEventListener("activate", (e) => {
  // Drop any old-versioned caches so a worker update doesn't leak storage.
  e.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(
        keys
          .filter((k) => k !== SHELL_CACHE && k !== RECIPES_CACHE)
          .map((k) => caches.delete(k)),
      ),
    ),
  );
  self.clients.claim();
});

self.addEventListener("fetch", (e) => {
  const req = e.request;
  if (req.method !== "GET") return;
  const url = new URL(req.url);
  if (url.origin !== self.location.origin) return;

  e.respondWith(handle(e, req));
});

async function handle(e, req) {
  const url = new URL(req.url);

  const recipes = await caches.open(RECIPES_CACHE);
  const recipeMatch = await recipes.match(req, { ignoreSearch: true });
  if (recipeMatch) {
    // Network-first: try to refresh the cached recipe, but if offline use the
    // cached copy. The .catch() is what makes this work without a connection.
    return fetch(req)
      .then((res) => {
        if (res.ok) recipes.put(req, res.clone());
        return res;
      })
      .catch(() => recipeMatch);
  }

  const shell = await caches.open(SHELL_CACHE);
  const shellMatch = await shell.match(req);

  // Anything under /static/ that is not precached (notably the content-hashed
  // stylesheet URL) is cached on first use, so the app still has its CSS
  // offline even though the filename changes on every build.
  if (!shellMatch && url.pathname.startsWith("/static/")) {
    return fetch(req)
      .then((res) => {
        if (res.ok) shell.put(req, res.clone());
        return res;
      })
      .catch(
        async () =>
          (await shell.match(req, { ignoreSearch: true })) || Response.error(),
      );
  }

  if (shellMatch) {
    // Refresh in the background. waitUntil keeps the worker alive until the
    // put finishes; the failure case (offline) is ignored on purpose, since
    // we have already decided to serve the cached copy.
    e.waitUntil(
      fetch(req)
        .then((res) => (res.ok ? shell.put(req, res.clone()) : null))
        .catch(() => null),
    );
    return shellMatch;
  }

  if (req.mode === "navigate") {
    return fetch(req).catch(
      async () => (await shell.match(OFFLINE_URL)) || Response.error(),
    );
  }
  return fetch(req);
}

self.addEventListener("message", (e) => {
  const msg = e.data || {};
  if (msg.type === "cache" && msg.url) {
    e.waitUntil(
      caches.open(RECIPES_CACHE).then(async (c) => {
        const res = await fetch(msg.url, { credentials: "include" });
        if (res.ok) await c.put(msg.url, res);
      }),
    );
  } else if (msg.type === "uncache" && msg.url) {
    e.waitUntil(caches.open(RECIPES_CACHE).then((c) => c.delete(msg.url)));
  } else if (msg.type === "clear") {
    e.waitUntil(
      caches.keys().then((keys) => Promise.all(keys.map((k) => caches.delete(k)))),
    );
  }
});
