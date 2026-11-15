// Search as you type (SPEC 9.1). htmx replaces #results; this keeps the box's
// error state in step and reads the outcome out through the shared announcer.
(() => {
  "use strict";
  const box = document.getElementById("mod-search");
  if (!box) return;
  const short = () => box.value.trim().length < 2;
  const isResults = (e) => e.detail.target?.id === "results";
  // Under 2 characters the old results no longer match the box, so they are
  // hidden (their ticked boxes still go with the form) and a late reply is dropped.
  box.addEventListener("input", () => {
    if (!short()) return;
    const results = document.getElementById("results");
    // An inline style, as .panels sets display and would beat the hidden attribute.
    if (results) results.style.display = "none";
  });
  document.addEventListener("htmx:beforeRequest", (e) => {
    if (isResults(e) && short()) e.preventDefault();
  });
  // A refused or unreachable search still shows its message in place.
  document.addEventListener("htmx:beforeSwap", (e) => {
    if (isResults(e) && e.detail.xhr.status >= 400) e.detail.shouldSwap = true;
  });
  document.addEventListener("htmx:afterSettle", (e) => {
    if (!isResults(e)) return;
    const results = document.getElementById("results");
    if (!results) return;
    const error = document.getElementById("mod-search-error");
    if (error) {
      box.setAttribute("aria-invalid", "true");
      box.setAttribute("aria-describedby", "mod-search-hint mod-paste-hint mod-search-error");
    } else {
      box.removeAttribute("aria-invalid");
      box.setAttribute("aria-describedby", "mod-search-hint mod-paste-hint");
    }
    const announcer = document.getElementById("announcer");
    const said = results.querySelector("#mod-search-error, caption, .empty, .message")?.textContent.trim();
    if (!announcer || !said) return;
    // Emptied first and filled on the next frame, so the same words are read out again (DESIGN 7.5).
    announcer.textContent = "";
    requestAnimationFrame(() => {
      announcer.textContent = said;
    });
  });
})();
