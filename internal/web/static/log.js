// The Console tab (DESIGN 8.5). Without this script the page still shows the
// log as the server last drew it and the console works by plain form posts.
(() => {
  "use strict";

  // A pixel or two of slack, since scroll positions are fractional.
  const atEnd = (box) => box.scrollHeight - box.scrollTop - box.clientHeight <= 4;

  function followLog(frame) {
    const box = frame.querySelector(".log");
    // The game log's lines are spans in a pre; chat's are list items.
    const pre = box.querySelector("pre, ol");
    const jump = frame.querySelector("button");
    const max = Number(frame.dataset.maxLines);

    // Whether the view follows new lines. Only the reader moving up turns it
    // off and only reaching the end turns it back on, so a rewrap or a late
    // scroll event from our own jump can never stop the follow (DESIGN 8.5).
    let following = true;
    let lastTop = box.scrollTop;
    const readScroll = () => {
      if (atEnd(box)) following = true;
      else if (box.scrollTop < lastTop - 1) following = false;
      lastTop = box.scrollTop;
    };
    const toEnd = () => {
      box.scrollTop = box.scrollHeight;
      lastTop = box.scrollTop;
      following = true;
      jump.hidden = true;
    };
    const keepEnd = () => {
      if (following) toEnd();
    };
    // The game's own Error and Warning lines are coloured, as console.go does.
    const kind = /^\s*\d+\.\d+ (Error|Warning) /;
    const span = (text) => {
      const el = document.createElement("span");
      el.textContent = text + "\n";
      const m = kind.exec(text);
      if (m) el.className = "log-" + m[1].toLowerCase();
      return el;
    };
    // With data-log-html each line comes as one list item the server drew and
    // escaped (DESIGN 8.14); its time is put in the viewer's zone before it
    // is shown, so a screen reader reads the local time.
    const parsed = (html) => {
      const t = document.createElement("template");
      t.innerHTML = html;
      const el = t.content.firstElementChild;
      window.foremanTimes?.run(el);
      return el;
    };
    const row = "logHtml" in frame.dataset ? parsed : span;
    const noEmptyNote = (lines) => {
      if (lines.length > 0) document.getElementById("log-empty")?.remove();
    };

    // New lines are appended, never redrawn, and only a view already at the
    // bottom follows them. Focus is never touched.
    const add = (lines) => {
      readScroll();
      pre.append(...lines.map(row));
      while (pre.childElementCount > max) pre.firstElementChild.remove();
      noEmptyNote(lines);
      if (following) toEnd();
      else jump.hidden = false;
    };

    // A reconnect starts again from every line so far.
    const reset = (lines) => {
      readScroll();
      const top = box.scrollTop;
      pre.replaceChildren(...lines.slice(-max).map(row));
      noEmptyNote(lines);
      if (following) toEnd();
      else {
        box.scrollTop = top;
        lastTop = box.scrollTop;
      }
    };

    box.addEventListener("scroll", () => {
      readScroll();
      if (following) jump.hidden = true;
    });
    // Lines that rewrap (the web font arriving, a turned phone, a resized box)
    // move the end without any new line, so a following view is taken back.
    const resized = new ResizeObserver(keepEnd);
    resized.observe(pre);
    resized.observe(box);
    document.fonts?.addEventListener("loadingdone", keepEnd);
    document.fonts?.ready.then(keepEnd);
    jump.addEventListener("click", () => {
      toEnd();
      // The button is about to disappear; keep the keyboard on the log.
      box.focus();
    });

    toEnd();
    const events = new EventSource(frame.dataset.logEvents);
    events.addEventListener("reset", (e) => reset(JSON.parse(e.data)));
    events.addEventListener("lines", (e) => add(JSON.parse(e.data)));
    // live.js says so when the log stops following the game.
    events.addEventListener("error", () => window.foremanLive?.lost("log"));
    events.addEventListener("open", () => window.foremanLive?.back("log"));
  }

  // Newest last: a history box opens, and after each command shows, its end.
  const historyToEnd = () => {
    for (const box of document.querySelectorAll("[data-follow-end]")) box.scrollTop = box.scrollHeight;
  };

  const frame = document.querySelector("[data-log-events]");
  if (frame) followLog(frame);
  historyToEnd();
  document.addEventListener("htmx:afterSettle", historyToEnd);
})();
