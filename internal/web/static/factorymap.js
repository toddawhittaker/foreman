// The factory map page (SPEC 5.16): a zoomable, pannable picture of one
// surface with a layer per category of building, and a poll of the page's
// state region while a drawing runs. Zoom and pan come from viewer.js.
"use strict";

(() => {
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
    frame.style.setProperty("--ratio", String(width / height));
    zoomBar.hidden = false;
    // From the fitted size up to the larger of 8 times and 8 CSS pixels a
    // picture pixel, as the Generate page's large view (SPEC 5.6).
    const view = window.ForemanViewer.attach(frame, {
      map: () => map,
      most: (zoom) => Math.max(8, (8 * width) / (map.getBoundingClientRect().width / zoom || 1)),
      buttons: {
        in: zoomBar.querySelector('[data-zoom="in"]'),
        out: zoomBar.querySelector('[data-zoom="out"]'),
        fit: zoomBar.querySelector('[data-zoom="fit"]'),
      },
      strings,
      say: (text) => { status.textContent = text; },
      keys: true,
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

    new ResizeObserver(() => view.zoomTo(view.zoom())).observe(frame);
    view.zoomTo(1);
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
      // Still drawing: swap only the parts a drawing changes, and only when
      // they changed, so focus, zoom, ticked layers and a screen reader's
      // place in a kept picture survive each poll.
      if (next.dataset.state === "drawing") {
        for (const sel of [".factorymap__summary", ".factorymap__draw", "#fm-refused"]) {
          const old = region.querySelector(sel);
          const now = next.querySelector(sel);
          if (old && now) {
            if (old.innerHTML !== now.innerHTML) old.replaceWith(now);
          } else if (old) {
            old.remove();
          } else if (now) {
            region.querySelector(".factorymap__summary").after(now);
          }
        }
        poll(region);
        return;
      }
      // The drawing ended: the whole region is swapped once, and focus goes
      // back to the control that had it, matched by id, or by name and value.
      const at = region.contains(document.activeElement) ? document.activeElement : null;
      region.replaceWith(next);
      const news = document.getElementById("factorymap-news");
      const summary = next.querySelector("[data-news]");
      if (news && summary) news.textContent = summary.textContent.replace(/\s+/g, " ").trim();
      // Focus moves after start, which unhides the zoom buttons.
      start(next);
      if (at) {
        const match = (at.id && next.querySelector(`#${CSS.escape(at.id)}`)) ||
          (at.name && next.querySelector(`[name="${CSS.escape(at.name)}"][value="${CSS.escape(at.value || "")}"]`)) ||
          (at.dataset.zoom && next.querySelector(`[data-zoom="${CSS.escape(at.dataset.zoom)}"]`)) ||
          next.querySelector(".factorymap__frame,button,a");
        if (match) match.focus();
      }
    }, pollMs);
  }

  function start(region) {
    viewer(region);
    poll(region);
  }

  const region = document.getElementById("factorymap-state");
  if (region) start(region);
})();
