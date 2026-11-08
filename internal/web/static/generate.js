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

  function announce(said) {
    const announcer = document.getElementById("announcer");
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
    const pick = document.getElementById("preview-planet");
    if (pick && pick.value !== d.dataset.planet && [...pick.options].some((o) => o.value === d.dataset.planet)) {
      pick.value = d.dataset.planet;
      pick.dispatchEvent(new Event("change", { bubbles: true }));
      announce(d.dataset.said);
    }
  }, true);
  // A planet chosen in the preview opens its row, and focus stays where it is.
  document.addEventListener("change", (e) => {
    if (e.target.id === "preview-planet") openRow(e.target.value);
  });
  document.addEventListener("preview:planet", (e) => {
    if (e.detail && e.detail.planet) openRow(e.detail.planet);
  });

  // Sliders: the select is the field that is sent; the slider moves it.
  function syncSlider(select) {
    const range = document.getElementById(select.id + "-range");
    const value = document.getElementById(select.id + "-value");
    const text = select.selectedIndex >= 0 ? select.options[select.selectedIndex].text : "";
    if (range) {
      range.value = String(select.selectedIndex);
      range.setAttribute("aria-valuetext", text);
    }
    if (value) value.textContent = text;
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
      range.max = String(select.options.length - 1);
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
    const settings = document.getElementById("map-settings");
    if (!settings) return;
    const word = formEl().dataset.achWord || "";
    let modified = false;
    const active = new Set();
    settings.querySelectorAll("[data-preset]").forEach((el) => {
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
    if (reset) reset.hidden = !modified;
    const summary = document.getElementById("ach-summary");
    if (summary) {
      summary.querySelectorAll("li[data-reason]").forEach((li) => { li.hidden = !active.has(li.dataset.reason); });
      summary.hidden = active.size === 0;
    }
    // A resource the game needs warns while off; a warning, not a block (SPEC 5.6).
    const off = new Set();
    needsOff = off;
    settings.querySelectorAll("input[data-need]").forEach((box) => {
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
        select.selectedIndex = Number(t.value);
        syncSlider(select);
      }
    }
    if (t.closest("#map-settings")) changed(t);
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
    if (t.closest("#map-settings")) changed(t);
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
  // size and the territory box are views, not settings.
  const views = new Set(["tab", "preview_size", "territories"]);
  let unsaved = Boolean(document.getElementById("error-summary"));
  let submitting = false;
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
  window.addEventListener("pageshow", () => { submitting = false; });
  // A redraw (Preview map's seed; a version, Space Age, preset or reset) puts in
  // fields that load with the edited values, so what was unsaved stays so, even
  // if a setting is later changed back (SPEC 5.6).
  document.body.addEventListener("htmx:beforeSwap", (e) => {
    const elt = e.detail.requestConfig && e.detail.requestConfig.elt;
    if (elt && elt.closest && elt.closest("#generate-form") && isDirty()) unsaved = true;
  });

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
      if (sender && sender.id === "preset-reset") {
        // Reset is hidden once nothing differs, so focus goes to the preset it went back to.
        const preset = document.getElementById("generate-preset");
        if (preset) {
          preset.focus();
          announce(strings["js.generate.reset"].replace("{preset}", preset.selectedIndex >= 0 ? preset.options[preset.selectedIndex].text : ""));
        }
      } else if (!(sender && sender.id === "generate-preset")) announce(strings["js.generate.options"]);
    } else if (path.endsWith("/generate/preview")) {
      const failedMsg = document.querySelector("#preview .message");
      announce(failedMsg ? failedMsg.textContent.replace(/\s+/g, " ").trim() : strings["js.generate.preview_ready"]);
    }
  });
});
