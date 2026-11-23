// Toggletips (DESIGN 7.7): an (i) button shows the text that explains a
// setting. A click or tap toggles it, keyboard focus and mouse hover show it,
// and Escape, a tap elsewhere or leaving the button closes it. One is open at
// a time, and it is placed under its button inside the window. Without this
// script the text shows under its field. Listeners sit on the document, so
// tips in a part the page redraws need nothing more. An open tip sits in the
// top layer as a popover, so no scrolling or clipping box around it (the
// Generate preview pane) cuts it off or grows a scroll bar (DESIGN 7.7).
document.addEventListener("DOMContentLoaded", () => {
  const edge = 8; // px kept between a tip and the window's edge
  let open = null; // { button, text, pinned }
  let hoverTimer = 0;

  const textOf = (button) => document.getElementById(button.getAttribute("aria-controls"));

  function place(button, text) {
    // The top layer places from the window; elsewhere from the positioned parent.
    const layered = text.matches(":popover-open");
    const field = layered ? null : text.offsetParent;
    if (!layered && !field) return;
    // The window less its scroll bar.
    const width = document.documentElement.clientWidth;
    const height = document.documentElement.clientHeight;
    text.style.left = "0px";
    text.style.top = "0px";
    text.style.setProperty("--tip-room", `${width - 2 * edge}px`);
    const f = field ? field.getBoundingClientRect() : { left: 0, top: 0 };
    const b = button.getBoundingClientRect();
    const t = text.getBoundingClientRect();
    let left = Math.min(b.left, width - edge - t.width);
    left = Math.max(left, edge);
    let top = b.bottom + 4;
    // Above the button when there is no room under it but there is above.
    if (top + t.height > height - edge && b.top - 4 - t.height >= edge) top = b.top - 4 - t.height;
    text.style.left = `${left - f.left}px`;
    text.style.top = `${top - f.top}px`;
  }

  function show(button, pinned) {
    const text = textOf(button);
    if (!text) return;
    if (open && open.button !== button) hide();
    if (open) {
      open.pinned = open.pinned || pinned;
      return;
    }
    open = { button, text, pinned };
    button.setAttribute("aria-expanded", "true");
    text.classList.add("is-open");
    if (typeof text.showPopover === "function") {
      text.setAttribute("popover", "manual");
      text.showPopover();
    }
    place(button, text);
  }

  function hide() {
    if (!open) return;
    open.button.setAttribute("aria-expanded", "false");
    open.text.classList.remove("is-open");
    if (open.text.matches(":popover-open")) open.text.hidePopover();
    open = null;
  }

  const tipButton = (el) => (el instanceof Element ? el.closest("button.tip") : null);

  document.addEventListener("click", (e) => {
    const button = tipButton(e.target);
    if (!button) return;
    // Enter and Space (detail 0) toggle by whether the tip is shown, since focus
    // already showed it. A pointer click on a tip that hover showed pins it.
    const shown = open && open.button === button;
    if (shown && (open.pinned || e.detail === 0)) hide();
    else show(button, true);
  });

  // Keyboard focus shows the tip; a mouse click's focus is left to the click.
  document.addEventListener("focusin", (e) => {
    const button = tipButton(e.target);
    if (button && button.matches(":focus-visible")) show(button, false);
  });
  // Focus moving to another control closes it; a press on its own text is left to pointerdown.
  document.addEventListener("focusout", (e) => {
    if (open && e.target === open.button && e.relatedTarget) hide();
  });

  // Hover with a mouse shows it; moving onto the text keeps it.
  document.addEventListener("pointerover", (e) => {
    if (e.pointerType !== "mouse") return;
    const button = tipButton(e.target);
    if (button || (open && open.text.contains(e.target))) {
      clearTimeout(hoverTimer);
      if (button) show(button, false);
    }
  });
  document.addEventListener("pointerout", (e) => {
    if (e.pointerType !== "mouse" || !open || open.pinned) return;
    if (!open.button.contains(e.target) && !open.text.contains(e.target)) return;
    clearTimeout(hoverTimer);
    hoverTimer = setTimeout(() => {
      if (open && !open.pinned && document.activeElement !== open.button) hide();
    }, 150);
  });

  // A press elsewhere closes it. A touch on its text closes it too, because on
  // a phone the text can cover the field it explains; a mouse may select the text.
  document.addEventListener("pointerdown", (e) => {
    if (!open || open.button.contains(e.target)) return;
    if (!open.text.contains(e.target) || e.pointerType !== "mouse") hide();
  });

  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape" && open) {
      hide();
      e.preventDefault();
    }
  });

  window.addEventListener("resize", () => {
    if (open) place(open.button, open.text);
  });
  // A tip in the top layer is fixed to the window, so it follows its button when any box scrolls.
  document.addEventListener("scroll", () => {
    if (open) place(open.button, open.text);
  }, { capture: true, passive: true });
});
