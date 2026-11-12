// The user menu (DESIGN 6). It is a native details element, with the
// language list a details element inside it, and works without this script;
// the script closes it on Escape, on a click outside, and when focus moves
// on to something outside it, so that on a phone the open panel never
// covers the control that takes focus (WCAG 2.4.11). Escape inside the open
// language list folds the list first, back to its own row.
(() => {
  "use strict";
  const menu = document.querySelector("details.menu");
  if (!menu) return;
  const langs = menu.querySelector("details.menu__langs");
  // The menu opens with its language list folded, as it was first drawn. It
  // folds as the menu opens, not as it closes, so that a closed menu is the
  // nearest closed details around a language button that had focus.
  menu.querySelector(":scope > summary").addEventListener("click", () => {
    if (!menu.open && langs) langs.open = false;
  });
  document.addEventListener("keydown", (e) => {
    if (e.key !== "Escape" || !menu.open) return;
    if (langs && langs.open && langs.contains(document.activeElement)) {
      langs.open = false;
      langs.querySelector("summary").focus();
      return;
    }
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
