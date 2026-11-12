// Delete selected waits for a ticked box. Without script the
// button stays enabled and the server says when nothing was ticked.
(() => {
  "use strict";
  for (const button of document.querySelectorAll("[data-needs-tick]")) {
    const form = button.form;
    if (!form) continue;
    const sync = () => {
      button.disabled = !form.querySelector('input[name="snap"]:checked');
    };
    form.addEventListener("change", sync);
    sync();
  }
})();
