// Toasts (DESIGN 6, "Toasts"): result messages over the page. A success is
// read out through #announcer and closes itself after a while, waiting while
// the pointer or focus is on it; an error is an alert and stays until closed.
// No toast ever takes focus. A toast that comes after the page is the same
// partial, swapped into #toasts by htmx (hx-swap-oob="beforeend:#toasts").
(() => {
  "use strict";
  const region = document.getElementById("toasts");
  if (!region) return;
  // At least 6 s, counted again each time the pointer or focus leaves it.
  const successMs = 8000;
  // Where focus goes back to when the toast that held it closes.
  let lastOutside = null;
  document.addEventListener("focusin", (e) => {
    if (!region.contains(e.target)) lastOutside = e.target;
  });

  function close(toast) {
    const held = toast.contains(document.activeElement);
    toast.remove();
    if (!held) return;
    const back = lastOutside && lastOutside.isConnected ? lastOutside : document.getElementById("main");
    if (back) back.focus();
  }

  // Emptied first and filled on the next frame, so the same words twice are
  // still read (DESIGN 7.5).
  function announce(text) {
    const announcer = document.getElementById("announcer");
    if (!announcer) return;
    announcer.textContent = "";
    requestAnimationFrame(() => {
      announcer.textContent = text;
    });
  }

  function arm(toast) {
    if (toast.hasAttribute("data-armed")) return;
    toast.setAttribute("data-armed", "");
    const button = toast.querySelector("[data-toast-close]");
    if (button) button.addEventListener("click", () => close(toast));
    const words = toast.querySelector(".toast__text");
    if (toast.classList.contains("toast--error")) {
      // An alert reads out what changes inside it, so the words go in again.
      toast.setAttribute("role", "alert");
      if (words) words.replaceWith(words.cloneNode(true));
      return;
    }
    if (words) announce(words.textContent.replace(/\s+/g, " ").trim());
    let timer = 0;
    let hovered = false;
    let focused = false;
    const wait = () => {
      clearTimeout(timer);
      timer = 0;
    };
    const run = () => {
      if (timer || hovered || focused) return;
      timer = setTimeout(() => close(toast), successMs);
    };
    toast.addEventListener("mouseenter", () => {
      hovered = true;
      wait();
    });
    toast.addEventListener("mouseleave", () => {
      hovered = false;
      run();
    });
    toast.addEventListener("focusin", () => {
      focused = true;
      wait();
    });
    toast.addEventListener("focusout", (e) => {
      if (toast.contains(e.relatedTarget)) return;
      focused = false;
      run();
    });
    run();
  }

  new MutationObserver((changes) => {
    for (const c of changes) for (const n of c.addedNodes) if (n.nodeType === 1 && n.matches("[data-toast]")) arm(n);
  }).observe(region, { childList: true });
  for (const toast of region.querySelectorAll("[data-toast]")) arm(toast);
})();
