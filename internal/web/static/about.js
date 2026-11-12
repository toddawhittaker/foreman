// The user menu's About item (DESIGN 6, "User menu"). Without this script it
// is a link to the About page. With it, the item opens the same content in
// the page's native modal dialog: the browser moves focus into it, keeps it
// inside and closes the dialog on Escape. When it closes, focus goes back to
// the menu's button: the menu closes as focus moves into the dialog, and a
// closed menu hides its items.
(() => {
  "use strict";
  const dialog = document.getElementById("about-dialog");
  const link = document.querySelector("a[data-about]");
  const close = document.getElementById("about-close");
  if (!dialog || !link || !close || typeof dialog.showModal !== "function") return;

  // On the window in the capture phase, so it runs ahead of any page's own
  // document-level link handlers whatever the script order (Generate asks
  // before leaving the page): opening the dialog leaves nothing.
  window.addEventListener("click", (e) => {
    if (e.button !== 0 || e.ctrlKey || e.metaKey || e.shiftKey || e.altKey) return;
    if (!e.target.closest || e.target.closest("a[data-about]") !== link) return;
    e.preventDefault();
    e.stopImmediatePropagation();
    dialog.showModal();
    close.focus();
  }, true);

  close.addEventListener("click", () => dialog.close());
  // A press on the dim area outside the box, which the dialog element itself receives.
  dialog.addEventListener("click", (e) => {
    if (e.target === dialog) dialog.close();
  });
  // The browser keeps focus out of the page behind the dialog, but Tab from its
  // last control would leave the document for the browser's own controls. The
  // dialog wraps instead, both ways, so focus stays inside it.
  dialog.addEventListener("keydown", (e) => {
    if (e.key !== "Tab") return;
    const items = [...dialog.querySelectorAll("a[href], button:not([disabled])")];
    if (items.length === 0) return;
    const first = items[0];
    const last = items[items.length - 1];
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault();
      first.focus();
    }
  });
  dialog.addEventListener("close", () => {
    link.closest("details").querySelector(":scope > summary").focus();
  });
})();
