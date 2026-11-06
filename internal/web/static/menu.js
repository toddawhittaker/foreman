// The user menu (DESIGN 6). It is a native details element and works
// without this script; the script only closes it on Escape or a click
// outside, as people expect of a menu.
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
})();
