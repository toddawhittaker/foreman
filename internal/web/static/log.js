// The Console tab (DESIGN 8.5). Without this script the page still shows the
// log as the server last drew it and the console works by plain form posts.
(() => {
  "use strict";

  const t = window.foremanText;

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
    // Set while the reader has turned Follow off; reaching the end then
    // does not turn it back on.
    let held = false;
    let lastTop = box.scrollTop;
    const readScroll = () => {
      if (atEnd(box)) {
        if (!held) setFollowing(true);
      }
      else if (box.scrollTop < lastTop - 1) setFollowing(false);
      lastTop = box.scrollTop;
    };
    // The console's bar: find, a filter by kind of line, and Follow, which
    // says whether the view follows and can stop or restart it.
    const tools = pre.tagName === "PRE" ? frame.parentElement.querySelector(".log-tools") : null;
    const follow = tools?.querySelector("#log-follow");
    const setFollowing = (on) => {
      following = on;
      if (follow) follow.checked = on;
    };
    const toEnd = () => {
      box.scrollTop = box.scrollHeight;
      lastTop = box.scrollTop;
      setFollowing(true);
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
    // The first shown line of each day carries data-day, which shows its
    // date, so a filter that hides a day's first line keeps the date.
    const markDays = () => {
      let day = "";
      for (const line of pre.children) {
        const date = line.hidden ? "" : line.querySelector(":scope > .log-time > .log-date")?.textContent.trim();
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

    // A line shows when it is of the chosen kind and holds the words looked for.
    const kindOf = (line) => {
      if (line.classList.contains("log-error")) return "error";
      if (line.classList.contains("log-warning")) return "warning";
      return / \[CHAT\] /.test(line.textContent) ? "chat" : "";
    };
    const sift = (line) => {
      if (!tools) return line;
      const kind = tools.querySelector("input[name=log-filter]:checked")?.value || "";
      const words = tools.querySelector("#log-find").value.trim().toLowerCase();
      line.hidden = (kind !== "" && kindOf(line) !== kind) || (words !== "" && !line.textContent.toLowerCase().includes(words));
      return line;
    };
    // With a filter or words to find, the log says how many lines show, in
    // a polite status line, and in the log itself when none do.
    const status = document.getElementById("log-count");
    const noMatch = document.getElementById("log-no-match");
    const filtering = () => tools.querySelector("input[name=log-filter]:checked")?.value !== "" || tools.querySelector("#log-find").value.trim() !== "";
    const shownCount = () => [...pre.children].filter((line) => !line.hidden).length;
    const showNoMatch = () => {
      if (noMatch) noMatch.hidden = !(filtering() && pre.childElementCount > 0 && shownCount() === 0);
    };
    const sayCount = () => {
      if (!status) return;
      const shown = shownCount();
      if (!filtering()) status.textContent = "";
      else if (shown === 0) status.textContent = noMatch?.textContent ?? "";
      else status.textContent = t("js.console.shown", { shown, count: pre.childElementCount });
    };
    let sayLater = 0;
    if (tools) {
      tools.hidden = false;
      const siftAll = () => {
        for (const line of pre.children) sift(line);
        if (pre.tagName === "PRE") markDays();
        showNoMatch();
        keepEnd();
      };
      tools.addEventListener("input", (e) => {
        if (e.target === follow) return;
        siftAll();
        // While words are typed the count is said once the typing pauses.
        clearTimeout(sayLater);
        if (e.target.id === "log-find") sayLater = setTimeout(sayCount, 500);
        else sayCount();
      });
      // A filter the browser kept over a reload applies to the lines already drawn.
      siftAll();
      if (filtering()) sayCount();
      follow.addEventListener("change", () => {
        held = !follow.checked;
        if (follow.checked) toEnd();
        else setFollowing(false);
      });
    }
    const noEmptyNote = (lines) => {
      if (lines.length > 0) document.getElementById("log-empty")?.remove();
    };

    // New lines are appended, never redrawn, and only a view already at the
    // bottom follows them. Focus is never touched.
    const add = (lines) => {
      readScroll();
      pre.append(...lines.map((l) => sift(row(l))));
      while (pre.childElementCount > max) pre.firstElementChild.remove();
      if (row === span) markDays();
      noEmptyNote(lines);
      if (tools) showNoMatch();
      if (following) toEnd();
      else jump.hidden = false;
    };

    // A reconnect starts again from every line so far.
    const reset = (lines) => {
      readScroll();
      const top = box.scrollTop;
      pre.replaceChildren(...lines.slice(-max).map((l) => sift(row(l))));
      if (row === span) markDays();
      noEmptyNote(lines);
      if (tools) showNoMatch();
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
      held = false;
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
  // A closed "Past commands" box has no height, so its end is found again
  // when it opens. "toggle" does not bubble, and htmx may replace the
  // details, so it is caught on the document.
  document.addEventListener("toggle", (e) => {
    if (e.target.matches?.(".console-past") && e.target.open) historyToEnd();
  }, true);

  // After a command or a message is sent the page stays where it is: the
  // reply is read out, and the history box shows its end (DESIGN 6, "Toasts":
  // nothing moves because of a change elsewhere). The swap puts focus back on
  // the new panel's control, and the browser may scroll to bring it into
  // view, so the scroll is put back. A refusal htmx does not swap keeps no
  // scroll, so a later swap elsewhere never jumps back to it.
  let keptScroll = null;
  document.addEventListener("htmx:beforeSwap", (e) => {
    if (!e.detail.shouldSwap) return;
    if (e.detail.target?.matches?.("#console-panel, #chat-panel")) keptScroll = window.scrollY;
  });
  document.addEventListener("htmx:afterRequest", (e) => {
    if (!e.detail.successful) keptScroll = null;
  });
  document.addEventListener("htmx:afterSettle", () => {
    if (keptScroll === null) return;
    if (window.scrollY !== keptScroll) window.scrollTo({ top: keptScroll, behavior: "instant" });
    keptScroll = null;
  });
})();
