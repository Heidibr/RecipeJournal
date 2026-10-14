// Recipe Journal service worker.
//
// Two caches:
//   shell-v1   — app shell (CSS, JS, fonts, manifest). Precached on install.
//   recipes-v1 — per-recipe pages the user has explicitly "downloaded for
//                offline" from the recipe list.
//
// Strategy:
//   * Same-origin GET to a URL in recipes-v1 → network-first, fall back to
//     cache. Keeps the cache fresh while online, still works offline.
//   * Anything in shell-v1 → cache-first.
//   * Everything else → network (no offline fallback).
//
// Coordination with the page happens via postMessage:
//   { type: 'cache',  url }    — fetch and store a recipe URL
//   { type: 'uncache', url }   — drop a stored recipe URL
//   { type: 'clear' }          — wipe everything (called on logout)

const SHELL_CACHE = "shell-v1";
const RECIPES_CACHE = "recipes-v1";

const SHELL_URLS = [
  "/static/css/styles.css",
  "/static/js/htmx.min.js",
  "/static/js/offline.js",
  "/static/img/icon.svg",
  "/manifest.webmanifest",
];

self.addEventListener("install", (e) => {
  e.waitUntil(caches.open(SHELL_CACHE).then((c) => c.addAll(SHELL_URLS)));
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

  e.respondWith(handle(req));
});

async function handle(req) {
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
  if (shellMatch) return shellMatch;

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
