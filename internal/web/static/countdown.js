// Stop countdowns (SPEC 7.6). Counts each "N seconds left" down in place from
// its data-until time, changing only that text, so the buttons beside it stay
// the same elements and a screen reader is not interrupted (DESIGN 7.6).
// Without this script the text shows the time left when the page was sent.
(() => {
  "use strict";

  const t = window.foremanText;

  // The same words as the server's stepList.Left.
  function left(until) {
    const n = Math.ceil((until - Date.now()) / 1000);
    if (n <= 0) return t("server.step.ending_now");
    return t("server.step.seconds_left", { count: n });
  }

  // One timer for the page; a live update that swaps in a new list is picked
  // up on the next tick.
  setInterval(() => {
    for (const el of document.querySelectorAll("[data-until]")) {
      const until = Date.parse(el.dataset.until);
      if (Number.isNaN(until)) continue;
      const text = left(until);
      if (el.textContent !== text) el.textContent = text;
    }
  }, 250);
})();
