// Keeps the import forms in step with what is chosen: suggests each form's
// world name from the chosen folder or file, names the save's version in
// "Same as the save", sets Space Age from the folder's save, and warns when
// the Space Age box differs from the save. Anything the admin has typed or
// ticked is left alone. With several folders ticked the per-world fields do
// not apply, so they are turned off (SPEC 5.5).
document.addEventListener("DOMContentLoaded", () => {
  // Reads the change out politely, emptying the region first so a repeated
  // sentence is still read.
  function announce(said) {
    const announcer = document.getElementById("announcer");
    if (!announcer) return;
    announcer.textContent = "";
    setTimeout(() => { announcer.textContent = said; }, 50);
  }

  // Keeps a name field filled with suggestions until the admin types in it,
  // and returns the sentence to read out, or "".
  // suggested is what this script, or the server, last put there.
  function nameSuggester(field, suggested) {
    if (!field) return () => "";
    let edited = field.value !== "" && field.value !== suggested;
    field.addEventListener("input", () => { edited = true; });
    return (name) => {
      if (edited || field.value === name) return "";
      field.value = name;
      return "World name suggested: " + name + ".";
    };
  }

  const boxes = Array.from(document.querySelectorAll('input[name="folder"]'));
  const ticked = () => boxes.filter((b) => b.checked);
  const first = ticked()[0];
  const suggestFolderName = nameSuggester(document.getElementById("folder-name"), first ? first.dataset.name : "");
  const suggestUploadName = nameSuggester(document.getElementById("upload-name"), "");

  const file = document.getElementById("upload-source");
  if (file) {
    file.addEventListener("change", () => {
      if (file.files.length !== 1) return;
      const said = suggestUploadName(file.files[0].name.replace(/\.zip$/i, ""));
      if (said) announce(said);
    });
  }

  const name = document.getElementById("folder-name");
  const select = document.getElementById("folder-version-select");
  const typed = document.getElementById("folder-version");
  const same = document.getElementById("folder-version-same");
  const spaceAge = document.getElementById("folder-space-age");
  const warning = document.getElementById("folder-space-age-warning");
  if (!spaceAge) return;
  let spaceAgeEdited = false;
  spaceAge.addEventListener("change", () => { spaceAgeEdited = true; });

  // The one ticked folder, or null.
  function only() {
    const t = ticked();
    return t.length === 1 ? t[0] : null;
  }

  // The words match spaceAgeOnWarning and spaceAgeOffWarning in import.go.
  function warningText() {
    const box = only();
    if (!box || !box.dataset.gameVersion) return "";
    const version = (typed && typed.value.trim()) || (select && select.value) || box.dataset.gameVersion;
    if (spaceAge.checked && version.startsWith("1.")) {
      return "Space Age needs Factorio 2.0 or later. Untick Space Age for a Factorio 1.1 world.";
    }
    const saveHas = "spaceAge" in box.dataset;
    if (spaceAge.checked === saveHas) return "";
    return spaceAge.checked
      ? "The save was made without Space Age. Factorio converts it to Space Age when it first starts, and that cannot be undone."
      : "The save was made with Space Age. Without it, Factorio may not load the save, or may remove its Space Age content.";
  }

  // Shows or hides the warning, and returns its words when they changed.
  function showWarning() {
    if (!warning) return "";
    const text = warningText();
    const span = warning.querySelector("span");
    const changed = span.textContent !== text;
    span.textContent = text;
    warning.hidden = text === "";
    return changed && text ? "Warning: " + text : "";
  }

  [spaceAge, typed, select].forEach((el) => {
    if (!el) return;
    el.addEventListener(el === typed ? "input" : "change", () => {
      const said = showWarning();
      if (said) announce(said);
    });
  });

  boxes.forEach((box) => {
    box.addEventListener("change", () => {
      const one = only();
      const many = ticked().length > 1;
      [name, select, typed, spaceAge].forEach((el) => { if (el) el.disabled = many; });
      const said = [];
      if (many) said.push("Several folders chosen: each uses its own name, version and Space Age.");
      if (one) {
        said.push(suggestFolderName(one.dataset.name));
        if (same) {
          same.textContent = one.dataset.gameVersion
            ? "Same as the save (" + one.dataset.gameVersion + ")"
            : "Same as the save (could not be read, so choose or type one)";
        }
        if (!spaceAgeEdited && one.dataset.gameVersion) {
          spaceAge.checked = "spaceAge" in one.dataset;
        }
      } else if (same) {
        same.textContent = "Same as the save";
      }
      said.push(showWarning());
      const text = said.filter((s) => s).join(" ");
      if (text) announce(text);
    });
  });
});
