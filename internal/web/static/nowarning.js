// The No warning button beside the countdown field (SPEC 7.6). It sets the
// field to 0 and moves focus there, so a screen reader hears the new value.
// Without script the button stays hidden and the person types 0.
(() => {
  "use strict";
  const button = document.getElementById("countdown-zero");
  const field = document.getElementById("countdown");
  if (!button || !field) return;
  button.hidden = false;
  button.addEventListener("click", () => {
    field.value = "0";
    field.focus();
  });
})();
