// Tab rows that scroll sideways (DESIGN 6, "Sub-navigation"): the current
// tab is brought fully into view, and data-more names the edge that has
// more tabs past it, so the style sheet can fade that edge.
(() => {
  "use strict";
  const rows = document.querySelectorAll(".subnav ul, .tablist");
  for (const row of rows) {
    const mark = () => {
      const max = row.scrollWidth - row.clientWidth;
      // Scroll positions are fractional, and negative in right-to-left text.
      const at = Math.abs(row.scrollLeft);
      const start = at > 1;
      const end = max - at > 1;
      if (start && end) row.dataset.more = "both";
      else if (start) row.dataset.more = "start";
      else if (end) row.dataset.more = "end";
      else delete row.dataset.more;
    };
    const current = row.querySelector("[aria-current], :checked + *, [aria-selected='true']");
    if (current && row.scrollWidth > row.clientWidth) {
      current.scrollIntoView({ block: "nearest", inline: "nearest" });
    }
    mark();
    row.addEventListener("scroll", mark, { passive: true });
    window.addEventListener("resize", mark);
  }
})();

// A disclosure that is folded only on a phone (the Server page's Emergency
// actions): on a wide screen the script opens it and marks it, so the style
// sheet hides its summary. Without the script it stays a working disclosure.
// data-open-wide may name the width, such as "64rem" for the Mods page's
// tools, which sit in a rail only once the list has room beside them.
(() => {
  "use strict";
  const queries = new Map();
  const wideFor = (el) => {
    const at = el.dataset.openWide || "40rem";
    if (!queries.has(at)) {
      const q = window.matchMedia(`(min-width: ${at})`);
      q.addEventListener("change", () => apply());
      queries.set(at, q);
    }
    return queries.get(at);
  };
  const apply = () => {
    for (const el of document.querySelectorAll("details[data-open-wide]")) {
      if (wideFor(el).matches) {
        el.open = true;
        el.setAttribute("data-wide", "");
      } else if (el.hasAttribute("data-wide")) {
        el.open = false;
        el.removeAttribute("data-wide");
      }
    }
  };
  apply();
  document.addEventListener("htmx:afterSwap", apply);
})();
