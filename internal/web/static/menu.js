// The user menu (DESIGN 6). It is a native details element and works
// without this script; the script closes it on Escape, on a click outside,
// and when focus moves on to something outside it, so that on a phone the
// open panel never covers the control that takes focus (WCAG 2.4.11).
(() => {
  "use strict";
  const menu = document.querySelector("details.menu");
  if (!menu) return;
  document.addEventListener("keydown", (e) => {
    if (e.key !== "Escape" || !menu.open) return;
    menu.open = false;
    menu.querySelector("summary").focus();
  });
  document.addEventListener("click", (e) => {
    if (menu.open && !menu.contains(e.target)) menu.open = false;
  });
  // A null relatedTarget is a press on a part that cannot take focus, which
  // the click handler above deals with; closing here would drop the press.
  menu.addEventListener("focusout", (e) => {
    if (menu.open && e.relatedTarget && !menu.contains(e.relatedTarget)) menu.open = false;
  });
})();
