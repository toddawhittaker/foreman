// The map preview on Generate (SPEC 5.6; DESIGN 8.8). It draws on page load
// and about 0.8 s after the last change to the map's settings, only for the
// planet being viewed; the newest request wins and older ones are dropped.
// It never moves focus. The large view opens each picture in the page, with
// Previous, Next, zoom and panning; without script the picture link opens
// the file. Another planet viewed is told as a "preview:planet" event.
// Vulcanus's demolisher territories, when their box is ticked, are asked
// for after its picture arrives and drawn over it and in the large view.
document.addEventListener("DOMContentLoaded", () => {
  const strings = JSON.parse(document.getElementById("i18n").textContent);
  const formEl = () => document.getElementById("generate-form");
  const button = () => document.getElementById("preview-button");
  if (!formEl() || !button()) return;
  const path = button().getAttribute("formaction");
  const pause = 800;

  let timer = 0;
  let sent = 0; // the number of the newest request; a reply to an older one is ignored
  let pending = null; // the AbortController of the request on its way
  let pendingInView = false; // whether that request was asked for by the large view
  let drawing = ""; // the planet the request on its way is for
  let refreshing = 0; // htmx redraws of the settings still on their way
  // The planet shown, blank for the first; the settings may say which planet is open.
  const open = document.getElementById("open-planet");
  let viewing = open && open.value ? open.value : "";

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
  }

  function drop() {
    clearTimeout(timer);
    sent++;
    if (pending) pending.abort();
    pending = null;
    setStatus(false);
    dropTerritories();
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
    if (!box || !box.checked || box.disabled || !link || !slot) return;
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
      if (say) announce(words(slot));
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

  async function draw(say) {
    if (refreshing > 0) return; // the redraw's end asks again, with the settings it brings
    drop();
    const n = sent;
    if (nothingToDraw()) {
      const slot = document.getElementById("preview");
      if (slot) slot.replaceChildren();
      return;
    }
    // Sent as htmx sends a form, URL-encoded.
    const body = new URLSearchParams(new FormData(formEl()));
    body.set("auto", "1");
    if (viewing) body.set("planet", viewing);
    const seed = formEl().querySelector("#generate-seed");
    const seedSent = seed ? seed.value : "";
    const ctrl = new AbortController();
    pending = ctrl;
    pendingInView = Boolean(dialog && dialog.open);
    drawing = viewing || firstPlanet();
    setStatus(true);
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
    const ticked = before && !before.disabled ? before.checked : null;
    swap(fresh);
    const after = terrBox();
    if (ticked !== null && after && !after.disabled && after.checked !== ticked) {
      after.checked = ticked;
      if (!ticked) clearTerritories();
    }
    // The seed used is kept, so the world made is the one previewed (SPEC 5.6).
    if (seed && seed.value === seedSent && seedSent.trim() === "" && fresh.dataset.seed && formEl().querySelector("#mode-settings:checked")) seed.value = fresh.dataset.seed;
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
        announce(el("large-title").textContent);
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
    // A preview the person asked for is older than this change, so it goes too.
    if (button().getAttribute("aria-disabled") === "true") htmx.trigger(button(), "htmx:abort");
    timer = setTimeout(() => draw(true), pause);
  }

  // Which changes redraw: anything in the form that changes the map. The
  // name, the tabs, the download box and the preview's own controls do not;
  // version and Space Age redraw the settings first, and that redraw asks.
  function counts(t) {
    if (!t.closest || !t.closest("#generate-form")) return false;
    if (t.closest("#preview, #large-view, .version-fields, .tabset")) return false;
    return !["generate-name", "generate-download"].includes(t.id);
  }
  document.addEventListener("input", (e) => { if (counts(e.target)) soon(); });
  document.addEventListener("change", (e) => {
    if (e.target.id === "preview-territories") {
      if (e.target.checked) territories(true);
      else clearTerritories();
      return;
    }
    if (e.target.id === "preview-planet") {
      view(e.target.value);
      drop();
      draw(true);
      return;
    }
    if (counts(e.target)) soon();
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
    }
    // Preview map asks for every planet; nothing automatic may overwrite it.
    if (e.detail.elt === button()) drop();
  });
  // Preview map's pictures are in place; the territories come after them.
  document.body.addEventListener("htmx:afterSwap", (e) => {
    if (e.target.id === "preview") territories(false);
  });
  document.body.addEventListener("htmx:afterRequest", (e) => {
    if (!isRefresh(e)) return;
    refreshing = Math.max(0, refreshing - 1);
    if (refreshing === 0) followSettings();
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

  // The list from the last section that had one: a section holding only an
  // error has none, and Previous and Next must still move then.
  let known = [];
  function planets() {
    const pick = el("preview-planet");
    const found = pick ? [...pick.options].map((o) => o.value) : [...document.querySelectorAll("#preview a.picture")].map((a) => a.dataset.planet);
    if (found.length) known = found;
    return known;
  }
  const firstPlanet = () => planets()[0] || "";

  function nameOf(planet) {
    const pick = el("preview-planet");
    const option = pick && [...pick.options].find((o) => o.value === planet);
    if (option) return option.text;
    const link = pictureLink(planet);
    const caption = link && link.closest("figure").querySelector("figcaption");
    return caption ? caption.textContent.trim() : planet;
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
    const pick = el("preview-planet");
    if (pick) pick.value = planet;
    view(planet);
    nameArrows();
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
    if (pending && drawing !== planet) drop();
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
      zoomTo(1);
    });
    el("large-prev").addEventListener("click", () => step(-1));
    el("large-next").addEventListener("click", () => step(1));
    el("large-in").addEventListener("click", () => zoomSaid(zoom * factor));
    el("large-out").addEventListener("click", () => zoomSaid(zoom / factor));
    el("large-close").addEventListener("click", () => dialog.close());
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
    let spread = 0;
    let middle = null;
    frame().addEventListener("pointerdown", (e) => {
      if (e.pointerType === "mouse" && e.button !== 0) return;
      frame().setPointerCapture(e.pointerId);
      pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
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
    };
    frame().addEventListener("pointerup", lift);
    frame().addEventListener("pointercancel", lift);
    frame().addEventListener("lostpointercapture", lift);

    // The arrows step between planets; a zoomed picture with focus scrolls
    // with them instead. The browser's own zoom keys are left alone.
    const arrows = { ArrowLeft: -1, ArrowRight: 1 };
    dialog.addEventListener("keydown", (e) => {
      if (e.ctrlKey || e.metaKey || e.altKey) return;
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
    dialog.addEventListener("close", () => {
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
