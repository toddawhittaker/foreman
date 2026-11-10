// Keeps the Generate form in step with what the browser redraws (SPEC 5.6,
// 17.3; DESIGN 8.6 to 8.8). Choosing a version from the list clears the typed
// one, which would win. The map settings show one tab at a time, sliders on a
// wide panel, "[modified]" once a setting leaves the preset, and a warning as
// soon as a setting turns achievements off; the server gives the same answers
// when the form is sent. After the settings are redrawn for a new game line,
// or a preview is made, the change is read out. A form shown again with
// errors gets focus on its error summary; an old summary is removed when a
// redraw leaves the form in place. While a preview runs the buttons are
// marked aria-disabled rather than disabled, so the one the person pressed
// keeps focus. Leaving with unsaved settings asks first.
document.addEventListener("DOMContentLoaded", () => {
  const strings = JSON.parse(document.getElementById("i18n").textContent);
  // The whole form is swapped when it comes back with errors, so it is looked up each time.
  const formEl = () => document.getElementById("generate-form");
  if (!formEl()) return;
  formEl().classList.add("js-on");

  // The settings the large view holds are its own (DESIGN 8.8), and outside
  // an open modal dialog the page is inert, so the dialog says the news then.
  const settingsIn = "#map-settings, #large-settings";
  function announce(said) {
    const view = document.getElementById("large-view");
    const announcer = document.getElementById(view && view.open ? "large-status" : "announcer");
    if (!announcer) return;
    announcer.textContent = "";
    setTimeout(() => { announcer.textContent = said; }, 50);
  }

  // Tabs: the checked radio names the panel the style sheet shows.
  function showTab(name) {
    const radio = document.getElementById("tab-" + name);
    if (radio) radio.checked = true;
    formEl().dataset.tab = name;
  }

  // A link or focus that lands in another tab, or a closed planet, opens it
  // first. A row's own summary is left to its click, which would otherwise
  // toggle shut the row its focus just opened.
  function reveal(el) {
    const panel = el.closest(".tab-panel");
    if (panel) showTab(panel.id.replace("panel-", ""));
    for (let d = el.closest("details"); d; d = d.parentElement.closest("details")) {
      if (d.querySelector(":scope > summary") !== el) d.open = true;
    }
  }

  // Planet rows (DESIGN 8.7): each tab's rows are one exclusive group, so
  // the browser closes the other row. One planet is open in every tab, kept
  // in #open-planet for the server, and the preview shows it.
  const openPlanet = () => document.getElementById("open-planet");
  // Rows the script opened. Their toggle event comes a moment later, when a
  // tab without the next planet's row may still hold this one open; it must
  // not be taken for the person choosing it.
  const byScript = new WeakSet();
  function openRow(planet) {
    const field = openPlanet();
    if (field) field.value = planet;
    document.querySelectorAll("#generate-form details[data-planet]").forEach((d) => {
      if (d.dataset.planet !== planet) {
        if (!d.open) byScript.delete(d);
      } else if (!d.open) {
        byScript.add(d);
        d.open = true;
      }
    });
  }
  document.addEventListener("toggle", (e) => {
    const d = e.target;
    if (!(d instanceof HTMLDetailsElement) || !d.dataset.planet || !d.closest("#generate-form")) return;
    if (byScript.delete(d) || !d.open) return;
    const field = openPlanet();
    // A row opened to follow the choice already made says and sends nothing.
    if (!field || field.value === d.dataset.planet) return;
    openRow(d.dataset.planet);
    // The preview shows the planet whose settings are open; it cancels the event when it switches, which is said.
    const told = new CustomEvent("settings:planet", { detail: { planet: d.dataset.planet }, cancelable: true });
    if (!document.dispatchEvent(told)) announce(d.dataset.said);
  }, true);
  // A planet shown in the large view opens its row, and focus stays where it is.
  document.addEventListener("preview:planet", (e) => {
    if (e.detail && e.detail.planet) openRow(e.detail.planet);
  });

  // Sliders: the select is the field that is sent; the slider moves it.
  // The default step ("100", or "0" for bias) sits at the track's middle: the
  // steps below it and above it each take half of the track, whatever their
  // number. With no default, or one at an end, the track is the plain index.
  const gcd = (a, b) => (b ? gcd(b, a % b) : a);
  function track(select) {
    const last = select.options.length - 1;
    const values = [...select.options].map((o) => o.value);
    let d = values.indexOf("100");
    if (d < 0) d = values.indexOf("0");
    if (d <= 0 || d >= last) return { last, max: last, at: (i) => i };
    const below = d;
    const above = last - d;
    const half = (below * above) / gcd(below, above);
    return {
      last,
      max: 2 * half,
      at: (i) => (i <= d ? i * (half / below) : half + (i - d) * (half / above)),
    };
  }

  // The step whose place on the track is nearest to a value.
  function nearestStep(t, value) {
    let best = 0;
    for (let i = 1; i <= t.last; i++) {
      if (Math.abs(t.at(i) - value) < Math.abs(t.at(best) - value)) best = i;
    }
    return best;
  }

  function syncSlider(select) {
    const range = document.getElementById(select.id + "-range");
    const value = document.getElementById(select.id + "-value");
    const text = select.selectedIndex >= 0 ? select.options[select.selectedIndex].text : "";
    if (range) {
      range.value = String(track(select).at(Math.max(select.selectedIndex, 0)));
      range.setAttribute("aria-valuetext", text);
    }
    if (value) value.textContent = text;
  }

  // The step a slider's new value stands for. Under a pointer, the nearest.
  // From the keys or a screen reader's increment, which move one unit of the
  // track, the next step the way it went, so a small step never sticks.
  let pointerOnRange = false;
  document.addEventListener("pointerdown", (e) => {
    pointerOnRange = e.button === 0 && e.target.classList.contains("step__range");
  }, true);
  // A release can be lost (a menu, a drag out of the window), so a key press also ends the pointer's turn.
  for (const type of ["pointerup", "pointercancel", "keydown"]) {
    window.addEventListener(type, () => { pointerOnRange = false; }, true);
  }
  function stepOfSlider(range, select) {
    const t = track(select);
    const value = Number(range.value);
    const now = Math.max(select.selectedIndex, 0);
    const near = nearestStep(t, value);
    if (pointerOnRange || near !== now) return near;
    const way = Math.sign(value - t.at(now));
    return Math.min(Math.max(now + way, 0), t.last);
  }

  // Each select of steps gets a slider over the same steps and an output
  // with the step's words; the style sheet shows them on a wide panel.
  function addSliders(root) {
    root.querySelectorAll(".step select").forEach((select) => {
      if (document.getElementById(select.id + "-range")) return;
      const label = document.getElementById(select.id + "-label");
      const value = document.createElement("output");
      value.className = "step__value";
      // An output is a live region; the slider's aria-valuetext already speaks the step.
      value.setAttribute("aria-hidden", "true");
      value.id = select.id + "-value";
      value.htmlFor = select.id + "-range";
      if (label) label.after(value);
      const range = document.createElement("input");
      range.type = "range";
      range.className = "step__range";
      range.id = select.id + "-range";
      range.min = "0";
      range.max = String(track(select).max);
      range.step = "1";
      range.dataset.for = select.id;
      range.setAttribute("aria-labelledby", select.id + "-label");
      range.setAttribute("aria-describedby", select.getAttribute("aria-describedby") || "");
      select.after(range);
      syncSlider(select);
    });
  }
  addSliders(document);

  // A box with data-disables turns the element it names on and off, so a
  // resource that is off sends none of its settings (SPEC 5.6).
  function applyDisables() {
    document.querySelectorAll("#generate-form input[type=checkbox][data-disables]").forEach((box) => {
      const target = document.getElementById(box.dataset.disables);
      if (target) target.disabled = !box.checked;
    });
  }
  applyDisables();
  document.addEventListener("change", (e) => {
    if (e.target.matches && e.target.matches("#generate-form input[type=checkbox][data-disables]")) applyDisables();
  });
  document.body.addEventListener("htmx:afterSwap", applyDisables);

  // The achievement line of one field, in the units it shows.
  function crosses(el) {
    const test = el.dataset.ach;
    if (!test) return false;
    if (el.type === "checkbox") return test === "true" ? el.checked : !el.checked;
    const v = parseFloat(el.value);
    const limit = parseFloat(el.dataset.achLimit);
    if (Number.isNaN(v) || Number.isNaN(limit)) return false;
    const tol = 1e-6 * Math.abs(limit);
    return test === "below" ? v < limit - tol : v > limit + tol;
  }

  function differs(el) {
    if (el.type === "checkbox") return el.checked !== (el.dataset.preset === "on");
    if (el.tagName === "SELECT") return el.value !== el.dataset.preset;
    const a = parseFloat(el.value);
    const b = parseFloat(el.dataset.preset);
    if (Number.isNaN(a) || Number.isNaN(b)) return el.value.trim() !== el.dataset.preset.trim();
    return Math.abs(a - b) > 1e-9 * Math.max(1, Math.abs(b));
  }

  // The needed resources that are off, as last drawn.
  let needsOff = new Set();
  // Redraws "[modified]", the reset button, each warning and the summary.
  function refresh() {
    if (!document.getElementById("map-settings")) return;
    const word = formEl().dataset.achWord || "";
    let modified = false;
    const active = new Set();
    document.querySelectorAll(`:is(${settingsIn}) [data-preset]`).forEach((el) => {
      if (differs(el)) modified = true;
      if (!el.dataset.ach) return;
      const blocked = crosses(el);
      if (blocked) active.add(el.dataset.achReason);
      const note = document.getElementById(el.id + "-ach");
      if (note && note.textContent !== (blocked ? word : "")) note.textContent = blocked ? word : "";
    });
    const raw = document.getElementById("mg-raw");
    if (raw && (raw.dataset.modified === "true" || raw.value !== raw.defaultValue)) modified = true;
    const mark = document.getElementById("preset-modified");
    if (mark) mark.textContent = modified ? mark.dataset.word : "";
    const reset = document.getElementById("preset-reset");
    // Reset to defaults has something to do while a setting differs or another preset is chosen; "default" is the game's own preset name.
    const preset = document.getElementById("generate-preset");
    if (reset) reset.hidden = !modified && (!preset || preset.value === "default");
    const summary = document.getElementById("ach-summary");
    if (summary) {
      summary.querySelectorAll("li[data-reason]").forEach((li) => { li.hidden = !active.has(li.dataset.reason); });
      summary.hidden = active.size === 0;
    }
    // A resource the game needs warns while off; a warning, not a block (SPEC 5.6).
    const off = new Set();
    needsOff = off;
    document.querySelectorAll(`:is(${settingsIn}) input[data-need]`).forEach((box) => {
      if (!box.checked) off.add(box.dataset.needKey);
      const note = document.getElementById(box.id + "-need");
      const text = box.checked ? "" : box.dataset.need;
      if (note && note.textContent !== text) note.textContent = text;
    });
    const needs = document.getElementById("need-summary");
    if (needs) {
      needs.querySelectorAll("li[data-need-key]").forEach((li) => { li.hidden = !off.has(li.dataset.needKey); });
      needs.hidden = off.size === 0;
    }
    return active.size;
  }

  // The words a field is known by: its label, or for a box the label around it.
  function labelOf(el) {
    const label = document.getElementById(el.id + "-label") || el.closest("label") || document.querySelector(`label[for="${CSS.escape(el.id)}"]`);
    return label ? label.textContent.replace(/\s+/g, " ").trim() : "";
  }

  let blockedBefore = refresh();
  // What the achievement announcements said in this task (a number box's input
  // and change come together), so a live-fit announcement can say it too.
  let saidNow = "";
  function changed(t) {
    const offBefore = needsOff;
    const blocked = refresh();
    // A setting that newly turns achievements off is said once, not on every
    // keystroke, by name; so is the last warning going away.
    let said = "";
    if (blocked > blockedBefore) {
      const field = t && t.dataset && t.dataset.ach ? t : t && document.getElementById(t.dataset.for || "");
      const name = field ? labelOf(field) : "";
      said = name ? strings["js.generate.ach_setting"].replace("{setting}", name) : formEl().dataset.achWord || "";
    } else if (blocked === 0 && blockedBefore > 0) {
      said = strings["js.generate.ach_clear"];
    }
    // A needed resource's box is said once each way.
    const key = t && t.dataset ? t.dataset.needKey : "";
    if (key && offBefore.has(key) !== needsOff.has(key)) {
      const need = t.checked ? t.dataset.needClear : t.dataset.need;
      said = said ? said + " " + need : need;
    }
    if (said) {
      announce(said);
      saidNow = saidNow ? saidNow + " " + said : said;
      setTimeout(() => { saidNow = ""; }, 0);
    }
    blockedBefore = blocked;
  }

  document.addEventListener("input", (e) => {
    const t = e.target;
    if (!t.closest("#generate-form")) return;
    if (t.classList.contains("step__range")) {
      const select = document.getElementById(t.dataset.for);
      if (select) {
        select.selectedIndex = stepOfSlider(t, select);
        syncSlider(select);
      }
    }
    if (t.closest(settingsIn)) changed(t);
  });

  document.addEventListener("change", (e) => {
    const t = e.target;
    if (!t.closest("#generate-form")) return;
    if (t.name === "tab") {
      showTab(t.value);
      return;
    }
    if (t.tagName === "SELECT" && t.closest(".step")) syncSlider(t);
    if (t.id === "generate-version-select") {
      const typed = document.getElementById("generate-version");
      if (typed) typed.value = "";
    }
    if (t.closest(settingsIn)) changed(t);
  });

  // A minimum and its maximum keep at least their gap apart, as the game's
  // own form does: the field not changed moves, within its own limits, and
  // what moved is said once. The maximum names its minimum in data-min-field.
  function keepRange(t) {
    const max = t.dataset.minField ? t : document.querySelector(`#generate-form [data-min-field="${CSS.escape(t.id)}"]`);
    const min = max && document.getElementById(max.dataset.minField);
    if (!max || !min) return;
    const gap = Number(max.dataset.gap || 0);
    const lo = parseFloat(min.value);
    const hi = parseFloat(max.value);
    if (Number.isNaN(lo) || Number.isNaN(hi) || hi >= lo + gap) return;
    const round = (n) => String(Number(n.toFixed(6)));
    const moved = [];
    const set = (el, n) => {
      if (el.value === round(n)) return;
      el.value = round(n);
      moved.push(el);
    };
    if (t === min) {
      const top = parseFloat(max.dataset.max);
      if (!Number.isNaN(top) && lo + gap > top) {
        set(min, top - gap);
        set(max, top);
      } else set(max, lo + gap);
    } else {
      const bottom = parseFloat(min.dataset.min);
      if (!Number.isNaN(bottom) && hi - gap < bottom) {
        set(min, bottom);
        set(max, bottom + gap);
      } else set(min, hi - gap);
    }
    moved.forEach((el) => {
      el.dispatchEvent(new Event("input", { bubbles: true }));
      el.dispatchEvent(new Event("change", { bubbles: true }));
    });
    if (!moved.length) return;
    const movedSaid = moved.map((el) => strings["js.generate.moved"].replace("{field}", labelOf(el)).replace("{value}", el.value)).join(" ");
    announce(saidNow ? saidNow + " " + movedSaid : movedSaid);
  }
  document.addEventListener("change", (e) => {
    const t = e.target;
    if (t.matches && t.matches("#generate-form input") && t.id) keepRange(t);
  });

  // Random seed fills a new one as the game's own button does, 0 to 4294967295 (SPEC 5.6).
  document.addEventListener("click", (e) => {
    if (!e.target.closest("#seed-random")) return;
    const seed = document.getElementById("generate-seed");
    if (!seed) return;
    seed.value = String(crypto.getRandomValues(new Uint32Array(1))[0]);
    seed.dispatchEvent(new Event("input", { bubbles: true }));
    seed.dispatchEvent(new Event("change", { bubbles: true }));
  });

  // The error summary's links reach fields in a tab not shown.
  document.addEventListener("click", (e) => {
    const link = e.target.closest("#error-summary a[href^='#'], #preview a[href^='#']");
    if (!link) return;
    const target = document.getElementById(link.getAttribute("href").slice(1));
    if (target) reveal(target);
  });
  document.addEventListener("focusin", (e) => {
    if (e.target.closest("#generate-form .tab-panel")) reveal(e.target);
  });

  // Leaving with settings that are not saved asks the browser's own question
  // (SPEC 5.6). Not saved: a field that differs from what the page loaded
  // with, or a form the server sent back with errors. The tab, the preview
  // size, the territory box and the one-or-all choice are views, not settings.
  const views = new Set(["tab", "preview_size", "territories", "preview_view"]);
  let unsaved = Boolean(document.getElementById("error-summary"));
  let submitting = false;
  let leaving = false;
  function differsFromLoaded(el) {
    if (!el.name || views.has(el.name)) return false;
    if (el.type === "checkbox" || el.type === "radio") return el.checked !== el.defaultChecked;
    if (el.tagName === "SELECT") {
      const loaded = [...el.options].filter((o) => o.defaultSelected).pop() || el.options[0];
      return Boolean(loaded) && el.value !== loaded.value;
    }
    if (el.tagName !== "TEXTAREA" && el.tagName !== "INPUT") return false;
    return el.value !== el.defaultValue;
  }
  const isDirty = () => unsaved || [...formEl().elements].some(differsFromLoaded);
  window.addEventListener("beforeunload", (e) => {
    // Leave skips the question once; if the navigation is then cancelled, the next leave asks again.
    if (leaving) {
      leaving = false;
      return;
    }
    if (submitting || !isDirty()) return;
    e.preventDefault();
    // Older browsers ask only when this is set.
    e.returnValue = "";
  });
  // Generate itself leaves the page; Preview map and Reset are htmx requests and never submit.
  document.addEventListener("submit", (e) => {
    if (e.target === formEl() && !e.defaultPrevented) submitting = true;
  });
  // A page restored from the back-forward cache did not leave after all.
  window.addEventListener("pageshow", () => {
    submitting = false;
    leaving = false;
  });

  // Leaving by a link or a form of the page (the menu, the language, sign out,
  // Cancel) asks in Foreman's own dialog, which says more than the browser's
  // question. Reload, closing the tab, the address bar and Back cannot be
  // caught and keep the browser's question above.
  const leaveDialog = document.getElementById("leave-dialog");
  let leaveOpener = null;
  let leaveGo = null;
  function askToLeave(opener, go) {
    leaveOpener = opener;
    leaveGo = go;
    leaveDialog.showModal();
  }
  if (leaveDialog) {
    // A plain click on a link to another page. A modified click, a download,
    // a link with a target, a same-page "#" link and the pictures that open
    // the large view in place are not leaving.
    document.addEventListener("click", (e) => {
      const link = e.target.closest && e.target.closest("a[href]");
      if (!link || e.button !== 0 || e.ctrlKey || e.metaKey || e.shiftKey || e.altKey) return;
      if (link.hasAttribute("download") || link.hasAttribute("target") || link.classList.contains("picture")) return;
      if (link.protocol !== "http:" && link.protocol !== "https:") return;
      if (link.hash && link.href.split("#")[0] === location.href.split("#")[0]) return;
      if (leaving || submitting || !isDirty()) return;
      e.preventDefault();
      askToLeave(link, () => location.assign(link.href));
    }, true);
    // The header's language and sign-out forms; the Generate form itself is not leaving.
    document.addEventListener("submit", (e) => {
      const form = e.target;
      if (form === formEl() || leaving || submitting || !isDirty()) return;
      e.preventDefault();
      const submitter = e.submitter;
      askToLeave(submitter || document.activeElement, () => form.requestSubmit(submitter));
    }, true);
    document.getElementById("leave-go").addEventListener("click", () => {
      leaving = true;
      leaveDialog.close();
      leaveGo();
    });
    document.getElementById("leave-stay").addEventListener("click", () => leaveDialog.close());
    // Stay and Escape both close it; focus goes back to what was used.
    // A button in the user menu is hidden once the menu has closed, so the menu's own button gets it.
    leaveDialog.addEventListener("close", () => {
      if (leaving || !leaveOpener) return;
      const closed = leaveOpener.matches("summary") ? null : leaveOpener.closest("details:not([open])");
      const to = closed ? closed.querySelector(":scope > summary") : leaveOpener;
      if (to) to.focus();
    });
  }
  // A redraw (Preview map's seed; a version, Space Age, preset or reset) puts in
  // fields that load with the edited values, so what was unsaved stays so, even
  // if a setting is later changed back (SPEC 5.6).
  document.body.addEventListener("htmx:beforeSwap", (e) => {
    const elt = e.detail.requestConfig && e.detail.requestConfig.elt;
    if (elt && elt.closest && elt.closest("#generate-form") && isDirty()) unsaved = true;
  });

  // Reset to defaults throws every change away, so it is undone in one click
  // (SPEC 5.6, 17.9): the form as it was is kept and Undo sends it back for
  // the server to draw. Any later change to the settings ends the Undo, which
  // would otherwise throw that change away.
  let undoValues = null;
  function hideUndo() {
    undoValues = null;
    const undo = document.getElementById("preset-undo");
    if (!undo) return;
    const had = document.activeElement === undo;
    undo.hidden = true;
    // Focus never falls to the page: Reset takes it back, or the preset while there is nothing to reset.
    const reset = document.getElementById("preset-reset");
    const next = reset && !reset.hidden ? reset : document.getElementById("generate-preset");
    if (had && next) next.focus();
  }
  // The reset's reply replaces the fields anyway, so a change made while it is
  // on its way leaves the Undo to bring back the form as it was when Reset was pressed.
  let resetting = false;
  document.body.addEventListener("htmx:beforeRequest", (e) => {
    if (!e.detail.elt || e.detail.elt.id !== "preset-reset") return;
    undoValues = new FormData(formEl());
    resetting = true;
  });
  document.body.addEventListener("htmx:afterRequest", (e) => {
    if (e.detail.elt && e.detail.elt.id === "preset-reset") resetting = false;
  });
  document.body.addEventListener("htmx:configRequest", (e) => {
    if (!e.detail.elt || e.detail.elt.id !== "preset-undo" || !undoValues) return;
    const sent = e.detail.formData;
    [...new Set(sent.keys())].forEach((k) => sent.delete(k));
    undoValues.forEach((v, k) => sent.append(k, v));
  });
  // A view (the tab shown) changes no setting, so it keeps the Undo; a version
  // or Space Age change does end it, as Undo would mix two game lines.
  ["input", "change"].forEach((type) => document.addEventListener(type, (e) => {
    if (!undoValues || resetting || !e.target.closest || views.has(e.target.name)) return;
    if (e.target.closest("#line-box, #large-settings") || (type === "change" && e.target.closest(".version-fields"))) hideUndo();
  }));

  const actions = "#preview-button, #generate-button, #generate-default";
  const isPreview = (e) => e.detail.elt && e.detail.elt.id === "preview-button";

  function setBusy(on) {
    document.querySelectorAll(actions).forEach((b) => {
      if (on) b.setAttribute("aria-disabled", "true");
      else b.removeAttribute("aria-disabled");
    });
  }

  // A busy button does nothing, and also stops the Enter key's implicit submit.
  document.addEventListener("click", (e) => {
    const button = e.target.closest(actions);
    if (button && button.getAttribute("aria-disabled") === "true") {
      e.preventDefault();
      e.stopImmediatePropagation();
    }
  }, true);

  // The wait box appearing is not read out, so the start is said too.
  document.body.addEventListener("htmx:beforeRequest", (e) => {
    if (!isPreview(e)) return;
    setBusy(true);
    const wait = document.querySelector("#preview-wait p");
    if (wait) announce(wait.textContent.trim());
  });
  document.body.addEventListener("htmx:afterRequest", (e) => { if (isPreview(e)) setBusy(false); });

  // htmx swaps nothing on a 4xx, a 5xx or a lost connection, so say so.
  function failed(e) {
    if (!isPreview(e)) return;
    const slot = document.getElementById("preview");
    const message = document.getElementById("preview-failed");
    if (!slot || !message) return;
    slot.replaceChildren(message.content.cloneNode(true));
    announce(slot.querySelector(".message").textContent.replace(/\s+/g, " ").trim());
  }
  document.body.addEventListener("htmx:responseError", failed);
  document.body.addEventListener("htmx:sendError", failed);

  // A field typed in while a redraw was on its way keeps what was typed.
  const textFields = () => [...document.querySelectorAll("#line-box input[type=text][name], #line-box textarea[name]")];
  const isRefresh = (e) => (e.detail.pathInfo ? e.detail.pathInfo.requestPath : "").endsWith("/generate/refresh");
  let sentValues = new Map();
  let typedValues = new Map();
  document.body.addEventListener("htmx:beforeRequest", (e) => {
    if (isRefresh(e)) sentValues = new Map(textFields().map((f) => [f.name, f.value]));
  });
  document.body.addEventListener("htmx:beforeSwap", (e) => {
    if (!isRefresh(e)) return;
    typedValues = new Map(textFields().filter((f) => sentValues.has(f.name) && sentValues.get(f.name) !== f.value).map((f) => [f.name, f.value]));
  });

  document.body.addEventListener("htmx:afterSwap", (e) => {
    if (isRefresh(e) && typedValues.size) {
      textFields().forEach((f) => { if (typedValues.has(f.name)) f.value = typedValues.get(f.name); });
      typedValues = new Map();
    }
    const path = e.detail.pathInfo ? e.detail.pathInfo.requestPath : "";
    // The version list's own load is also a swap; it must not touch the summary.
    if (!path.endsWith("/generate/refresh") && !path.endsWith("/generate/preview")) return;
    const xhr = e.detail.xhr;
    const summary = document.getElementById("error-summary");
    // The form came back whole, with its errors: they are what to read next.
    if (xhr && xhr.getResponseHeader("HX-Retarget") === "#generate-form") {
      unsaved = true;
      const again = document.getElementById("generate-form");
      if (again) {
        again.classList.add("js-on");
        showTab(again.dataset.tab || "resources");
        addSliders(again);
        if (openPlanet()) openRow(openPlanet().value);
      }
      if (summary) summary.focus();
      return;
    }
    // Otherwise only a part was redrawn, so a summary left in the form is out of date.
    if (summary) summary.remove();
    if (path.endsWith("/generate/refresh")) {
      // The browser shuts a row inserted while the old one of its group was
      // still in the page, so the open planet is opened again.
      if (openPlanet()) openRow(openPlanet().value);
      // The redrawn settings keep the tab that was shown.
      const tab = formEl().querySelector("input[name='tab']:checked");
      if (tab) formEl().dataset.tab = tab.value;
      addSliders(document);
      blockedBefore = refresh();
      // Choosing a preset redraws what the person is looking at; nothing more is read out.
      const sender = e.detail.requestConfig && e.detail.requestConfig.elt;
      const undo = document.getElementById("preset-undo");
      // Each out-of-band part is a swap of its own; the settings' swap speaks for them all.
      if (e.target.id !== "map-settings" && e.target.id !== "line-box") return;
      if (sender && sender.id === "preset-reset") {
        // Reset is hidden once nothing differs; Undo takes its place, with the
        // focus Reset had. Focus the person moved on, as into a field, stays there.
        if (undo && undoValues) {
          const at = document.activeElement;
          undo.hidden = false;
          if (at === document.getElementById("preset-reset") || at === document.body) undo.focus();
        }
        announce(strings["js.generate.reset"]);
      } else if (sender && sender.id === "preset-undo") {
        hideUndo();
        const reset = document.getElementById("preset-reset");
        if (reset && !reset.hidden) reset.focus();
        announce(strings["js.generate.undone"]);
      } else if (!(sender && sender.id === "generate-preset")) announce(strings["js.generate.options"]);
    } else if (path.endsWith("/generate/preview")) {
      const failedMsg = document.querySelector("#preview .message");
      announce(failedMsg ? failedMsg.textContent.replace(/\s+/g, " ").trim() : strings["js.generate.preview_ready"]);
    }
  });
});
