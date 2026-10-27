// Client-side glue for the read-only offline feature.
//
// Responsibilities:
//   * Register the service worker (scope: /).
//   * Persist the user's downloaded recipes ({id, name}) in localStorage, so
//     the list buttons survive reloads without a server call, and so the
//     /offline page can list them by name when there is no network.
//   * Provide window.offlineToggle(id, url, btn) for the list-page buttons.
//   * Provide window.offlineClear() for logout and account deletion.

(function () {
  const STORAGE_KEY = "rj.offlineRecipes";
  const swReady =
    "serviceWorker" in navigator
      ? navigator.serviceWorker.register("/sw.js", { scope: "/" }).then(
          () => navigator.serviceWorker.ready,
          (err) => {
            console.warn("[offline] sw registration failed", err);
            return null;
          },
        )
      : Promise.resolve(null);

  // Returns a Map of id -> name. Older versions stored a bare array of ids;
  // those still load, just without a name until the recipe is re-saved.
  function load() {
    try {
      const raw = JSON.parse(localStorage.getItem(STORAGE_KEY) || "[]");
      return new Map(
        raw.map((e) => (typeof e === "object" ? [String(e.id), e.name || ""] : [String(e), ""])),
      );
    } catch {
      return new Map();
    }
  }
  function save(map) {
    localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify([...map].map(([id, name]) => ({ id, name }))),
    );
  }
  window.offlineRecipes = load;

  // Labels come from the template (data-label-on / data-label-off), so they
  // are in the page's language like every other string.
  function paint(btn, downloaded) {
    btn.dataset.downloaded = downloaded ? "1" : "0";
    btn.setAttribute("aria-pressed", downloaded ? "true" : "false");
    const label = downloaded ? btn.dataset.labelOn : btn.dataset.labelOff;
    if (label) {
      btn.title = label;
      btn.setAttribute("aria-label", label);
    }
  }

  window.offlineToggle = async function (id, url, btn) {
    const reg = await swReady;
    if (!reg || !reg.active) return;
    const map = load();
    const idStr = String(id);
    if (map.has(idStr)) {
      reg.active.postMessage({ type: "uncache", url });
      map.delete(idStr);
      paint(btn, false);
    } else {
      reg.active.postMessage({ type: "cache", url });
      map.set(idStr, btn.dataset.name || "");
      paint(btn, true);
    }
    save(map);
  };

  window.offlineClear = function () {
    // Fire-and-forget — the logout POST will navigate away before we'd hear back.
    if (navigator.serviceWorker && navigator.serviceWorker.controller) {
      navigator.serviceWorker.controller.postMessage({ type: "clear" });
    }
    localStorage.removeItem(STORAGE_KEY);
  };

  // On DOM ready, paint every list-row button to reflect its persisted state.
  document.addEventListener("DOMContentLoaded", () => {
    const map = load();
    document.querySelectorAll("[data-offline-toggle]").forEach((btn) => {
      paint(btn, map.has(btn.dataset.offlineToggle));
    });
  });
})();
