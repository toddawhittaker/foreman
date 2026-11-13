// Delete selected waits for a ticked box: its bar shows, with the number
// ticked, once there is one. Without script the button stays in view and
// enabled, and the server says when nothing was ticked.
(() => {
  "use strict";
  for (const button of document.querySelectorAll("[data-needs-tick]")) {
    const form = button.form;
    if (!form) continue;
    const bar = button.closest("[data-select-bar]");
    const count = bar && bar.querySelector("[data-tick-count]");
    const sync = () => {
      const ticked = form.querySelectorAll('input[name="snap"]:checked').length;
      button.disabled = ticked === 0;
      if (bar) bar.hidden = ticked === 0;
      if (count) count.textContent = String(ticked);
    };
    form.addEventListener("change", sync);
    sync();
  }
})();
