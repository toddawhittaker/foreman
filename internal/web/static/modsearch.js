// Search as you type (SPEC 9.1). htmx replaces #results; this reads the
// new count out through the shared announcer once typing has paused.
(() => {
  "use strict";
  const isResults = (e) => e.detail.target?.id === "results";
  // A refused or unreachable search still shows its page's message in place.
  document.addEventListener("htmx:beforeSwap", (e) => {
    if (isResults(e) && (e.detail.xhr.status === 400 || e.detail.xhr.status === 502)) e.detail.shouldSwap = true;
  });
  document.addEventListener("htmx:afterSettle", (e) => {
    if (!isResults(e)) return;
    const results = document.getElementById("results");
    const announcer = document.getElementById("announcer");
    if (!results || !announcer) return;
    const said = results.querySelector("caption, .empty, .message")?.textContent.trim();
    if (!said) return;
    // Emptied first and filled on the next frame, so the same count is read out again (DESIGN 7.5).
    announcer.textContent = "";
    requestAnimationFrame(() => {
      announcer.textContent = said;
    });
  });
})();
