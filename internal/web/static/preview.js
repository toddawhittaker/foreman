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
  let drawing = ""; // the planet the request on its way is for
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
    swap(fresh);
    // The seed used is kept, so the world made is the one previewed (SPEC 5.6).
    if (seed && seed.value === seedSent && seedSent.trim() === "" && fresh.dataset.seed && formEl().querySelector("#mode-settings:checked")) seed.value = fresh.dataset.seed;
    const filled = formEl().querySelector("#filled-seed");
    if (filled && fresh.dataset.filledSeed) filled.value = fresh.dataset.filledSeed;
    markInvalid(fresh);
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
      // The dialog names the planet at once and shows no other planet's picture meanwhile.
      const pick = el("preview-planet");
      if (pick) pick.value = planet;
      const option = pick && [...pick.options].find((o) => o.value === planet);
      const name = option ? option.text : planet;
      el("large-title").textContent = name;
      el("large-frame").dataset.missing = "true";
      el("large-download").hidden = true;
      el("large-status").textContent = strings["js.preview.drawing"].replace("{planet}", name);
      draw(true);
      return false;
    }
    // A draw on its way for another planet is dropped, or its reply would
    // replace this picture and its link.
    if (pending && drawing !== planet) drop();
    const img = link.querySelector("img");
    const figure = link.closest("figure");
    const download = link.closest("li").querySelector("a.download");
    el("large-title").textContent = figure.querySelector("figcaption").textContent.trim();
    el("large-frame").removeAttribute("data-missing");
    el("large-img").src = img.src;
    el("large-img").alt = img.alt;
    el("large-download").href = download ? download.href : link.href;
    el("large-download").textContent = download ? download.textContent : "";
    el("large-download").hidden = false;
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
      redrawn = false;
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
        el("large-status").textContent = text.replace(/\s+/g, " ").trim();
        return;
      }
      redrawn = true;
      el("large-status").textContent = status ? status.dataset.text : "";
      draw(true);
    });
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
