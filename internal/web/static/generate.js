// Keeps the Generate form in step with what the browser redraws (SPEC 5.6).
// Choosing a version from the list clears the typed one, which would win.
// After the map options are redrawn for a new game line, or a preview is
// made, the change is read out; a form shown again with errors gets focus
// on its error summary.
document.addEventListener("DOMContentLoaded", () => {
  const strings = JSON.parse(document.getElementById("i18n").textContent);

  function announce(said) {
    const announcer = document.getElementById("announcer");
    if (!announcer) return;
    announcer.textContent = "";
    setTimeout(() => { announcer.textContent = said; }, 50);
  }

  const select = document.getElementById("generate-version-select");
  const typed = document.getElementById("generate-version");
  if (select && typed) {
    select.addEventListener("change", () => { typed.value = ""; });
  }

  document.body.addEventListener("htmx:afterSwap", (e) => {
    const path = e.detail.pathInfo ? e.detail.pathInfo.requestPath : "";
    const summary = document.getElementById("error-summary");
    if (summary) {
      summary.focus();
      return;
    }
    if (path.endsWith("/generate/refresh")) {
      announce(strings["js.generate.options"]);
    } else if (path.endsWith("/generate/preview")) {
      const failed = document.querySelector("#preview .message");
      announce(failed ? failed.textContent.trim() : strings["js.generate.preview_ready"]);
    }
  });
});
