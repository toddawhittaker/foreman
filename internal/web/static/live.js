// Live pages (SPEC 16.3). Says so when live updates stop, and keeps keyboard
// focus when the Server page swaps in a region. Without this script the
// pages still work; they just do not follow the game.
(() => {
  "use strict";

  // A stream that ends by design is back within a few seconds, before this.
  const graceMs = 5000;
  let timer = null;

  // Text from the page's #i18n block, in the page's language.
  const strings = JSON.parse(document.getElementById("i18n").textContent);
  const t = (key) => strings[key];

  // Reads a sentence out through #announcer. The region is emptied first and
  // filled a moment later, so a sentence the same as the last one is still
  // read (DESIGN 7.5).
  function announce(said) {
    const announcer = document.getElementById("announcer");
    if (!announcer) return;
    announcer.textContent = "";
    setTimeout(() => {
      announcer.textContent = said;
    }, 50);
  }

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
      word.textContent = t("common.label.note");
      const stopped = t("js.live.stopped");
      p.append(word, " " + stopped);
      note.append(p);
      document.querySelector("main h1")?.after(note);
      announce(stopped);
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

  // The active world changed in a switch, so this page's world is no longer
  // the one the Server page shows: load the page again to show the new one.
  // The reload would drop the announcement, so the sentence is kept for the
  // next page to read out (SPEC 16.3). The server's sentence is kept as it
  // came; when there was none, the stored value is empty and the next page
  // says the standard sentence in its own language.
  const movedKey = "foreman-moved";
  document.addEventListener("htmx:sseMessage", (e) => {
    if (e.target.getAttribute("sse-swap") !== "moved") return;
    try {
      sessionStorage.setItem(movedKey, e.target.textContent);
    } catch (_) {
      // Without storage the page still reloads, only silently.
    }
    location.reload();
  });

  // After a reload that followed a switch, focus goes to the control heading
  // and the sentence is read out. The pause lets a screen reader settle on
  // the new page first.
  try {
    const said = sessionStorage.getItem(movedKey);
    if (said !== null) {
      sessionStorage.removeItem(movedKey);
      document.getElementById("control-heading")?.focus();
      setTimeout(() => announce(said || t("js.live.moved")), 500);
    }
  } catch (_) {
    // Storage is blocked, so there is nothing to read out.
  }

  // A region swap removes the element that had focus. htmx gives focus back
  // to the new element with the same id; when there is none, focus goes to
  // the region's heading instead of the top of the page (DESIGN 5).
  let held = null;
  document.addEventListener("htmx:sseBeforeMessage", (e) => {
    // The server's sentence is read out here rather than swapped in, so it
    // can be emptied first.
    if (e.target.getAttribute("sse-swap") === "announce") {
      e.preventDefault();
      const doc = new DOMParser().parseFromString(e.detail.data, "text/html");
      announce(doc.body.textContent.trim());
      return;
    }
    held = e.target.contains(document.activeElement) ? e.target : null;
  });
  document.addEventListener("htmx:sseMessage", (e) => {
    if (held !== e.target) return;
    held = null;
    if (e.target.contains(document.activeElement)) return;
    // A region outside any section, such as the switch panel, hands focus to
    // the first heading that takes it: the start and stop controls.
    (e.target.closest("section") ?? document).querySelector("h2[tabindex='-1']")?.focus();
  });

  // After the admin's own cancel stop or snapshot, the Done message takes
  // focus once, as soon as it is there: at once when the page already shows
  // it, or when a live update brings it. The server marks such a load with
  // data-focus-result on the control region (SPEC 17.9).
  function focusDone() {
    const region = document.querySelector("[data-focus-result]");
    const done = region?.querySelector("#done-message");
    if (!done) return;
    region.removeAttribute("data-focus-result");
    done.focus();
  }
  focusDone();
  document.addEventListener("htmx:sseMessage", (e) => {
    if (e.target.hasAttribute("data-focus-result")) focusDone();
  });
})();
