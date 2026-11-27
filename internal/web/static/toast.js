// Toasts (DESIGN 6, "Toasts"): result messages over the page. A success is
// read out through #announcer and closes itself after a while, waiting while
// the pointer or focus is on it; an error is read out through #alerts and
// stays until closed. No toast ever takes focus. A toast that comes after the
// page is the "toast-oob" partial in an htmx answer, newest first in #toasts;
// one in an error answer, which htmx does not swap, is taken from it here.
(() => {
  "use strict";
  const inRegion = "#toasts > [data-toast]";
  // At least 6 s, counted again each time the pointer or focus leaves it.
  const successMs = 8000;
  // Where focus goes back to when the toast that held it closes.
  let lastOutside = null;
  document.addEventListener("focusin", (e) => {
    if (!e.target.closest("#toasts")) lastOutside = e.target;
  });

  function close(toast) {
    const held = toast.contains(document.activeElement);
    toast.remove();
    if (!held) return;
    const back = lastOutside && lastOutside.isConnected ? lastOutside : document.getElementById("main");
    if (back) back.focus();
  }

  // Emptied first and filled on the next frame, so the same words twice are
  // still read (DESIGN 7.5). Toasts that arrive together are read together.
  const pending = new Map();
  function announce(id, text) {
    const region = document.getElementById(id);
    if (!region) return;
    if (pending.has(region)) {
      pending.get(region).push(text);
      return;
    }
    pending.set(region, [text]);
    region.textContent = "";
    requestAnimationFrame(() => {
      region.textContent = pending.get(region).join(" ");
      pending.delete(region);
    });
  }

  function arm(toast) {
    if (toast.hasAttribute("data-armed")) return;
    toast.setAttribute("data-armed", "");
    const button = toast.querySelector("[data-toast-close]");
    if (button) button.addEventListener("click", () => close(toast));
    const words = toast.querySelector(".toast__text");
    const text = words ? words.textContent.replace(/\s+/g, " ").trim() : "";
    if (toast.classList.contains("toast--error")) {
      if (text) announce("alerts", text);
      return;
    }
    if (text) announce("announcer", text);
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

  // The stack's height, so a control scrolled into view clears it (WCAG
  // 2.4.11). A swap may replace the region, so the newest one is measured.
  let measured = null;
  const sizes = new ResizeObserver(([entry]) => {
    document.documentElement.style.setProperty("--toast-room", `${Math.ceil(entry.borderBoxSize[0].blockSize)}px`);
  });
  function watch() {
    const region = document.getElementById("toasts");
    if (region === measured) return;
    if (measured) sizes.unobserve(measured);
    measured = region;
    if (region) sizes.observe(region);
  }

  function armAll(root) {
    if (root.matches && root.matches(inRegion)) arm(root);
    if (root.querySelectorAll) for (const t of root.querySelectorAll(inRegion)) arm(t);
  }
  new MutationObserver((changes) => {
    for (const c of changes) for (const n of c.addedNodes) if (n.nodeType === 1) armAll(n);
    watch();
  }).observe(document.body, { childList: true, subtree: true });
  armAll(document);
  watch();

  // htmx swaps nothing from a 4xx or 5xx answer, out of band included, so its
  // toasts are added here; everything else stays as htmx and the page left
  // it. On window, this runs after every page's own say on the swap.
  window.addEventListener("htmx:beforeSwap", (e) => {
    const d = e.detail;
    if (d.shouldSwap || !d.xhr || d.xhr.status < 400) return;
    const region = document.getElementById("toasts");
    if (!region) return;
    const answer = document.createElement("template");
    answer.innerHTML = d.serverResponse || "";
    for (const t of answer.content.querySelectorAll("[hx-swap-oob] > [data-toast]")) region.prepend(document.importNode(t, true));
  });

  // Escape closes the newest toast, unless something else owns the key: an
  // open menu or dialog, or a field that uses it.
  const owners = ["details[open]", "dialog", "[role=dialog]", "[role=menu]", "[role=listbox]", "[aria-expanded=true]", "textarea", "select", "[contenteditable]"].join();
  const keyedInput = "input:not([type=button], [type=submit], [type=reset], [type=checkbox], [type=radio], [type=range], [type=color], [type=file], [type=image])";
  document.addEventListener(
    "keydown",
    (e) => {
      if (e.key !== "Escape" || e.defaultPrevented) return;
      const at = document.activeElement;
      if (at && at.closest && (at.closest(owners) || at.matches(keyedInput))) return;
      if (document.querySelector("details.menu[open], details:is(.row-menu, .emergency)[open], dialog[open]")) return;
      const newest = document.querySelector(inRegion);
      if (newest) close(newest);
    },
    true,
  );
})();
