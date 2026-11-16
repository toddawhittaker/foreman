// Makes each [data-dropzone] field also a drop target (DESIGN 7.10). The
// native file input stays the control: it is stretched over the zone, so a
// click or Enter or Space opens the picker as before. A wrong type, a file
// over the limit or several files are refused here before uploading; the
// server still checks everything (SPEC 14.6). Without script the field is
// the plain labelled file input.
document.addEventListener("DOMContentLoaded", () => {
  const zones = Array.from(document.querySelectorAll("[data-dropzone]"));
  if (zones.length === 0) return;

  function announce(said) {
    const announcer = document.getElementById("announcer");
    if (!announcer) return;
    announcer.textContent = "";
    setTimeout(() => { announcer.textContent = said; }, 50);
  }

  const fill = (text, args) => text.replace(/\{(\w+)\}/g, (_, k) => args[k] ?? "");

  // A size in the server's words and 1000-based units: "512 B", "1.5 MB".
  function size(zone, n) {
    const units = zone.dataset.units.split("|");
    let i = 0;
    while (n >= 1000 && i < units.length - 1) { n /= 1000; i++; }
    const digits = i ? 1 : 0;
    const num = new Intl.NumberFormat(document.documentElement.lang || undefined,
      { minimumFractionDigits: digits, maximumFractionDigits: digits, useGrouping: false }).format(n);
    return fill(units[i], { n: num });
  }

  // A drag anywhere on the page marks every zone, not by colour alone: the
  // border and the prompt's words change.
  let depth = 0;
  const marking = (on) => zones.forEach((z) => z.classList.toggle("dropzone--page", on));
  const hasFiles = (e) => e.dataTransfer && Array.from(e.dataTransfer.types).some((type) => type.toLowerCase() === "files");
  document.addEventListener("dragenter", (e) => { if (hasFiles(e)) { depth++; marking(true); } });
  document.addEventListener("dragleave", (e) => { if (hasFiles(e) && --depth <= 0) { depth = 0; marking(false); } });
  // A file dropped beside a zone must not open in place of the page.
  document.addEventListener("dragover", (e) => { if (hasFiles(e)) e.preventDefault(); });
  document.addEventListener("drop", (e) => {
    depth = 0;
    marking(false);
    if (hasFiles(e)) e.preventDefault();
  });

  zones.forEach((zone) => {
    const input = zone.querySelector('input[type="file"]');
    const prompt = zone.querySelector(".dropzone__prompt");
    const chosen = zone.querySelector(".dropzone__chosen");
    const fileText = zone.querySelector(".dropzone__file");
    const clear = zone.querySelector(".dropzone__clear");
    const error = document.getElementById(input.id + "-client-error");
    const accept = zone.dataset.accept.toLowerCase();
    const describedBy = input.getAttribute("aria-describedby") || "";
    const serverInvalid = input.getAttribute("aria-invalid") === "true";
    zone.classList.add("dropzone--on");
    prompt.hidden = false;

    // The words that refuse these files, or "".
    function refusal(files) {
      if (files.length !== 1) return zone.dataset.single;
      const f = files[0];
      if (!f.name.toLowerCase().endsWith(accept)) return fill(zone.dataset.wrongType, { file: f.name });
      if (f.size > Number(zone.dataset.maxBytes)) return fill(zone.dataset.tooBig, { file: f.name });
      return "";
    }

    function showError(text) {
      error.hidden = text === "";
      error.querySelector("span").textContent = text;
      const ids = [describedBy, text ? error.id : ""].filter((s) => s).join(" ");
      input.setAttribute("aria-describedby", ids);
      if (text || serverInvalid) input.setAttribute("aria-invalid", "true");
      else input.removeAttribute("aria-invalid");
      if (text) announce(text);
    }

    function showChosen() {
      const f = input.files[0];
      chosen.hidden = !f;
      zone.classList.toggle("dropzone--chosen", !!f);
      fileText.textContent = f ? fill(zone.dataset.chosen, { file: f.name, size: size(zone, f.size) }) : "";
    }

    // Capture runs before the page's own listeners, so a refused file never
    // reaches them.
    zone.addEventListener("change", (e) => {
      if (e.target !== input) return;
      const said = input.files.length ? refusal(input.files) : "";
      if (said) {
        e.stopPropagation();
        input.value = "";
      }
      showError(said);
      showChosen();
    }, true);

    zone.addEventListener("dragover", (e) => {
      if (!hasFiles(e)) return;
      e.preventDefault();
      zone.classList.add("dropzone--over");
    });
    zone.addEventListener("dragleave", (e) => {
      if (!zone.contains(e.relatedTarget)) zone.classList.remove("dropzone--over");
    });
    zone.addEventListener("drop", (e) => {
      zone.classList.remove("dropzone--over");
      if (!hasFiles(e)) return;
      e.preventDefault();
      e.stopPropagation();
      depth = 0;
      marking(false);
      const said = refusal(e.dataTransfer.files);
      if (said) {
        showError(said);
        return;
      }
      input.files = e.dataTransfer.files;
      input.dispatchEvent(new Event("change", { bubbles: true }));
      input.focus();
    });

    clear.addEventListener("click", () => {
      input.value = "";
      input.dispatchEvent(new Event("change", { bubbles: true }));
      input.focus();
      announce(zone.dataset.cleared);
    });
  });
});
