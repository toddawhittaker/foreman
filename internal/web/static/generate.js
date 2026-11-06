// Keeps the Generate form in step with what the browser redraws (SPEC 5.6,
// 17.3; DESIGN 8.8). Choosing a version from the list clears the typed one,
// which would win. After the map options are redrawn for a new game line, or
// a preview is made, the change is read out. A form shown again with errors
// gets focus on its error summary; an old summary is removed when a redraw
// leaves the form in place. While a preview runs the buttons are marked
// aria-disabled rather than disabled, so the one the person pressed keeps
// focus.
document.addEventListener("DOMContentLoaded", () => {
  const strings = JSON.parse(document.getElementById("i18n").textContent);

  function announce(said) {
    const announcer = document.getElementById("announcer");
    if (!announcer) return;
    announcer.textContent = "";
    setTimeout(() => { announcer.textContent = said; }, 50);
  }

  document.body.addEventListener("change", (e) => {
    if (e.target.id !== "generate-version-select") return;
    const typed = document.getElementById("generate-version");
    if (typed) typed.value = "";
  });

  const actions = "#preview-button, #generate-button";
  const isPreview = (e) => e.detail.elt && e.detail.elt.id === "preview-button";

  function setBusy(on) {
    document.querySelectorAll(actions).forEach((b) => {
      if (on) b.setAttribute("aria-disabled", "true");
      else b.removeAttribute("aria-disabled");
    });
  }

  // A busy button does nothing, and also stops the Enter key's implicit submit.
  document.addEventListener("click", (e) => {
    const button = e.target.closest(actions);
    if (button && button.getAttribute("aria-disabled") === "true") {
      e.preventDefault();
      e.stopImmediatePropagation();
    }
  }, true);

  document.body.addEventListener("htmx:beforeRequest", (e) => { if (isPreview(e)) setBusy(true); });
  document.body.addEventListener("htmx:afterRequest", (e) => { if (isPreview(e)) setBusy(false); });

  // htmx swaps nothing on a 4xx, a 5xx or a lost connection, so say so.
  function failed(e) {
    if (!isPreview(e)) return;
    const slot = document.getElementById("preview");
    const message = document.getElementById("preview-failed");
    if (!slot || !message) return;
    slot.replaceChildren(message.content.cloneNode(true));
    announce(slot.querySelector(".message").textContent.replace(/\s+/g, " ").trim());
  }
  document.body.addEventListener("htmx:responseError", failed);
  document.body.addEventListener("htmx:sendError", failed);

  document.body.addEventListener("htmx:afterSwap", (e) => {
    const path = e.detail.pathInfo ? e.detail.pathInfo.requestPath : "";
    // The version list's own load is also a swap; it must not touch the summary.
    if (!path.endsWith("/generate/refresh") && !path.endsWith("/generate/preview")) return;
    const xhr = e.detail.xhr;
    const summary = document.getElementById("error-summary");
    // The form came back whole, with its errors: they are what to read next.
    if (xhr && xhr.getResponseHeader("HX-Retarget") === "#generate-form") {
      if (summary) summary.focus();
      return;
    }
    // Otherwise only a part was redrawn, so a summary left in the form is out of date.
    if (summary) summary.remove();
    if (path.endsWith("/generate/refresh")) {
      // Choosing a preset refills the hidden custom form; nothing is shown to read out.
      if (!(e.detail.elt && e.detail.elt.id === "generate-preset")) announce(strings["js.generate.options"]);
    } else if (path.endsWith("/generate/preview")) {
      const failed = document.querySelector("#preview .message");
      announce(failed ? failed.textContent.replace(/\s+/g, " ").trim() : strings["js.generate.preview_ready"]);
    }
  });
});
