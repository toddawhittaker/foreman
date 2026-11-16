// Copy buttons (SPEC 3.2, 5.6). Browsers give the Clipboard API only on HTTPS
// or localhost, so a button shows only where it works; elsewhere the text is
// plain text to select. A button carries its text in data-copy, or names a
// field to read in data-copy-from; data-done names the element that says it
// was copied, "copy-done" when absent.
(() => {
  "use strict";
  if (!window.isSecureContext || !navigator.clipboard?.writeText) return;
  const buttons = document.querySelectorAll("button[data-copy], button[data-copy-from]");
  for (const button of buttons) {
    const done = document.getElementById(button.dataset.done || "copy-done");
    if (!done) continue;
    button.hidden = false;
    button.addEventListener("click", async () => {
      const field = button.dataset.copyFrom && document.getElementById(button.dataset.copyFrom);
      try {
        await navigator.clipboard.writeText(field ? field.value : button.dataset.copy);
      } catch (_) {
        return;
      }
      done.hidden = false;
      const announcer = document.getElementById("announcer");
      if (!announcer) return;
      // Emptied first and filled on the next frame, so a second press is read out again (DESIGN 7.5).
      announcer.textContent = "";
      requestAnimationFrame(() => {
        announcer.textContent = done.textContent;
      });
    });
  }
})();
