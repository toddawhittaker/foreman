// Copy address on the Server page (SPEC 3.2). Browsers give the Clipboard
// API only on HTTPS or localhost, so the button shows only where it works;
// elsewhere the address is plain text to select.
(() => {
  "use strict";
  const button = document.getElementById("copy-address");
  const done = document.getElementById("copy-done");
  if (!button || !done || !window.isSecureContext || !navigator.clipboard?.writeText) return;
  button.hidden = false;
  button.addEventListener("click", async () => {
    try {
      await navigator.clipboard.writeText(button.dataset.copy);
    } catch (_) {
      return;
    }
    done.hidden = false;
    const announcer = document.getElementById("announcer");
    if (announcer) announcer.textContent = done.textContent;
  });
})();
