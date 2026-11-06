// Shows each <time datetime="..."> in the browser's time zone, followed by
// how long ago it was, refreshed every minute. Without this script the page
// keeps the UTC text the server wrote (DESIGN 9). Durations such as the
// uptime are left alone. A time inside visually hidden text (part of a
// control's spoken name) gets the local time only: relative text there
// would go stale and make the name long.
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
    var clock = new Intl.DateTimeFormat(lang, {
      hour: "numeric",
      minute: "2-digit",
      second: withSeconds ? "2-digit" : undefined,
      timeZoneName: "short",
    }).format(date);

    // A table cell keeps its date and clock each on one line (DESIGN 8.3).
    var parts = el.querySelectorAll(":scope > .nowrap");
    if (parts.length === 2) {
      parts[0].textContent = day;
      parts[1].textContent = clock;
    } else {
      el.textContent = day + " " + clock;
    }
    el.dataset.local = "true";
    if (el.closest(".visually-hidden")) {
      return;
    }
    var age = document.createElement("span");
    age.textContent = " (" + relative(date) + ")";
    el.append(age);
    ages.push({ date: date, el: age });
  }

  function refresh() {
    ages = ages.filter(function (a) {
      return a.el.isConnected;
    });
    ages.forEach(function (a) {
      a.el.textContent = " (" + relative(a.date) + ")";
    });
  }

  function run(root) {
    root.querySelectorAll("time[datetime]").forEach(localize);
  }

  run(document);
  setInterval(refresh, 60000);
  document.addEventListener("htmx:afterSettle", function () {
    run(document);
  });
})();
