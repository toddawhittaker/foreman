// Search as you type (SPEC 9.1). htmx replaces #results; this keeps the box's
// error state in step and reads the outcome out through the shared announcer.
(() => {
  "use strict";
  const box = document.getElementById("mod-search");
  if (!box) return;
  const short = () => box.value.trim().length < 2;
  const isResults = (e) => e.detail.target?.id === "results";
  const markError = (on) => {
    if (on) box.setAttribute("aria-invalid", "true");
    else box.removeAttribute("aria-invalid");
    box.setAttribute("aria-describedby", on ? "mod-search-hint mod-paste-hint mod-search-error" : "mod-search-hint mod-paste-hint");
  };
  const say = (text) => {
    const announcer = document.getElementById("announcer");
    if (!announcer || !text) return;
    // Emptied first and filled on the next frame, so the same words are read out again (DESIGN 7.5).
    announcer.textContent = "";
    requestAnimationFrame(() => {
      announcer.textContent = text;
    });
  };
  // Under 2 characters the old results no longer match the box, so they are
  // hidden (their ticked boxes still go with the form) and said so once.
  box.addEventListener("input", () => {
    const results = document.getElementById("results");
    // Nothing shown means nothing to clear, so nothing is said.
    if (!short() || !results || !results.firstElementChild || results.style.display === "none") return;
    // An inline style, as .panels sets display and would beat the hidden attribute.
    results.style.display = "none";
    // On the Mods tab the results' own form would keep an empty grid row.
    const wrap = results.closest(".mod-results");
    if (wrap) wrap.style.display = "none";
    markError(false);
    say(box.dataset.short);
  });
  document.addEventListener("htmx:beforeRequest", (e) => {
    if (isResults(e) && short()) e.preventDefault();
  });
  document.addEventListener("htmx:beforeSwap", (e) => {
    if (!isResults(e)) return;
    // A refused or unreachable search still shows its message in place.
    if (e.detail.xhr.status >= 400) e.detail.shouldSwap = true;
    // A reply that lands after the box went under 2 characters is stale.
    if (short()) e.detail.shouldSwap = false;
  });
  document.addEventListener("htmx:afterSettle", (e) => {
    if (!isResults(e)) return;
    const results = document.getElementById("results");
    if (!results) return;
    results.closest(".mod-results")?.style.removeProperty("display");
    markError(!!document.getElementById("mod-search-error"));
    say(results.querySelector("#mod-search-error, caption, .empty, .message")?.textContent.trim());
  });
})();
