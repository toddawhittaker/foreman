// Shows each <time datetime="..."> in the browser's time zone, followed by
// how long ago it was, refreshed every minute. Without this script the page
// keeps the UTC text the server wrote (DESIGN 9). Durations such as the
// uptime are left alone. A time inside visually hidden text (part of a
// control's spoken name) or inside a button's label gets the local time
// only: relative text there would go stale and make the name long. A
// button whose time is the point of it, such as the next scheduled restart,
// asks for the age with data-age; it is refreshed with the rest. A time
// still to come reads forward ("in 7 hours").
(function () {
  "use strict";

  var lang = document.documentElement.lang;
  var ages = [];

  var absolute = /^\d{4}-\d\d-\d\dT/;
  var steps = [
    ["second", 60],
    ["minute", 60],
    ["hour", 24],
    ["day", 30],
    ["month", 12],
    ["year", Infinity],
  ];

  function relative(date) {
    var rtf = new Intl.RelativeTimeFormat(lang, { numeric: "auto" });
    var amount = (date.getTime() - Date.now()) / 1000;
    for (var i = 0; i < steps.length; i++) {
      if (Math.abs(amount) < steps[i][1] || steps[i][1] === Infinity) {
        return rtf.format(Math.round(amount), steps[i][0]);
      }
      amount /= steps[i][1];
    }
  }

  function localize(el) {
    var iso = el.getAttribute("datetime");
    if (el.dataset.local || !iso || !absolute.test(iso)) {
      return;
    }
    var date = new Date(iso);
    if (isNaN(date.getTime())) {
      return;
    }
    var withSeconds = /:\d\d:\d\d/.test(el.textContent);
    // The year is left out for a date in the current year, to keep the line short.
    var dayOptions = { month: "short", day: "numeric" };
    if (date.getFullYear() !== new Date().getFullYear()) {
      dayOptions.year = "numeric";
    }
    var day = new Intl.DateTimeFormat(lang, dayOptions).format(date);
    var clockParts = new Intl.DateTimeFormat(lang, {
      hour: "numeric",
      minute: "2-digit",
      second: withSeconds ? "2-digit" : undefined,
      timeZoneName: "short",
    }).formatToParts(date);
    var clock = clockParts
      .filter(function (p) {
        return p.type !== "timeZoneName";
      })
      .map(function (p) {
        return p.value;
      })
      .join("")
      .trim();
    var zone = document.createElement("span");
    zone.className = "nowrap";
    zone.textContent = clockParts.filter(function (p) {
      return p.type === "timeZoneName";
    })[0].value;

    // The date, the clock and the zone each stay on one line, and the zone,
    // such as "GMT+5:45", may take a line of its own (DESIGN 8.3). A table
    // cell already holds the date and the clock in .nowrap spans.
    var parts = el.querySelectorAll(":scope > .nowrap");
    var dayNode = day;
    var clockNode = clock;
    if (parts.length === 2) {
      parts[0].textContent = day;
      parts[1].textContent = clock;
      dayNode = parts[0];
      clockNode = parts[1];
    }
    // Some languages, such as Chinese, put the zone before the clock.
    var order = clockParts[0].type === "timeZoneName" ? [dayNode, zone, clockNode] : [dayNode, clockNode, zone];
    el.replaceChildren(order[0], " ", order[1], " ", order[2]);
    el.dataset.local = "true";
    if (el.closest(".visually-hidden") || (el.closest(".button") && !el.hasAttribute("data-age"))) {
      return;
    }
    var age = document.createElement("span");
    age.textContent = " (" + relative(date) + ")";
    ages.push({ date: date, el: age, plain: false });
    if (el.hasAttribute("data-age-only")) {
      // The full text and the age alone are both written; the style sheet
      // shows one, by the width of the panel (DESIGN 8.2).
      var full = document.createElement("span");
      full.className = "time-full";
      full.append.apply(full, el.childNodes);
      full.append(age);
      var short = document.createElement("span");
      short.className = "time-short";
      short.textContent = relative(date);
      ages.push({ date: date, el: short, plain: true });
      el.replaceChildren(short, full);
      return;
    }
    el.append(age);
  }

  function refresh() {
    ages = ages.filter(function (a) {
      return a.el.isConnected;
    });
    ages.forEach(function (a) {
      a.el.textContent = a.plain ? relative(a.date) : " (" + relative(a.date) + ")";
    });
  }

  // A time already shown in local time is skipped (data-local), so calling
  // this again on the same element is harmless.
  function run(root) {
    root.querySelectorAll("time[datetime]").forEach(localize);
  }

  // Lets a script put a region in local time before a screen reader reads it,
  // without waiting for the page to settle.
  window.foremanTimes = { run: run };

  run(document);
  setInterval(refresh, 60000);
  document.addEventListener("htmx:afterSettle", function () {
    run(document);
  });
})();
