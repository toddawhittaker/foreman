// Stop countdowns (SPEC 7.6). Counts each "N seconds left" down in place from
// its data-until time, changing only that text, so the buttons beside it stay
// the same elements and a screen reader is not interrupted (DESIGN 7.6).
// Without this script the text shows the time left when the page was sent.
(() => {
  "use strict";

  // Text from the page's #i18n block: a plural entry is picked by the
  // page's language, and {name} placeholders are filled in.
  const lang = document.documentElement.lang;
  const strings = JSON.parse(document.getElementById("i18n").textContent);
  function t(key, args = {}) {
    let text = strings[key];
    if (typeof text === "object") text = text[new Intl.PluralRules(lang).select(args.count)] ?? text.other;
    return text.replace(/\{(\w+)\}/g, (_, name) => (typeof args[name] === "number" ? args[name].toLocaleString(lang) : args[name]));
  }

  // The same words as the server's stepList.Left.
  function left(until) {
    const n = Math.ceil((until - Date.now()) / 1000);
    if (n <= 0) return t("js.countdown.ending");
    return t("js.countdown.left", { count: n });
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
