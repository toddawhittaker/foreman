// Search as you type and the plan panel (SPEC 9.1, 9.2). htmx replaces
// #results and #plan-panel; this keeps the box's error state in step, reads
// each outcome out through the shared announcer and puts focus where the
// hand is.
(() => {
  "use strict";
  const say = (text) => {
    const announcer = document.getElementById("announcer");
    if (!announcer || !text) return;
    // Emptied first and filled on the next frame, so the same words are read out again (DESIGN 7.5).
    announcer.textContent = "";
    requestAnimationFrame(() => {
      announcer.textContent = text;
    });
  };
  const markError = (on) => {
    const box = document.getElementById("mod-search");
    if (!box) return;
    if (on) box.setAttribute("aria-invalid", "true");
    else box.removeAttribute("aria-invalid");
    box.setAttribute("aria-describedby", on ? "mod-search-hint mod-paste-hint mod-search-error" : "mod-search-hint mod-paste-hint");
  };
  // Changes to the plan go one at a time: each reply redraws the panel and the
  // rows, so a second change sent meanwhile would act on controls the first
  // replaces. A change asked for while one runs waits, then is sent from the
  // redrawn page if it still applies.
  const isPlanForm = (el) => el instanceof HTMLFormElement && /\/mods\/plan\/(mods|clear)$/.test(el.getAttribute("hx-post") || "");
  const fields = (form) => ["hx-post", "name", "op", "from"].map((k) => (k === "hx-post" ? form.getAttribute(k) : form.elements[k]?.value ?? ""));
  let planBusy = false;
  let waiting = null;
  // While a change runs, Review would open the plan without it, so it is held
  // back and says why (aria-disabled keeps it in the tab order).
  const holdReview = (on) => {
    const review = document.getElementById("act-review-plan") || document.querySelector("#flow-next[data-review]");
    const wait = document.getElementById("plan-wait");
    if (!review || !wait) return;
    wait.hidden = !on;
    if (on) {
      review.setAttribute("aria-disabled", "true");
      review.setAttribute("aria-describedby", "plan-wait");
    } else {
      review.removeAttribute("aria-disabled");
      review.removeAttribute("aria-describedby");
    }
  };
  // Enter searches at once, but the typing's delayed search for the same text
  // still fires after it; by then a page control may have asked for another
  // page, which that late reply would replace with page 1. A search for the
  // text already searched is dropped; one that got no answer does not
  // count, so typing that text again sends it again.
  let searched = null;
  const isSearch = (elt) => {
    const box = document.getElementById("mod-search");
    return !!box && (elt === box || elt === box.form);
  };
  document.addEventListener("htmx:confirm", (e) => {
    const elt = e.detail.elt;
    if (!isSearch(elt)) return;
    const box = document.getElementById("mod-search");
    if (elt === box && box.value === searched) {
      e.preventDefault();
      return;
    }
    searched = box.value;
  });
  for (const ev of ["htmx:sendError", "htmx:responseError", "htmx:timeout"]) {
    document.addEventListener(ev, (e) => {
      if (isSearch(e.detail.elt)) searched = null;
    });
  }
  document.addEventListener("click", (e) => {
    if (e.target.closest?.('#act-review-plan[aria-disabled="true"], #flow-next[aria-disabled="true"]')) e.preventDefault();
  });
  const planDone = () => {
    planBusy = false;
    holdReview(false);
    if (!waiting) return;
    const want = waiting.join("\n");
    waiting = null;
    const next = [...document.querySelectorAll("form[hx-post]")].find((f) => isPlanForm(f) && fields(f).join("\n") === want);
    if (next) window.htmx.trigger(next, "submit");
  };
  document.addEventListener("htmx:confirm", (e) => {
    const form = e.detail.elt;
    if (!isPlanForm(form)) return;
    // The row's "Adding…" is read out too, not only shown.
    const busy = form.querySelector(".hit__busy");
    if (busy) say(busy.textContent.trim());
    holdReview(true);
    if (!planBusy) {
      planBusy = true;
      return;
    }
    e.preventDefault();
    waiting = fields(form);
    // It shows as working while it waits.
    form.classList.add("htmx-request");
  });
  for (const ev of ["htmx:responseError", "htmx:sendError", "htmx:timeout", "htmx:swapError"]) {
    document.addEventListener(ev, (e) => {
      // A refused "+" (400) redraws the panel, which ends the change there.
      if (isPlanForm(e.detail.elt) && e.detail.xhr?.status !== 400) planDone();
    });
  }
  // On a phone the open list folds again once focus leaves the panel, so it
  // does not hide the page behind it (WCAG 2.4.11).
  document.addEventListener("focusout", (e) => {
    const panel = document.getElementById("plan-panel");
    const list = document.getElementById("plan-list");
    if (!panel || !list || !list.open || list.hasAttribute("data-wide") || !panel.contains(e.target)) return;
    if (e.relatedTarget && !panel.contains(e.relatedTarget)) list.open = false;
  });
  // The phone bar keeps its open state through a redraw of the plan.
  let listOpen = false;
  let paged = false;
  document.addEventListener("htmx:beforeRequest", (e) => {
    paged = !!e.detail.elt.closest?.(".result-pager");
  });
  document.addEventListener("htmx:beforeSwap", (e) => {
    const id = e.detail.target?.id;
    // A refused or unreachable search still shows its message in place.
    if (id === "results" && e.detail.xhr.status >= 400) e.detail.shouldSwap = true;
    // A refused "+" says why in the panel.
    if (id === "plan-panel" && e.detail.xhr.status === 400) e.detail.shouldSwap = true;
    // A typed search's reply that no longer matches the box is stale.
    const box = document.getElementById("mod-search");
    const elt = e.detail.requestConfig?.elt;
    if (id === "results" && box && elt && (elt === box || elt.contains(box))) {
      const asked = new URL(e.detail.pathInfo?.finalRequestPath ?? "", location.href).searchParams.get("q") ?? "";
      if (asked.trim() !== box.value.trim()) e.detail.shouldSwap = false;
    }
    if (id === "plan-panel") listOpen = !!document.querySelector("#plan-list:not([data-wide])[open]");
  });
  document.addEventListener("htmx:afterSettle", (e) => {
    const results = document.getElementById("results");
    if (results && e.detail.target?.id === "results") {
      markError(!!document.getElementById("mod-search-error"));
      say(results.querySelector("#mod-search-error, #results-count, .empty, .message")?.textContent.trim());
      // A page control's own place is gone, so focus goes to the new page's heading.
      if (paged) document.getElementById("results-heading")?.focus();
      return;
    }
    const plan = document.getElementById("plan-panel");
    if (!plan || e.detail.target?.id !== "plan-panel") return;
    planDone();
    const list = document.getElementById("plan-list");
    if (list && listOpen && !list.hasAttribute("data-wide")) list.open = true;
    const said = plan.querySelector("[data-said]");
    if (!said) return;
    say(said.textContent.trim());
    document.getElementById(said.dataset.focus)?.focus();
    said.remove();
  });
  // Typing a search, or sending it, shows the Find a mod view, where the
  // results sit right under the box (DESIGN 8.11).
  const showFind = (e) => {
    if (e.target?.id !== "mod-search" && !e.target?.contains?.(document.getElementById("mod-search"))) return;
    const find = document.getElementById("mods-view-find");
    if (find && !find.checked) find.checked = true;
  };
  document.addEventListener("input", showFind);
  document.addEventListener("submit", showFind, true);
})();
