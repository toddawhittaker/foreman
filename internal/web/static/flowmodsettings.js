// Step 3 of the guided Generate (SPEC 5.6): saving the mod settings can take
// a game run, so once the form is sent its save button is disabled and a
// status line says the save is under way (SPEC 17.19). A page brought back
// from the back-forward cache is ready again.
(() => {
  "use strict";
  const status = document.getElementById("flow-modsettings-saving");
  if (!status) return;
  const buttons = () => document.querySelectorAll("#mod-settings-form button[type=submit]");
  document.addEventListener("submit", (e) => {
    if (e.defaultPrevented || !e.target.closest("#mod-settings-form")) return;
    // After the submit, so the button's own press still sends the form.
    setTimeout(() => {
      buttons().forEach((b) => { b.disabled = true; b.setAttribute("aria-busy", "true"); });
      status.textContent = status.dataset.text;
    }, 0);
  });
  window.addEventListener("pageshow", (e) => {
    if (!e.persisted) return;
    buttons().forEach((b) => { b.disabled = false; b.removeAttribute("aria-busy"); });
    status.textContent = "";
  });
})();
