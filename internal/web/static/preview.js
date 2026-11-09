// The map preview on Generate (SPEC 5.6; DESIGN 8.8). It draws on page load
// and about 0.8 s after the last change to the map's settings, for the planet
// whose settings are open, or for every planet once "All planets" or Preview
// map is chosen; the newest request wins and older ones are dropped.
// It never moves focus. The large view opens each picture in the page, with
// Previous, Next, zoom and panning; without script the picture link opens
// the file. Another planet viewed is told as a "preview:planet" event.
// Vulcanus's demolisher territories, when their box is ticked, are asked
// for after its picture arrives and drawn over it and in the large view.
// On a wide screen the large view holds the viewed planet's own Resources and
// Terrain fields beside the map, so a change there redraws it in place.
// While "Show resource amounts" is ticked, the large view asks for the amounts
// of the planet it shows once the picture is there, and labels the patches.
document.addEventListener("DOMContentLoaded", () => {
  const strings = JSON.parse(document.getElementById("i18n").textContent);
  const formEl = () => document.getElementById("generate-form");
  const button = () => document.getElementById("preview-button");
  if (!formEl() || !button()) return;
  const path = button().getAttribute("formaction");
  const pause = 800;

  let timer = 0;
  // A settings change waits out the pause before its draw; the picture shown meanwhile is of older settings.
  let waiting = false;
  let sent = 0; // the number of the newest request; a reply to an older one is ignored
  let pending = null; // the AbortController of the request on its way
  let pendingInView = false; // whether that request was asked for by the large view
  let drawing = ""; // the planet the request on its way is for, or "*" for every planet
  let refreshing = 0; // htmx redraws of the settings still on their way
  // The planet shown, blank for the first; the settings may say which planet is open.
  const open = document.getElementById("open-planet");
  let viewing = open && open.value ? open.value : "";
  // Whether every planet is shown, small, rather than the viewed one, large.
  let showAll = false;

  const dialog = document.getElementById("large-view");
  const status = document.getElementById("preview-status");

  // Only the newest words are said: a later announcement or status text
  // cancels one still waiting, so older news never overwrites newer.
  let saying = 0;
  function announce(said) {
    // Outside an open modal dialog the page is inert, so the dialog says it itself.
    const target = dialog && dialog.open ? document.getElementById("large-status") : document.getElementById("announcer");
    clearTimeout(saying);
    if (!target) return;
    target.textContent = "";
    saying = setTimeout(() => { target.textContent = said; }, 50);
  }
  function largeStatus(text) {
    clearTimeout(saying);
    document.getElementById("large-status").textContent = text;
  }

  function setStatus(on) {
    if (status) status.textContent = on ? status.dataset.text : "";
    const slot = document.getElementById("preview");
    if (slot) slot.setAttribute("aria-busy", on ? "true" : "false");
    // The large view fades its picture the same way while the next one is drawn.
    const big = document.getElementById("large-frame");
    if (big) big.setAttribute("aria-busy", on && dialog && dialog.open ? "true" : "false");
    const note = document.getElementById("large-drawing");
    if (note && !on) note.textContent = "";
  }

  function drop() {
    clearTimeout(timer);
    sent++;
    if (pending) pending.abort();
    pending = null;
    setStatus(false);
    dropTerritories();
    clearAmounts();
  }

  // Vulcanus's demolisher territories (SPEC 5.6; DESIGN 8.8) are asked for
  // once its picture has arrived, so the picture never waits. The newest
  // request wins; a new draw, a change of settings or another planet viewed
  // drops the one on its way.
  const terrPath = path + "/territories";
  let terrSent = 0;
  let terrPending = null;
  const terrPlanet = "vulcanus";
  const terrBox = () => document.getElementById("preview-territories");
  const terrSlot = () => document.getElementById("territory-result");

  function dropTerritories() {
    terrSent++;
    if (terrPending) terrPending.abort();
    terrPending = null;
  }

  // The downloads draw the territories in while they are shown.
  function territoryDownloads(on) {
    const link = pictureLink(terrPlanet);
    const one = link && link.closest("li").querySelector("a.download");
    const zip = document.querySelector('#preview a.download[href*="/all.zip"]');
    for (const a of [one, zip]) {
      if (!a) continue;
      const url = new URL(a.href);
      if (on) url.searchParams.set("territories", "1");
      else url.searchParams.delete("territories");
      a.setAttribute("href", url.pathname + url.search);
    }
    if (one) one.textContent = on ? one.dataset.territories : one.dataset.plain;
    if (dialog && dialog.open && (viewing || firstPlanet()) === terrPlanet) show(terrPlanet);
  }

  function clearTerritories() {
    dropTerritories();
    const link = pictureLink(terrPlanet);
    if (link) link.querySelectorAll(".territories").forEach((o) => o.remove());
    const slot = terrSlot();
    if (slot) slot.replaceChildren();
    territoryDownloads(false);
  }

  function showTerritoryResult(slot, from) {
    slot.replaceChildren(...[...from.childNodes].map((n) => document.importNode(n, true)));
  }

  async function territories(say) {
    dropTerritories();
    const box = terrBox();
    const link = pictureLink(terrPlanet);
    const slot = terrSlot();
    if (!box || !box.checked || !link || !slot) return;
    // The server already showed the ones it holds for these settings.
    if (link.querySelector(".territories")) return;
    const n = terrSent;
    const ctrl = new AbortController();
    terrPending = ctrl;
    const note = document.createElement("p");
    note.className = "hint";
    note.textContent = slot.dataset.finding;
    slot.replaceChildren(note);
    const body = new URLSearchParams(new FormData(formEl()));
    let html;
    try {
      const res = await fetch(terrPath, { method: "POST", body, signal: ctrl.signal, headers: { "HX-Request": "true" }, credentials: "same-origin" });
      if (!res.ok || res.redirected) throw new Error(String(res.status));
      html = await res.text();
    } catch {
      if (n !== terrSent || ctrl.signal.aborted) return;
      terrPending = null;
      const failed = document.getElementById("territory-failed");
      if (failed) showTerritoryResult(slot, failed.content);
      // The sentence only, not the Try again button's label.
      const why = slot.querySelector(".message p");
      if (say && why) announce(words(why));
      return;
    }
    if (n !== terrSent) return;
    terrPending = null;
    const reply = new DOMParser().parseFromString(html, "text/html").getElementById("territory-reply");
    const result = reply && reply.querySelector(".territory-result");
    if (!result) return;
    const overlay = reply.querySelector(".territories");
    if (overlay) link.querySelector(".map").insertBefore(document.importNode(overlay, true), link.querySelector(".pin"));
    showTerritoryResult(slot, result);
    territoryDownloads(Boolean(overlay));
    if (say) announce(words(slot));
  }

  // The large view shows the same overlay as the picture it opened. Its copy
  // uses the page's stripe pattern, so no id is there twice.
  function largeOverlay(link) {
    const map = document.querySelector("#large-view .map");
    if (!map) return;
    map.querySelectorAll(".territories").forEach((o) => o.remove());
    const overlay = link && link.querySelector(".territories");
    if (!overlay) return;
    const copy = overlay.cloneNode(true);
    copy.querySelectorAll("defs").forEach((d) => d.remove());
    map.insertBefore(copy, map.querySelector(".pin"));
    placeLabels();
  }

  document.addEventListener("click", (e) => {
    if (!e.target.closest("[data-territory-retry]")) return;
    // The button goes with the message, so focus goes back to the box.
    const box = terrBox();
    if (box) box.focus();
    territories(true);
  });

  // A pasted exchange string is needed before there is anything to draw.
  function nothingToDraw() {
    const form = formEl();
    const exchange = form.querySelector("#mode-exchange");
    const text = form.querySelector("#exchange-string");
    return exchange && exchange.checked && text && text.value.trim() === "";
  }

  // A draw after a settings change (change true) made in the large view
  // shows a drawing line there, as the page does, not read out, and says
  // when the new map is ready.
  async function draw(say, change) {
    if (refreshing > 0) return; // the redraw's end asks again, with the settings it brings
    drop();
    waiting = false;
    const n = sent;
    if (nothingToDraw()) {
      const slot = document.getElementById("preview");
      if (slot) slot.replaceChildren();
      return;
    }
    // Sent as htmx sends a form, URL-encoded.
    const body = new URLSearchParams(new FormData(formEl()));
    body.set("auto", "1");
    body.set("preview_view", showAll ? "all" : "one");
    if (viewing && !showAll) body.set("planet", viewing);
    const seed = formEl().querySelector("#generate-seed");
    const seedSent = seed ? seed.value : "";
    const ctrl = new AbortController();
    pending = ctrl;
    pendingInView = Boolean(dialog && dialog.open);
    drawing = showAll ? "*" : viewing || firstPlanet();
    setStatus(true);
    const note = el("large-drawing");
    if (change && pendingInView && status && note) note.textContent = status.dataset.text;
    let html;
    try {
      const res = await fetch(path, { method: "POST", body, signal: ctrl.signal, headers: { "HX-Request": "true" }, credentials: "same-origin" });
      if (!res.ok || res.redirected) throw new Error(String(res.status));
      html = await res.text();
    } catch {
      if (n !== sent || ctrl.signal.aborted) return;
      setStatus(false);
      pending = null;
      failed(say);
      return;
    }
    if (n !== sent) return;
    pending = null;
    setStatus(false);
    const fresh = new DOMParser().parseFromString(html, "text/html").getElementById("preview");
    if (!fresh) return;
    // A tick or untick made while the picture was drawn is newer than the reply.
    const before = terrBox();
    const ticked = before ? before.checked : null;
    swap(fresh);
    const after = terrBox();
    if (ticked !== null && after && after.checked !== ticked) {
      after.checked = ticked;
      if (!ticked) clearTerritories();
    }
    // The seed used is kept, so the world made is the one previewed (SPEC 5.6).
    // It is the page's own choice, not the person's, so leaving does not warn about it.
    if (seed && seed.value === seedSent && seedSent.trim() === "" && fresh.dataset.seed && formEl().querySelector("#mode-settings:checked")) {
      seed.value = fresh.dataset.seed;
      seed.defaultValue = fresh.dataset.seed;
    }
    const filled = formEl().querySelector("#filled-seed");
    if (filled && fresh.dataset.filledSeed) filled.value = fresh.dataset.filledSeed;
    markInvalid(fresh);
    territories(false);
    const error = fresh.querySelector(".message");
    // Problems are said, but focus stays where it is (DESIGN 8.8).
    if (fresh.dataset.preview === "invalid") {
      if (say && error) announce(words(error));
      return;
    }
    if (dialog && dialog.open) {
      // Only a reply with the viewed planet's picture is shown; anything else
      // is said and ends here, so a failing preview is never asked for again.
      const planet = viewing || firstPlanet();
      if (!error && pictureLink(planet)) {
        show(planet);
        announce(change ? strings["js.generate.preview_ready"] : el("large-title").textContent);
      } else if (error) announce(words(error));
      return;
    }
    if (say) announce(error ? words(error) : strings["js.generate.preview_ready"]);
  }

  const words = (node) => node.textContent.replace(/\s+/g, " ").trim();
  const pictureLink = (planet) => document.querySelector(`#preview a.picture[data-planet="${CSS.escape(planet)}"]`);

  // The fields an automatic preview found problems in are marked, until a
  // later reply has none; a mark the server put there is left alone.
  let marked = [];
  function markInvalid(fresh) {
    marked.forEach((f) => f.removeAttribute("aria-invalid"));
    marked = [];
    if (fresh.dataset.preview !== "invalid") return;
    fresh.querySelectorAll("a[href^='#']").forEach((a) => {
      const field = document.getElementById(a.getAttribute("href").slice(1));
      if (!field || field.getAttribute("aria-invalid") === "true") return;
      field.setAttribute("aria-invalid", "true");
      marked.push(field);
    });
  }

  // How to find the focused control again in the new section: by its id,
  // or, for a picture or download link, by the planet it belongs to.
  function focusKey(node) {
    if (node.id) return "#" + CSS.escape(node.id);
    if (!node.matches("a")) return "";
    const pic = node.closest("li") && node.closest("li").querySelector("a.picture");
    if (!pic) return node.matches("a.download") ? "#preview p a.download" : "";
    const planet = `a.picture[data-planet="${CSS.escape(pic.dataset.planet)}"]`;
    return node.matches("a.picture") ? "#preview " + planet : `#preview li:has(${planet}) a.download`;
  }

  // Puts the new section in place; a control that had focus there keeps it.
  function swap(fresh) {
    const slot = document.getElementById("preview");
    if (!slot) return;
    const key = slot.contains(document.activeElement) ? focusKey(document.activeElement) : "";
    slot.replaceWith(document.importNode(fresh, true));
    if (key) {
      const again = document.querySelector(key);
      if (again) again.focus({ preventScroll: true });
    }
  }

  // A failed request swaps nothing, so the page's own sentence is shown, and said.
  function failed(say) {
    const slot = document.getElementById("preview");
    const message = document.getElementById("preview-failed");
    if (!slot || !message) return;
    slot.replaceChildren(message.content.cloneNode(true));
    if (say) announce(words(slot.querySelector(".message")));
  }

  function soon() {
    drop();
    waiting = true;
    // A preview the person asked for is older than this change, so it goes too.
    if (button().getAttribute("aria-disabled") === "true") htmx.trigger(button(), "htmx:abort");
    timer = setTimeout(() => draw(true, true), pause);
  }

  // Which changes redraw: anything in the form that changes the map. The
  // name, the tabs, the download box and the preview's own controls do not;
  // version and Space Age redraw the settings first, and that redraw asks.
  // The settings shown in the large view count as where they came from.
  function counts(t) {
    if (!t.closest || !t.closest("#generate-form")) return false;
    if (t.closest("#preview, .version-fields, .tabset")) return false;
    if (t.closest("#large-view") && !t.closest("#large-settings")) return false;
    return !["generate-name", "generate-download"].includes(t.id);
  }
  document.addEventListener("input", (e) => { if (counts(e.target)) soon(); });
  document.addEventListener("change", (e) => {
    if (e.target.id === "preview-territories") {
      if (e.target.checked) territories(true);
      else clearTerritories();
      return;
    }
    if (e.target.name === "preview_view") {
      showAll = e.target.value === "all";
      draw(true);
      return;
    }
    if (counts(e.target)) soon();
  });

  // Opening a planet's settings shows that planet; with every planet shown,
  // it is the one "One planet" will show (DESIGN 8.8).
  document.addEventListener("settings:planet", (e) => {
    const planet = e.detail.planet;
    if (planet === (viewing || firstPlanet())) return;
    viewing = planet;
    if (showAll) return;
    e.preventDefault();
    draw(true);
  });

  // Redrawn settings may have other planets (Space Age, the game line), so the
  // planet viewed is the one they hold open, or the first when they lost it.
  function followSettings() {
    const held = document.getElementById("open-planet");
    const rows = [...document.querySelectorAll("#map-settings [data-planet]")].map((d) => d.dataset.planet);
    if (held && held.value) viewing = held.value;
    else if (rows.length && !rows.includes(viewing)) viewing = "";
    known = [];
  }

  const isRefresh = (e) => (e.detail.pathInfo ? e.detail.pathInfo.requestPath : "").endsWith("/generate/refresh");
  document.body.addEventListener("htmx:beforeRequest", (e) => {
    if (isRefresh(e)) {
      refreshing++;
      drop();
      // The redraw replaces the settings' rows, so the fields go home first. This
      // covers only a redraw started while the view is open; one already on its
      // way when the view opens keeps fillPanel waiting until it lands.
      putBack();
    }
    // Preview map asks for every planet; nothing automatic may overwrite it,
    // and later draws keep showing them all.
    if (e.detail.elt === button()) {
      drop();
      showAll = true;
    }
  });
  // Preview map's pictures are in place; the territories come after them.
  document.body.addEventListener("htmx:afterSwap", (e) => {
    if (e.target.id !== "preview") return;
    const all = el("preview-view-all");
    if (all) showAll = all.checked;
    territories(false);
  });
  document.body.addEventListener("htmx:afterRequest", (e) => {
    if (!isRefresh(e)) return;
    refreshing = Math.max(0, refreshing - 1);
    if (refreshing === 0) followSettings();
    if (refreshing === 0 && dialog && dialog.open) fillPanel(viewing || firstPlanet());
    // A redraw that ends while Preview map runs leaves that preview to finish.
    if (refreshing === 0 && button().getAttribute("aria-disabled") !== "true") soon();
  });

  // The large view: a picture viewer over a scrolling frame (DESIGN 8.8).
  // Zoom is a factor of the fitted size, from 1 up to the larger of 8 and
  // 8 CSS pixels per tile; scrolling keeps the pin inside the picture.
  const factor = 1.15;
  let zoom = 1;
  let tiles = 0; // the shown picture's width in tiles
  const lang = document.documentElement.lang || undefined;
  const el = (id) => document.getElementById(id);
  const frame = () => el("large-frame");

  // One planet's resource amounts in the large view (SPEC 5.6): asked for
  // while the box is ticked, once the planet's picture is shown there, so the
  // picture never waits. A draw, a change of settings, another planet,
  // closing the view or unticking drops the request on its way and the
  // labels at once; the server keeps what it counted, so going back is quick.
  const amountsPath = path + "/resources";
  let amountsSent = 0;
  let amountsPending = null;
  let amountsPlanet = ""; // the planet whose amounts are shown or on their way
  const largeMap = () => document.querySelector("#large-view .map");

  function counting(on) {
    const line = el("large-counting");
    if (line) line.hidden = !on;
  }

  function clearAmounts() {
    const slot = el("large-amounts-result");
    // The Try again button or the list's summary goes with the slot, so focus goes back to the box.
    if (slot && slot.contains(document.activeElement)) el("large-amounts").focus();
    // A status line still about this count says nothing now.
    const said = el("large-status");
    const bar = el("large-amounts-bar");
    const line = el("large-counting-text");
    if (said && bar && line && [bar.dataset.ready, words(line)].includes(said.textContent)) largeStatus("");
    clearTimeout(amountsSaying);
    amountsSent++;
    if (amountsPending) amountsPending.abort();
    amountsPending = null;
    amountsPlanet = "";
    counting(false);
    const map = largeMap();
    if (map) map.querySelectorAll(".resources").forEach((o) => o.remove());
    if (slot) slot.replaceChildren();
    const news = el("large-amounts-news");
    if (news) news.textContent = "";
    sizes = new Map();
  }

  async function amounts() {
    const box = el("large-amounts");
    const planet = viewing || firstPlanet();
    const slot = el("large-amounts-result");
    if (!box || !box.checked || waiting || !dialog || !dialog.open || !slot || !pictureLink(planet)) return;
    if (amountsPlanet === planet) return; // shown, failed, or on its way
    clearAmounts();
    amountsPlanet = planet;
    const n = amountsSent;
    const ctrl = new AbortController();
    amountsPending = ctrl;
    counting(true);
    // Said once as it starts (SPEC 17); the bar beside it is only seen.
    announce(words(el("large-counting-text")));
    const body = new URLSearchParams(new FormData(formEl()));
    body.set("planet", planet);
    let html;
    try {
      const res = await fetch(amountsPath, { method: "POST", body, signal: ctrl.signal, headers: { "HX-Request": "true" }, credentials: "same-origin" });
      if (!res.ok || res.redirected) throw new Error(String(res.status));
      html = await res.text();
    } catch {
      if (n !== amountsSent || ctrl.signal.aborted) return;
      amountsPending = null;
      counting(false);
      const failed = el("resource-failed");
      if (failed) slot.replaceChildren(failed.content.cloneNode(true));
      sayAmounts(slot);
      return;
    }
    if (n !== amountsSent) return;
    amountsPending = null;
    counting(false);
    const reply = new DOMParser().parseFromString(html, "text/html").getElementById("resource-reply");
    const result = reply && reply.querySelector(".resource-result");
    if (!result) return;
    slot.replaceChildren(...[...result.childNodes].map((node) => document.importNode(node, true)));
    const overlay = reply.querySelector(".resources");
    const map = largeMap();
    if (overlay && map) writeLabels(map.insertBefore(document.importNode(overlay, true), map.querySelector(".pin")));
    sayAmounts(slot);
  }

  // The status line says the amounts are ready. A failure is shown beside the
  // box, so it is said unseen: the sentence only, not Try again's label.
  let amountsSaying = 0;
  function sayAmounts(slot) {
    const why = slot.querySelector(".message p");
    const news = el("large-amounts-news");
    if (!why) {
      const bar = el("large-amounts-bar");
      if (bar) announce(bar.dataset.ready);
      return;
    }
    if (!news) return;
    news.textContent = "";
    clearTimeout(amountsSaying);
    amountsSaying = setTimeout(() => { news.textContent = words(why); }, 50);
  }

  // Each label is the amount, short in the page's language ("1.2M"), or a
  // well field's yield as a percentage; measured once, as its size is fixed.
  let sizes = new Map(); // each label of the shown amounts, in the server's order, and its size
  function writeLabels(overlay) {
    const amount = new Intl.NumberFormat(lang, { notation: "compact", maximumFractionDigits: 1 });
    const yieldOf = new Intl.NumberFormat(lang, { style: "percent", maximumFractionDigits: 0 });
    const labels = [...overlay.querySelectorAll(".resources__label")];
    labels.forEach((l) => { l.textContent = (l.dataset.kind === "yield" ? yieldOf : amount).format(Number(l.dataset.amount)); });
    sizes = new Map(labels.map((l) => [l, [l.offsetWidth, l.offsetHeight]]));
    placeLabels();
  }

  // Labels never overlap: in the server's order, largest patch first, each is
  // shown only where it meets no label already placed, no demolisher label
  // and not the pin, which are always kept. Zooming in makes room for more.
  // The boxes are kept in a grid of cells, so each label checks only its own.
  const cell = 64;
  const gap = 2;
  function placeLabels() {
    const map = largeMap();
    if (!map || !sizes.size || !dialog || !dialog.open) return;
    const w = map.clientWidth;
    const h = map.clientHeight;
    const origin = map.getBoundingClientRect();
    const grid = new Map();
    const cells = (b, each) => {
      for (let cx = Math.floor(b.x0 / cell); cx <= Math.floor(b.x1 / cell); cx++) {
        for (let cy = Math.floor(b.y0 / cell); cy <= Math.floor(b.y1 / cell); cy++) {
          if (each(cx + "," + cy)) return true;
        }
      }
      return false;
    };
    const meets = (b) => cells(b, (k) => (grid.get(k) || []).some((o) => b.x0 < o.x1 && o.x0 < b.x1 && b.y0 < o.y1 && o.y0 < b.y1));
    const take = (b) => cells(b, (k) => { if (!grid.has(k)) grid.set(k, []); grid.get(k).push(b); });
    map.querySelectorAll(".territories__label, .pin").forEach((kept) => {
      const r = kept.getBoundingClientRect();
      take({ x0: r.left - origin.left - gap, y0: r.top - origin.top - gap, x1: r.right - origin.left + gap, y1: r.bottom - origin.top + gap });
    });
    for (const [label, [lw, lh]] of sizes) {
      // Pulled in by as much of its size as its spot is across the map, as the style sheet does, so it stays inside.
      const u = Number(label.style.getPropertyValue("--x")) / 100;
      const v = Number(label.style.getPropertyValue("--y")) / 100;
      const x0 = Math.min(Math.max(0, u * (w - lw)), Math.max(0, w - lw));
      const y0 = Math.min(Math.max(0, v * (h - lh)), Math.max(0, h - lh));
      const b = { x0: x0 - gap, y0: y0 - gap, x1: x0 + lw + gap, y1: y0 + lh + gap };
      const free = !meets(b);
      label.hidden = !free;
      if (free) take(b);
    }
  }

  // After each zoom and resize, once a frame: the map's own size changes with both.
  let placing = 0;
  if (largeMap()) {
    new ResizeObserver(() => {
      if (placing) return;
      placing = requestAnimationFrame(() => { placing = 0; placeLabels(); });
    }).observe(largeMap());
  }

  // The list from the last section that had one: a section holding only an
  // error has none, and Previous and Next must still move then.
  let known = [];
  function planets() {
    const pick = el("preview-planets");
    const found = pick ? [...pick.options].map((o) => o.value) : [...document.querySelectorAll("#preview a.picture")].map((a) => a.dataset.planet);
    if (found.length) known = found;
    return known;
  }
  const firstPlanet = () => planets()[0] || "";

  function nameOf(planet) {
    const pick = el("preview-planets");
    const option = pick && [...pick.options].find((o) => o.value === planet);
    if (option) return option.text;
    const link = pictureLink(planet);
    const caption = link && link.closest("figure").querySelector("figcaption");
    return caption ? caption.textContent.trim() : planet;
  }

  // The planet's own Resources and Terrain fields, moved into the large view
  // while it is open and put back where they were when it closes, so there
  // is one copy of each and the form sends, checks and resets them as ever
  // (DESIGN 8.8). The style sheet shows them only where the view is wide
  // enough; elsewhere they wait unseen until the view closes.
  let moved = []; // each field group moved, and the mark where it goes back
  let panelPlanet = "";
  function putBack() {
    moved.forEach(({ node, mark }) => mark.replaceWith(node));
    moved = [];
    panelPlanet = "";
    const panel = el("large-settings");
    if (panel) panel.hidden = true;
  }

  // A tab's fields for one planet: its row's body, or on a world of one
  // planet, the tab's groups. A tab that holds one planet's groups without a
  // row on a world of several gives none, as they may be another planet's.
  function fieldsOf(tab, planet) {
    const fieldset = el("panel-" + tab);
    if (!fieldset) return [];
    const rows = [...fieldset.querySelectorAll(":scope > details[data-planet]")];
    if (!rows.length) return planets().length <= 1 ? [...fieldset.children].filter((n) => n.tagName !== "LEGEND") : [];
    const row = rows.find((d) => d.dataset.planet === planet);
    const body = row && row.querySelector(":scope > .mapgen-body");
    return body ? [body] : [];
  }

  function fillPanel(planet) {
    const panel = el("large-settings");
    // A settings redraw on its way would replace the rows moved; its end fills the view.
    if (!panel || !dialog.open || planet === panelPlanet || refreshing > 0) return;
    putBack();
    // An exchange string makes the map by itself; the settings are not used.
    if (!formEl().querySelector("#mode-settings:checked")) return;
    for (const tab of ["resources", "terrain"]) {
      const part = el("large-" + tab);
      const nodes = fieldsOf(tab, planet);
      part.hidden = nodes.length === 0;
      nodes.forEach((node) => {
        const mark = document.createComment("");
        node.before(mark);
        part.append(node);
        moved.push({ node, mark });
      });
    }
    panel.hidden = moved.length === 0;
    panelPlanet = planet;
  }

  // Another planet viewed is told to the page, so its settings row can follow.
  function view(planet) {
    const was = viewing || firstPlanet();
    viewing = planet;
    if (planet !== was) document.dispatchEvent(new CustomEvent("preview:planet", { detail: { planet } }));
  }

  // Measured from the picture itself, as the frame's own size is rounded.
  function maxZoom() {
    const fit = frame().querySelector(".map").getBoundingClientRect().width / zoom || 1;
    return Math.max(8, (8 * (tiles || fit)) / fit);
  }

  // Sets the zoom, keeping the spot of the picture under a point of the
  // window (the frame's centre when none is given) under that point.
  function zoomTo(next, x, y) {
    const f = frame();
    const map = f.querySelector(".map");
    if (x === undefined) {
      const box = f.getBoundingClientRect();
      x = box.left + f.clientWidth / 2;
      y = box.top + f.clientHeight / 2;
    }
    const before = map.getBoundingClientRect();
    const u = (x - before.left) / (before.width || 1);
    const v = (y - before.top) / (before.height || 1);
    const most = maxZoom();
    zoom = Math.min(most, Math.max(1, next));
    f.style.setProperty("--zoom", String(zoom));
    const after = map.getBoundingClientRect();
    f.scrollLeft += after.left + u * after.width - x;
    f.scrollTop += after.top + v * after.height - y;
    f.toggleAttribute("data-zoomed", zoom > 1);
    el("large-out").setAttribute("aria-disabled", String(zoom <= 1));
    el("large-in").setAttribute("aria-disabled", String(zoom >= most * 0.9999));
  }

  // A zoom from a key or a button is said as text; a wheel or a pinch is seen.
  function zoomSaid(next) {
    zoomTo(next);
    largeStatus(strings["js.preview.zoom"].replace("{level}", new Intl.NumberFormat(lang, { style: "percent", maximumFractionDigits: 0 }).format(zoom)));
  }

  // The arrows are named after the planets they lead to.
  function nameArrows() {
    const list = planets();
    const many = list.length > 1;
    el("large-prev").hidden = !many;
    el("large-next").hidden = !many;
    if (!many) return;
    const at = Math.max(0, list.indexOf(viewing || list[0]));
    const near = (by) => nameOf(list[(at + by + list.length) % list.length]);
    el("large-prev-name").textContent = strings["js.preview.previous"].replace("{planet}", near(-1));
    el("large-next-name").textContent = strings["js.preview.next"].replace("{planet}", near(1));
  }

  // Shows a planet's picture, or draws it first when it has not been drawn.
  function show(planet) {
    const link = pictureLink(planet);
    view(planet);
    nameArrows();
    fillPanel(planet);
    if (!link) {
      // The dialog names the planet at once and shows no other planet's picture meanwhile.
      const name = nameOf(planet);
      el("large-title").textContent = name;
      el("large-frame").dataset.missing = "true";
      el("large-download").hidden = true;
      largeOverlay(null);
      largeStatus(strings["js.preview.drawing"].replace("{planet}", name));
      draw(true);
      return false;
    }
    // A draw on its way for another planet is dropped, or its reply would
    // replace this picture and its link.
    if (pending && drawing !== "*" && drawing !== planet) drop();
    const img = link.querySelector("img");
    const figure = link.closest("figure");
    const download = link.closest("li").querySelector("a.download");
    const name = figure.querySelector("figcaption").textContent.trim();
    tiles = Number(img.getAttribute("width")) || img.naturalWidth;
    el("large-title").textContent = name;
    el("large-frame").removeAttribute("data-missing");
    el("large-img").src = img.src;
    el("large-img").alt = img.alt;
    el("large-download").href = download ? download.href : link.href;
    const drawnIn = download && new URL(download.href).searchParams.has("territories");
    el("large-download-name").textContent = strings[drawnIn ? "js.preview.download_territories" : "js.preview.download"].replace("{planet}", name);
    largeOverlay(link);
    el("large-download").hidden = false;
    // A bigger picture may allow more zoom, or a smaller one less.
    if (dialog.open) zoomTo(zoom);
    amounts();
    return true;
  }

  function step(by) {
    const list = planets();
    if (list.length < 2) return;
    const at = Math.max(0, list.indexOf(viewing || list[0]));
    if (show(list[(at + by + list.length) % list.length])) announce(el("large-title").textContent);
  }

  if (dialog) {
    document.addEventListener("click", (e) => {
      const link = e.target.closest("#preview a.picture");
      // A click with a modifier still opens the picture as the browser would.
      if (!link || e.button !== 0 || e.ctrlKey || e.metaKey || e.shiftKey || e.altKey) return;
      e.preventDefault();
      redrawn = false;
      show(link.dataset.planet);
      largeStatus("");
      dialog.showModal();
      // Before the zoom is set, as the settings narrow the picture's frame.
      fillPanel(link.dataset.planet);
      zoomTo(1);
      amounts();
    });
    el("large-prev").addEventListener("click", () => step(-1));
    el("large-next").addEventListener("click", () => step(1));
    el("large-in").addEventListener("click", () => zoomSaid(zoom * factor));
    el("large-out").addEventListener("click", () => zoomSaid(zoom / factor));
    el("large-close").addEventListener("click", () => dialog.close());
    // A settings field the view hides as it narrows gives the focus to the map,
    // whether the browser has already dropped it or not.
    new ResizeObserver(() => {
      const panel = el("large-settings");
      if (!dialog.open || !panel || panel.checkVisibility()) return;
      const at = document.activeElement;
      if (panel.contains(at) || at === document.body || at === dialog) frame().focus();
    }).observe(dialog);
    // A picture the server no longer has (kept 15 minutes) is drawn again,
    // once; the frame shows nothing meanwhile rather than a broken image.
    let redrawn = false;
    el("large-img").addEventListener("load", () => { redrawn = false; });
    el("large-img").addEventListener("error", () => {
      if (!dialog.open || el("large-img").getAttribute("src") === "data:,") return;
      el("large-frame").dataset.missing = "true";
      if (redrawn) {
        const message = document.getElementById("preview-failed");
        const text = message ? message.content.querySelector(".message").textContent : "";
        largeStatus(text.replace(/\s+/g, " ").trim());
        return;
      }
      redrawn = true;
      largeStatus(status ? status.dataset.text : "");
      draw(true);
    });

    // One wheel notch is one step; a touchpad's small moves are part steps.
    frame().addEventListener("wheel", (e) => {
      e.preventDefault();
      const steps = e.deltaMode === 0 ? Math.max(-1, Math.min(1, e.deltaY / 100)) : Math.sign(e.deltaY);
      zoomTo(zoom * Math.pow(factor, -steps), e.clientX, e.clientY);
    }, { passive: false });

    // One pointer drags the picture; two pinch, zooming around their middle.
    const pointers = new Map();
    // Where a lone pointer went down, so a tap can centre the spot (WCAG 2.5.7).
    let tap = null;
    let spread = 0;
    let middle = null;
    frame().addEventListener("pointerdown", (e) => {
      if (e.pointerType === "mouse" && e.button !== 0) return;
      frame().setPointerCapture(e.pointerId);
      pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
      tap = pointers.size === 1 ? { id: e.pointerId, x: e.clientX, y: e.clientY } : null;
      spread = 0;
      if (pointers.size !== 2) return;
      const [a, b] = [...pointers.values()];
      spread = Math.hypot(a.x - b.x, a.y - b.y);
      middle = { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
    });
    frame().addEventListener("pointermove", (e) => {
      const last = pointers.get(e.pointerId);
      if (!last) return;
      const f = frame();
      if (pointers.size === 1) {
        f.scrollLeft -= e.clientX - last.x;
        f.scrollTop -= e.clientY - last.y;
      }
      pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
      if (pointers.size !== 2) return;
      const [a, b] = [...pointers.values()];
      const apart = Math.hypot(a.x - b.x, a.y - b.y);
      const mid = { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
      if (spread) {
        f.scrollLeft -= mid.x - middle.x;
        f.scrollTop -= mid.y - middle.y;
        zoomTo((zoom * apart) / spread, mid.x, mid.y);
      }
      spread = apart;
      middle = mid;
    });
    const lift = (e) => {
      pointers.delete(e.pointerId);
      spread = 0;
      if (!tap || tap.id !== e.pointerId) return;
      const moved = Math.hypot(e.clientX - tap.x, e.clientY - tap.y);
      tap = null;
      if (e.type !== "pointerup" || moved >= 6 || zoom <= 1) return;
      const f = frame();
      const box = f.getBoundingClientRect();
      f.scrollLeft += e.clientX - (box.left + box.width / 2);
      f.scrollTop += e.clientY - (box.top + box.height / 2);
    };
    frame().addEventListener("pointerup", lift);
    frame().addEventListener("pointercancel", lift);
    frame().addEventListener("lostpointercapture", lift);

    // The arrows step between planets; a zoomed picture with focus scrolls
    // with them instead. The browser's own zoom keys are left alone, and a
    // settings field keeps every key of its own (Escape still closes).
    const arrows = { ArrowLeft: -1, ArrowRight: 1 };
    dialog.addEventListener("keydown", (e) => {
      if (e.ctrlKey || e.metaKey || e.altKey) return;
      if (e.target.closest && e.target.closest("#large-settings")) return;
      const panning = e.target === frame() && zoom > 1;
      if (arrows[e.key] && !panning) step(arrows[e.key]);
      else if (e.key === "+" || e.key === "=") zoomSaid(zoom * factor);
      else if (e.key === "-") zoomSaid(zoom / factor);
      else if (e.key === "0") zoomSaid(1);
      else return;
      e.preventDefault();
    });
    // Focus goes back to the picture shown, which is the one that opened it
    // or, after Next, its neighbour.
    // A planet still being drawn when the dialog closes is dropped: its reply
    // would swap the picture that has the focus away.
    el("large-amounts").addEventListener("change", (e) => {
      if (e.target.checked) amounts();
      else clearAmounts();
    });
    document.addEventListener("click", (e) => {
      if (!e.target.closest("[data-resource-retry]")) return;
      // The button goes with the message, so focus goes back to the box.
      el("large-amounts").focus();
      clearAmounts();
      amounts();
    });
    dialog.addEventListener("close", () => {
      clearAmounts();
      putBack();
      let link = pictureLink(viewing);
      if (!link) {
        // The close event comes a moment after the dialog shuts, so a planet
        // picked on the page meanwhile is already being drawn; it is kept.
        if (pending && !pendingInView) return;
        if (pending) drop();
        link = document.querySelector("#preview a.picture");
        if (link) show(link.dataset.planet);
      }
      if (link) link.focus();
    });
  }

  draw(false);
});
