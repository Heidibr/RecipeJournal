// Client-side glue for the read-only offline feature.
//
// Responsibilities:
//   * Register the service worker (scope: /).
//   * Persist the user's "downloaded recipe IDs" set in localStorage, so the
//     bookmark indicator on the list survives reloads without a server call.
//   * Provide window.offlineToggle(id, url, btn) for the list-page buttons.
//   * Provide window.offlineClear() for the logout form to call before submit.

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

  function loadSet() {
    try {
      return new Set(JSON.parse(localStorage.getItem(STORAGE_KEY) || "[]"));
    } catch {
      return new Set();
    }
  }
  function saveSet(set) {
    localStorage.setItem(STORAGE_KEY, JSON.stringify([...set]));
  }
  function paint(btn, downloaded) {
    btn.dataset.downloaded = downloaded ? "1" : "0";
    btn.setAttribute("aria-pressed", downloaded ? "true" : "false");
    btn.title = downloaded ? "Remove offline copy" : "Download for offline";
  }

  window.offlineToggle = async function (id, url, btn) {
    const reg = await swReady;
    if (!reg) {
      alert("Offline isn't supported in this browser.");
      return;
    }
    const set = loadSet();
    const idStr = String(id);
    if (set.has(idStr)) {
      reg.active && reg.active.postMessage({ type: "uncache", url });
      set.delete(idStr);
      paint(btn, false);
    } else {
      reg.active && reg.active.postMessage({ type: "cache", url });
      set.add(idStr);
      paint(btn, true);
    }
    saveSet(set);
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
    const set = loadSet();
    document.querySelectorAll("[data-offline-toggle]").forEach((btn) => {
      paint(btn, set.has(btn.dataset.offlineToggle));
    });
  });
})();
