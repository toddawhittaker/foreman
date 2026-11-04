// Live pages (SPEC 16.3). Says so when live updates stop, and keeps keyboard
// focus when the Server page swaps in a region. Without this script the
// pages still work; they just do not follow the game.
(() => {
  "use strict";

  // A stream that ends by design is back within a few seconds, before this.
  const graceMs = 5000;
  const text = "Live updates stopped. Reload to see the current state.";
  let timer = null;

  // Called when the live connection drops; shows the note once it has
  // stayed down for the grace period, and reads it out once.
  function lost() {
    if (timer !== null || document.getElementById("live-lost")) return;
    timer = setTimeout(() => {
      timer = null;
      const note = document.createElement("div");
      note.className = "message message--note";
      note.id = "live-lost";
      const p = document.createElement("p");
      const word = document.createElement("strong");
      word.textContent = "Note:";
      p.append(word, " " + text);
      note.append(p);
      document.querySelector("main h1")?.after(note);
      const announcer = document.getElementById("announcer");
      if (announcer) announcer.textContent = text;
    }, graceMs);
  }

  // Called when the live connection is open again.
  function back() {
    clearTimeout(timer);
    timer = null;
    document.getElementById("live-lost")?.remove();
  }

  window.foremanLive = { lost, back };

  document.addEventListener("htmx:sseError", lost);
  document.addEventListener("htmx:sseOpen", back);

  // A region swap removes the element that had focus. htmx gives focus back
  // to the new element with the same id; when there is none, focus goes to
  // the region's heading instead of the top of the page (DESIGN 5).
  let held = null;
  document.addEventListener("htmx:sseBeforeMessage", (e) => {
    held = e.target.contains(document.activeElement) ? e.target : null;
  });
  document.addEventListener("htmx:sseMessage", (e) => {
    if (held !== e.target) return;
    held = null;
    if (e.target.contains(document.activeElement)) return;
    e.target.closest("section")?.querySelector("h2[tabindex='-1']")?.focus();
  });
})();
