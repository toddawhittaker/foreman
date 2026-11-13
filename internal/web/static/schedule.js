// A schedule that was never saved starts on the viewer's own time zone, not
// UTC (DESIGN 8.15). The server cannot know the browser's zone, so the page
// marks the select and this picks the zone when the list has it. A saved or
// refused form is not marked and keeps its zone; without script UTC stays.
(() => {
  "use strict";
  const select = document.getElementById("schedule-zone");
  if (!select || !select.hasAttribute("data-viewer-zone")) return;
  let zone = "";
  try {
    zone = Intl.DateTimeFormat().resolvedOptions().timeZone || "";
  } catch (e) {
    return;
  }
  const option = Array.from(select.options).find((o) => o.value === zone);
  if (option) select.value = option.value;
})();
