// The factory map page (SPEC 5.16): a zoomable, pannable picture of one
// surface with a layer per category of building, and a poll of the page's
// state region while a drawing runs. Written apart from preview.js on
// purpose (plan rule 17); the two may share code once both are stable.
"use strict";

(() => {
  const factor = 1.15;
  const pollMs = 3000;
  // The layout's JSON strings block; "Zoom {level}" is shared with the Generate page.
  let strings = {};
  try {
    strings = JSON.parse(document.getElementById("i18n").textContent);
  } catch {
    strings = {};
  }

  function viewer(region) {
    const figure = region.querySelector(".factorymap__view");
    if (!figure) return;
    region.setAttribute("data-viewer", "");
    const frame = figure.querySelector(".factorymap__frame");
    const map = figure.querySelector(".factorymap__map");
    const status = figure.querySelector(".factorymap__status");
    const zoomBar = figure.querySelector(".factorymap__zoom");
    const width = Number(figure.dataset.width) || 1;
    const height = Number(figure.dataset.height) || 1;
    const lang = document.documentElement.lang || undefined;
    const percent = new Intl.NumberFormat(lang, { style: "percent", maximumFractionDigits: 0 });
    frame.style.setProperty("--ratio", String(width / height));
    zoomBar.hidden = false;
    let zoom = 1;

    // From the fitted size up to the larger of 8 times and 8 CSS pixels a
    // picture pixel, as the Generate page's large view (SPEC 5.6).
    const most = () => Math.max(8, (8 * width) / (map.getBoundingClientRect().width / zoom || 1));

    // Zooms around a point on screen, the frame's centre by default, by
    // moving the scroll position so that point stays on the same spot.
    function zoomTo(next, x, y) {
      const box = frame.getBoundingClientRect();
      if (x === undefined) {
        x = box.left + box.width / 2;
        y = box.top + box.height / 2;
      }
      const before = map.getBoundingClientRect();
      const u = (x - before.left) / (before.width || 1);
      const v = (y - before.top) / (before.height || 1);
      const top = most();
      zoom = Math.min(top, Math.max(1, next));
      frame.style.setProperty("--zoom", String(zoom));
      const after = map.getBoundingClientRect();
      frame.scrollLeft += after.left + u * after.width - x;
      frame.scrollTop += after.top + v * after.height - y;
      frame.toggleAttribute("data-zoomed", zoom > 1);
      zoomBar.querySelector('[data-zoom="out"]').setAttribute("aria-disabled", String(zoom <= 1));
      zoomBar.querySelector('[data-zoom="fit"]').setAttribute("aria-disabled", String(zoom <= 1));
      zoomBar.querySelector('[data-zoom="in"]').setAttribute("aria-disabled", String(zoom >= top * 0.9999));
    }

    // A zoom from a key or a button is said; a wheel or a pinch is seen.
    function zoomSaid(next) {
      zoomTo(next);
      status.textContent = (strings["js.preview.zoom"] || "{level}").replace("{level}", percent.format(zoom));
    }

    zoomBar.addEventListener("click", (e) => {
      const b = e.target.closest("[data-zoom]");
      if (!b) return;
      if (b.dataset.zoom === "in") zoomSaid(zoom * factor);
      else if (b.dataset.zoom === "out") zoomSaid(zoom / factor);
      else zoomSaid(1);
    });

    // One wheel notch is one step; a touchpad's small moves are part steps.
    frame.addEventListener("wheel", (e) => {
      e.preventDefault();
      const steps = e.deltaMode === 0 ? Math.max(-1, Math.min(1, e.deltaY / 100)) : Math.sign(e.deltaY);
      zoomTo(zoom * Math.pow(factor, -steps), e.clientX, e.clientY);
    }, { passive: false });

    // One pointer drags; two pinch around their middle; a tap (under 6 px of
    // movement) on a zoomed picture centres the spot, so panning never needs
    // a drag (WCAG 2.5.7).
    const pointers = new Map();
    let tap = null;
    let pinch = 0;
    const gap = (a, b) => Math.hypot(a.x - b.x, a.y - b.y);
    frame.addEventListener("pointerdown", (e) => {
      if (e.pointerType === "mouse" && e.button !== 0) return;
      frame.setPointerCapture(e.pointerId);
      pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
      tap = pointers.size === 1 ? { id: e.pointerId, x: e.clientX, y: e.clientY } : null;
      if (pointers.size === 2) pinch = gap(...pointers.values());
    });
    frame.addEventListener("pointermove", (e) => {
      const last = pointers.get(e.pointerId);
      if (!last) return;
      const now = { x: e.clientX, y: e.clientY };
      if (pointers.size === 1) {
        frame.scrollLeft -= now.x - last.x;
        frame.scrollTop -= now.y - last.y;
      }
      pointers.set(e.pointerId, now);
      if (pointers.size !== 2) return;
      const [a, b] = [...pointers.values()];
      const d = gap(a, b);
      if (pinch > 0) zoomTo(zoom * (d / pinch), (a.x + b.x) / 2, (a.y + b.y) / 2);
      pinch = d;
    });
    const lift = (e) => {
      pointers.delete(e.pointerId);
      if (pointers.size < 2) pinch = 0;
      if (!tap || tap.id !== e.pointerId) return;
      const moved = Math.hypot(e.clientX - tap.x, e.clientY - tap.y);
      tap = null;
      if (e.type !== "pointerup" || moved >= 6 || zoom <= 1) return;
      const box = frame.getBoundingClientRect();
      frame.scrollLeft += e.clientX - (box.left + box.width / 2);
      frame.scrollTop += e.clientY - (box.top + box.height / 2);
    };
    frame.addEventListener("pointerup", lift);
    frame.addEventListener("pointercancel", lift);
    frame.addEventListener("lostpointercapture", lift);

    // Keys while the frame has focus: arrows pan, plus and minus zoom, 0
    // fits. With Ctrl, Alt or Meta held the browser keeps its own keys.
    frame.addEventListener("keydown", (e) => {
      if (e.ctrlKey || e.metaKey || e.altKey) return;
      const pan = Math.max(32, frame.clientWidth / 8);
      const moves = { ArrowLeft: [-pan, 0], ArrowRight: [pan, 0], ArrowUp: [0, -pan], ArrowDown: [0, pan] };
      if (moves[e.key]) {
        frame.scrollBy(moves[e.key][0], moves[e.key][1]);
      } else if (e.key === "+" || e.key === "=") {
        zoomSaid(zoom * factor);
      } else if (e.key === "-" || e.key === "_") {
        zoomSaid(zoom / factor);
      } else if (e.key === "0") {
        zoomSaid(1);
      } else {
        return;
      }
      e.preventDefault();
    });

    // A ticked category lays its picture over the base; it is fetched the
    // first time it is ticked. The same boxes choose what the download
    // combines on the server.
    const form = region.querySelector(".factorymap__layers");
    const download = region.querySelector("#map-download");
    const sync = () => {
      const q = new URLSearchParams();
      for (const box of form.querySelectorAll('input[name="layer"]')) {
        if (box.checked) q.append("layer", box.value);
        const img = map.querySelector(`img[data-layer="${CSS.escape(box.value)}"]`);
        if (!img) continue;
        if (box.checked && !img.getAttribute("src")) img.src = img.dataset.src;
        img.hidden = !box.checked;
      }
      if (download) download.href = download.dataset.base + (q.size ? `?${q}` : "");
    };
    form.addEventListener("change", sync);
    sync();

    new ResizeObserver(() => zoomTo(zoom)).observe(frame);
    zoomTo(1);
  }

  // While a drawing runs, the region is fetched alone every 3 seconds and
  // swapped in; the first other state ends the poll and is said politely.
  function poll(region) {
    if (region.dataset.state !== "drawing") return;
    setTimeout(async () => {
      let next = null;
      try {
        const res = await fetch(region.dataset.fragment, { headers: { Accept: "text/html" }, credentials: "same-origin" });
        if (res.ok) {
          const t = document.createElement("template");
          t.innerHTML = await res.text();
          next = t.content.querySelector("#factorymap-state");
        }
      } catch {
        next = null;
      }
      if (!next || !region.isConnected) {
        if (region.isConnected) poll(region);
        return;
      }
      const hadFocus = region.contains(document.activeElement);
      region.replaceWith(next);
      if (next.dataset.state !== "drawing") {
        const news = document.getElementById("factorymap-news");
        const summary = next.querySelector("[data-news]");
        if (news && summary) news.textContent = summary.textContent.replace(/\s+/g, " ").trim();
        if (hadFocus) {
          const focus = next.querySelector(".factorymap__frame, button, a");
          if (focus) focus.focus();
        }
      }
      start(next);
    }, pollMs);
  }

  function start(region) {
    viewer(region);
    poll(region);
  }

  const region = document.getElementById("factorymap-state");
  if (region) start(region);
})();
