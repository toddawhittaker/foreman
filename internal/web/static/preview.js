// The map preview on Generate (SPEC 5.6; DESIGN 8.8). It draws on page load
// and about 0.8 s after the last change to the map's settings, only for the
// planet being viewed; the newest request wins and older ones are dropped.
// It never moves focus. The large view opens each picture in the page, with
// Previous, Next and zoom; without script the picture link opens the file.
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
  let refreshing = 0; // htmx redraws of the settings still on their way
  let viewing = ""; // the planet shown, blank for the first

  const dialog = document.getElementById("large-view");
  const status = document.getElementById("preview-status");

  function announce(said) {
    // Outside an open modal dialog the page is inert, so the dialog says it itself.
    const target = dialog && dialog.open ? document.getElementById("large-status") : document.getElementById("announcer");
    if (!target) return;
    target.textContent = "";
    setTimeout(() => { target.textContent = said; }, 50);
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
  }

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
      failed();
      return;
    }
    if (n !== sent) return;
    pending = null;
    setStatus(false);
    const fresh = new DOMParser().parseFromString(html, "text/html").getElementById("preview");
    if (!fresh) return;
    swap(fresh);
    // The seed used is kept, so the world made is the one previewed (SPEC 5.6).
    if (seed && seed.value === seedSent && seedSent.trim() === "" && fresh.dataset.seed && formEl().querySelector("#mode-settings:checked")) seed.value = fresh.dataset.seed;
    const filled = formEl().querySelector("#filled-seed");
    if (filled && fresh.dataset.filledSeed) filled.value = fresh.dataset.filledSeed;
    if (fresh.dataset.preview === "invalid") return;
    if (say) {
      const error = fresh.querySelector(".message");
      announce(error ? error.textContent.replace(/\s+/g, " ").trim() : strings["js.generate.preview_ready"]);
    }
    if (dialog && dialog.open) show(viewing || firstPlanet());
  }

  // Puts the new section in place; a control that had focus there keeps it.
  function swap(fresh) {
    const slot = document.getElementById("preview");
    if (!slot) return;
    const focused = slot.contains(document.activeElement) && document.activeElement.id;
    slot.replaceWith(document.importNode(fresh, true));
    if (focused) {
      const again = document.getElementById(focused);
      if (again) again.focus({ preventScroll: true });
    }
  }

  // htmx swaps nothing on a failed request, so the page's own sentence is shown.
  function failed() {
    const slot = document.getElementById("preview");
    const message = document.getElementById("preview-failed");
    if (!slot || !message) return;
    slot.replaceChildren(message.content.cloneNode(true));
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
    if (e.target.id === "preview-planet") {
      viewing = e.target.value;
      drop();
      draw(true);
      return;
    }
    if (counts(e.target)) soon();
  });

  const isRefresh = (e) => (e.detail.pathInfo ? e.detail.pathInfo.requestPath : "").endsWith("/generate/refresh");
  document.body.addEventListener("htmx:beforeRequest", (e) => {
    if (isRefresh(e)) {
      refreshing++;
      drop();
    }
    // Preview map asks for every planet; nothing automatic may overwrite it.
    if (e.detail.elt === button()) drop();
  });
  document.body.addEventListener("htmx:afterRequest", (e) => {
    if (!isRefresh(e)) return;
    refreshing = Math.max(0, refreshing - 1);
    // A redraw that ends while Preview map runs leaves that preview to finish.
    if (refreshing === 0 && button().getAttribute("aria-disabled") !== "true") soon();
  });

  // The large view.
  const zooms = [1, 2, 4, 8];
  let zoom = 0;
  const lang = document.documentElement.lang || undefined;
  const el = (id) => document.getElementById(id);

  function planets() {
    const pick = el("preview-planet");
    if (pick) return [...pick.options].map((o) => o.value);
    return [...document.querySelectorAll("#preview a.picture")].map((a) => a.dataset.planet);
  }
  const firstPlanet = () => planets()[0] || "";

  function setZoom(i) {
    const frame = el("large-frame");
    const before = { x: (frame.scrollLeft + frame.clientWidth / 2) / (frame.scrollWidth || 1), y: (frame.scrollTop + frame.clientHeight / 2) / (frame.scrollHeight || 1) };
    zoom = Math.max(0, Math.min(zooms.length - 1, i));
    frame.style.setProperty("--zoom", String(zooms[zoom]));
    el("large-zoom").textContent = strings["js.preview.zoom"].replace("{level}", new Intl.NumberFormat(lang, { style: "percent" }).format(zooms[zoom]));
    el("large-out").setAttribute("aria-disabled", String(zoom === 0));
    el("large-in").setAttribute("aria-disabled", String(zoom === zooms.length - 1));
    // The middle of what was shown stays in the middle.
    frame.scrollLeft = before.x * frame.scrollWidth - frame.clientWidth / 2;
    frame.scrollTop = before.y * frame.scrollHeight - frame.clientHeight / 2;
  }

  // Shows a planet's picture, or draws it first when it has not been drawn.
  function show(planet) {
    const link = document.querySelector(`#preview a.picture[data-planet="${CSS.escape(planet)}"]`);
    viewing = planet;
    if (!link) {
      const pick = el("preview-planet");
      if (pick) pick.value = planet;
      el("large-status").textContent = status ? status.dataset.text : "";
      draw(true);
      return false;
    }
    const img = link.querySelector("img");
    const figure = link.closest("figure");
    const download = link.closest("li").querySelector("a.download");
    el("large-title").textContent = figure.querySelector("figcaption").textContent.trim();
    el("large-img").src = img.src;
    el("large-img").alt = img.alt;
    el("large-download").href = download ? download.href : link.href;
    el("large-download").textContent = download ? download.textContent : "";
    const many = planets().length > 1;
    el("large-prev").hidden = !many;
    el("large-next").hidden = !many;
    const pick = el("preview-planet");
    if (pick) pick.value = planet;
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
      show(link.dataset.planet);
      el("large-status").textContent = "";
      dialog.showModal();
      setZoom(0);
    });
    el("large-prev").addEventListener("click", () => step(-1));
    el("large-next").addEventListener("click", () => step(1));
    el("large-in").addEventListener("click", () => setZoom(zoom + 1));
    el("large-out").addEventListener("click", () => setZoom(zoom - 1));
    el("large-close").addEventListener("click", () => dialog.close());
    // The arrows step between planets; a zoomed picture with focus scrolls with them instead.
    const arrows = { ArrowLeft: -1, ArrowRight: 1 };
    dialog.addEventListener("keydown", (e) => {
      const panning = e.target.id === "large-frame" && zoom > 0;
      if (arrows[e.key] && !panning) step(arrows[e.key]);
      else if (e.key === "+" || e.key === "=") setZoom(zoom + 1);
      else if (e.key === "-") setZoom(zoom - 1);
      else if (e.key === "0") setZoom(0);
      else return;
      e.preventDefault();
    });
    // Focus goes back to the picture shown, which is the one that opened it
    // or, after Next, its neighbour.
    dialog.addEventListener("close", () => {
      const link = document.querySelector(`#preview a.picture[data-planet="${CSS.escape(viewing)}"]`) || document.querySelector("#preview a.picture");
      if (link) link.focus();
    });
  }

  draw(false);
});
