// The Console tab (DESIGN 8.5). Without this script the page still shows the
// log as the server last drew it and the console works by plain form posts.
(() => {
  "use strict";

  // A pixel or two of slack, since scroll positions are fractional.
  const atEnd = (box) => box.scrollHeight - box.scrollTop - box.clientHeight <= 4;

  function followLog(frame) {
    const box = frame.querySelector(".log");
    // The game log's lines are spans in a pre; chat's are list items.
    const pre = (box.querySelector("pre") || box.querySelector("ol"));
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
    // Each line is split into its parts, so a phone can set the clock in a
    // column of its own and the date once above the day's first line: the
    // time's date and clock, and the words, where a path may break only
    // after a slash. The text itself is unchanged.
    const dateClock = /^(\s*)(\d{4}-\d\d-\d\d) (.*)$/s;
    const dress = (el) => {
      if (el.querySelector(":scope > .log-msg")) return el;
      const time = el.querySelector(":scope > .log-time");
      const words = [...el.childNodes].filter((n) => n !== time).map((n) => n.textContent).join("");
      const msg = document.createElement("span");
      msg.className = "log-msg";
      words.split("/").forEach((part, i, all) => {
        if (i < all.length - 1) msg.append(part + "/", document.createElement("wbr"));
        else msg.append(part);
      });
      if (time) {
        const m = dateClock.exec(time.textContent);
        const clock = document.createElement("span");
        clock.className = "log-clock";
        if (m) {
          const date = document.createElement("span");
          date.className = "log-date";
          date.textContent = m[1] + m[2] + " ";
          clock.textContent = m[3];
          time.replaceChildren(date, clock);
        } else {
          clock.textContent = time.textContent;
          time.replaceChildren(clock);
        }
      }
      el.replaceChildren(...(time ? [time] : []), msg);
      return el;
    };
    // The first line of each day carries data-day, which shows its date.
    const markDays = () => {
      let day = "";
      for (const line of pre.children) {
        const date = line.querySelector(":scope > .log-time > .log-date")?.textContent.trim();
        if (date && date !== day) {
          line.dataset.day = "";
          day = date;
        } else delete line.dataset.day;
      }
    };
    if (pre.tagName === "PRE") {
      for (const line of pre.children) dress(line);
      markDays();
    }

    // The game's own Error and Warning lines, and players joining or
    // leaving, are coloured, and the time is set apart, as console.go does.
    const kind = /^\s*\d+\.\d+ (Error|Warning) /;
    const player = /^\d{4}-\d\d-\d\d \d\d:\d\d:\d\d \[(JOIN|LEAVE)\] /;
    const stamp = /^\s*(?:\d+\.\d+|\d{4}-\d\d-\d\d \d\d:\d\d:\d\d) /;
    const span = (text) => {
      const el = document.createElement("span");
      const s = stamp.exec(text);
      if (s) {
        const time = document.createElement("span");
        time.className = "log-time";
        time.textContent = s[0];
        el.append(time);
      }
      el.append(text.slice(s ? s[0].length : 0) + "\n");
      const m = kind.exec(text);
      if (m) el.className = "log-" + m[1].toLowerCase();
      else {
        const p = player.exec(text);
        if (p) el.className = p[1] === "LEAVE" ? "log-join log-leave" : "log-join";
      }
      return dress(el);
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
      if (row === span) markDays();
      noEmptyNote(lines);
      if (following) toEnd();
      else jump.hidden = false;
    };

    // A reconnect starts again from every line so far.
    const reset = (lines) => {
      readScroll();
      const top = box.scrollTop;
      pre.replaceChildren(...lines.slice(-max).map(row));
      if (row === span) markDays();
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

  // After a command is sent, the newest reply is brought onto the screen (on
  // a phone it would land just below it). Focus stays where it was.
  document.addEventListener("htmx:afterSettle", (e) => {
    const form = e.detail && e.detail.requestConfig && e.detail.requestConfig.elt;
    if (!form || !form.matches || !form.matches(".console-form")) return;
    const reply = document.getElementById("last-reply");
    if (reply) reply.scrollIntoView({ block: "nearest" });
  });
})();
