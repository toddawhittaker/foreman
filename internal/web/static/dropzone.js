// Makes each [data-dropzone] field also a drop target (DESIGN 7.10). The
// native file input stays the control: it is stretched over the zone, so a
// click or Enter or Space opens the picker as before. A wrong type, a file
// over the limit or several files are refused here before uploading; the
// server still checks everything (SPEC 14.6). Without script the field is
// the plain labelled file input.
document.addEventListener("DOMContentLoaded", () => {
  const zones = () => document.querySelectorAll("[data-dropzone]");

  function announce(said) {
    const announcer = document.getElementById("announcer");
    if (!announcer) return;
    announcer.textContent = "";
    setTimeout(() => { announcer.textContent = said; }, 50);
  }

  // Text from the page's #i18n block, with {name} placeholders filled in.
  const strings = JSON.parse(document.getElementById("i18n").textContent);
  const t = (key, args) => strings[key].replace(/\{(\w+)\}/g, (_, k) => args[k] ?? "");

  // A size in the server's words and 1024-based units, as the limits are: "512 B", "1.5 MB".
  function size(n) {
    const units = ["common.bytes.b", "common.bytes.kb", "common.bytes.mb", "common.bytes.gb"];
    let i = 0;
    while (n >= 1024 && i < units.length - 1) { n /= 1024; i++; }
    const digits = i ? 1 : 0;
    const num = new Intl.NumberFormat(document.documentElement.lang || undefined,
      { minimumFractionDigits: digits, maximumFractionDigits: digits, useGrouping: false }).format(n);
    return t(units[i], { n: num });
  }

  // A drag anywhere on the page marks every zone, not by colour alone: the
  // border and the prompt's words change.
  let depth = 0;
  const marking = (on) => zones().forEach((z) => z.classList.toggle("dropzone--page", on));
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

  function enhance(zone) {
    if (zone.classList.contains("dropzone--on")) return;
    const input = zone.querySelector('input[type="file"]');
    const prompt = zone.querySelector(".dropzone__prompt");
    const chosen = zone.querySelector(".dropzone__chosen");
    const fileText = zone.querySelector(".dropzone__file");
    const clear = zone.querySelector(".dropzone__clear");
    const error = document.getElementById(input.id + "-client-error");
    const accept = zone.dataset.accept.toLowerCase();
    const serverError = document.getElementById(input.id + "-error");
    let describedBy = input.getAttribute("aria-describedby") || "";
    let serverInvalid = input.getAttribute("aria-invalid") === "true";

    // A new file answers the server's last refusal, so its error goes.
    function dropServerError() {
      if (!serverError) return;
      serverError.hidden = true;
      describedBy = describedBy.split(" ").filter((id) => id !== serverError.id).join(" ");
      serverInvalid = false;
    }
    zone.classList.add("dropzone--on");
    prompt.hidden = false;

    // The words that refuse these files, or "".
    function refusal(files) {
      if (files.length !== 1) return zone.dataset.single;
      const f = files[0];
      if (!f.name.toLowerCase().endsWith(accept)) return t("common.dropzone.wrong_type", { file: f.name, types: zone.dataset.accept });
      if (f.size > Number(zone.dataset.maxBytes)) return t("common.dropzone.too_big", { file: f.name, limit: zone.dataset.limit });
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
      fileText.textContent = f ? t("common.dropzone.chosen", { file: f.name, size: size(f.size) }) : "";
    }

    // Capture runs before the page's own listeners, so a refused file never
    // reaches them.
    zone.addEventListener("change", (e) => {
      if (e.target !== input) return;
      if (input.files.length) dropServerError();
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
      dropServerError();
      const said = refusal(e.dataTransfer.files);
      if (said) {
        // An earlier good choice must never upload behind a refusal.
        input.value = "";
        showChosen();
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
  }

  zones().forEach(enhance);
  // A redraw can bring a zone in after the page loads, as a plan does for a mod's zip.
  document.addEventListener("htmx:load", () => zones().forEach(enhance));
});
