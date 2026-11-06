// The Console tab (DESIGN 8.5). Without this script the page still shows the
// log as the server last drew it and the console works by plain form posts.
(() => {
  "use strict";

  // A pixel or two of slack, since scroll positions are fractional.
  const atEnd = (box) => box.scrollHeight - box.scrollTop - box.clientHeight <= 4;

  function followLog(frame) {
    const box = frame.querySelector(".log");
    const pre = box.querySelector("pre");
    const jump = frame.querySelector("button");
    const max = Number(frame.dataset.maxLines);

    const toEnd = () => {
      box.scrollTop = box.scrollHeight;
      jump.hidden = true;
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
    const noEmptyNote = (lines) => {
      if (lines.length > 0) document.getElementById("log-empty")?.remove();
    };

    // New lines are appended, never redrawn, and only a view already at the
    // bottom follows them. Focus is never touched.
    const add = (lines) => {
      const follow = atEnd(box);
      pre.append(...lines.map(span));
      while (pre.childElementCount > max) pre.firstElementChild.remove();
      noEmptyNote(lines);
      if (follow) toEnd();
      else jump.hidden = false;
    };

    // A reconnect starts again from every line so far.
    const reset = (lines) => {
      const follow = atEnd(box);
      const top = box.scrollTop;
      pre.replaceChildren(...lines.slice(-max).map(span));
      noEmptyNote(lines);
      if (follow) toEnd();
      else box.scrollTop = top;
    };

    box.addEventListener("scroll", () => {
      if (atEnd(box)) jump.hidden = true;
    });
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
