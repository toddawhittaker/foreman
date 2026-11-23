// The picture viewer shared by the factory map (SPEC 5.16) and the Generate
// page's large view (SPEC 5.6, DESIGN 8.8): a picture zoomed and panned
// inside a scrolling frame. Each page keeps its own extras.
//
// ForemanViewer.attach(frame, opts) returns { zoom(), zoomTo(next, x, y),
// zoomSaid(next), factor }. opts:
//   map()     the picture element inside the frame, measured on each zoom
//   most()    the largest zoom allowed now
//   buttons   { in, out, fit } elements; each is optional
//   say(text) speaks a zoom from a key or a button
//   zoomed()  runs after every zoom
//   keys      true to handle arrows, + - and 0 on the frame itself
"use strict";

(() => {
  const factor = 1.15;

  function attach(frame, opts) {
    const buttons = opts.buttons || {};
    const lang = document.documentElement.lang || undefined;
    const percent = new Intl.NumberFormat(lang, { style: "percent", maximumFractionDigits: 0 });
    let zoom = 1;

    // Zooms around a point on screen, the frame's centre by default, by
    // moving the scroll position so that point stays on the same spot.
    function zoomTo(next, x, y) {
      const map = opts.map();
      if (x === undefined) {
        const box = frame.getBoundingClientRect();
        x = box.left + frame.clientWidth / 2;
        y = box.top + frame.clientHeight / 2;
      }
      const before = map.getBoundingClientRect();
      const u = (x - before.left) / (before.width || 1);
      const v = (y - before.top) / (before.height || 1);
      const top = opts.most(zoom);
      zoom = Math.min(top, Math.max(1, next));
      frame.style.setProperty("--zoom", String(zoom));
      const after = map.getBoundingClientRect();
      frame.scrollLeft += after.left + u * after.width - x;
      frame.scrollTop += after.top + v * after.height - y;
      frame.toggleAttribute("data-zoomed", zoom > 1);
      if (buttons.out) buttons.out.setAttribute("aria-disabled", String(zoom <= 1));
      if (buttons.fit) buttons.fit.setAttribute("aria-disabled", String(zoom <= 1));
      if (buttons.in) buttons.in.setAttribute("aria-disabled", String(zoom >= top * 0.9999));
      if (opts.zoomed) opts.zoomed();
    }

    // A zoom from a key or a button is said; a wheel or a pinch is seen.
    function zoomSaid(next) {
      zoomTo(next);
      const strings = opts.strings || {};
      if (opts.say) opts.say((strings["js.preview.zoom"] || "{level}").replace("{level}", percent.format(zoom)));
    }

    if (buttons.in) buttons.in.addEventListener("click", () => zoomSaid(zoom * factor));
    if (buttons.out) buttons.out.addEventListener("click", () => zoomSaid(zoom / factor));
    if (buttons.fit) buttons.fit.addEventListener("click", () => zoomSaid(1));

    // One wheel notch is one step; a touchpad's small moves are part steps.
    frame.addEventListener("wheel", (e) => {
      e.preventDefault();
      const steps = e.deltaMode === 0 ? Math.max(-1, Math.min(1, e.deltaY / 100)) : Math.sign(e.deltaY);
      zoomTo(zoom * Math.pow(factor, -steps), e.clientX, e.clientY);
    }, { passive: false });

    // One pointer drags; two pinch around their middle, which also pans as
    // it moves; a tap (under 6 px of movement) on a zoomed picture centres
    // the spot, so panning never needs a drag (WCAG 2.5.7).
    const pointers = new Map();
    let tap = null;
    let spread = 0;
    let middle = null;
    frame.addEventListener("pointerdown", (e) => {
      if (e.pointerType === "mouse" && e.button !== 0) return;
      frame.setPointerCapture(e.pointerId);
      pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
      tap = pointers.size === 1 ? { id: e.pointerId, x: e.clientX, y: e.clientY } : null;
      spread = 0;
      if (pointers.size !== 2) return;
      const [a, b] = [...pointers.values()];
      spread = Math.hypot(a.x - b.x, a.y - b.y);
      middle = { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
    });
    frame.addEventListener("pointermove", (e) => {
      const last = pointers.get(e.pointerId);
      if (!last) return;
      if (pointers.size === 1) {
        frame.scrollLeft -= e.clientX - last.x;
        frame.scrollTop -= e.clientY - last.y;
      }
      pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
      if (pointers.size !== 2) return;
      const [a, b] = [...pointers.values()];
      const apart = Math.hypot(a.x - b.x, a.y - b.y);
      const mid = { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
      if (spread) {
        frame.scrollLeft -= mid.x - middle.x;
        frame.scrollTop -= mid.y - middle.y;
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
      const box = frame.getBoundingClientRect();
      frame.scrollLeft += e.clientX - (box.left + box.width / 2);
      frame.scrollTop += e.clientY - (box.top + box.height / 2);
    };
    frame.addEventListener("pointerup", lift);
    frame.addEventListener("pointercancel", lift);
    frame.addEventListener("lostpointercapture", lift);

    // Keys while the frame has focus: arrows pan, plus and minus zoom, 0
    // fits. With Ctrl, Alt or Meta held the browser keeps its own keys.
    if (opts.keys) {
      frame.addEventListener("keydown", (e) => {
        if (e.ctrlKey || e.metaKey || e.altKey) return;
        const pan = Math.max(32, frame.clientWidth / 8);
        const moves = { ArrowLeft: [-pan, 0], ArrowRight: [pan, 0], ArrowUp: [0, -pan], ArrowDown: [0, pan] };
        if (moves[e.key]) frame.scrollBy(moves[e.key][0], moves[e.key][1]);
        else if (e.key === "+" || e.key === "=") zoomSaid(zoom * factor);
        else if (e.key === "-" || e.key === "_") zoomSaid(zoom / factor);
        else if (e.key === "0") zoomSaid(1);
        else return;
        e.preventDefault();
      });
    }

    return { factor, zoom: () => zoom, zoomTo, zoomSaid };
  }

  window.ForemanViewer = { attach };
})();
