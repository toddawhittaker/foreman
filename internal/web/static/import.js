// Keeps the import forms in step with what is chosen: suggests each form's
// world name from the chosen folder or file, names the save's version in
// "Same as the save", sets Space Age from the folder's save, and warns when
// the Space Age box differs from the save. Anything the admin has typed or
// ticked is left alone. With several folders ticked the per-world fields do
// not apply, so they are emptied and turned off (SPEC 5.5).
document.addEventListener("DOMContentLoaded", () => {
  // Text from the page's #i18n block, with {name} placeholders filled in.
  const strings = JSON.parse(document.getElementById("i18n").textContent);
  const t = (key, args = {}) => strings[key].replace(/\{(\w+)\}/g, (_, name) => args[name]);

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
      return t("js.import.suggested", { name });
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

  function warningText() {
    const box = only();
    if (!box || !box.dataset.gameVersion) return "";
    const version = (typed && typed.value.trim()) || (select && select.value) || box.dataset.gameVersion;
    if (spaceAge.checked && version.startsWith("1.")) {
      return warning.dataset.warnOld;
    }
    const saveHas = "spaceAge" in box.dataset;
    if (spaceAge.checked === saveHas) return "";
    return spaceAge.checked ? warning.dataset.warnOn : warning.dataset.warnOff;
  }

  // Shows or hides the warning, and returns its words when they changed.
  function showWarning() {
    if (!warning) return "";
    const text = warningText();
    const span = warning.querySelector("span");
    const changed = span.textContent !== text;
    span.textContent = text;
    warning.hidden = text === "";
    return changed && text ? t("js.import.warning", { text }) : "";
  }

  [spaceAge, typed, select].forEach((el) => {
    if (!el) return;
    el.addEventListener(el === typed ? "input" : "change", () => {
      const said = showWarning();
      if (said) announce(said);
    });
  });

  // With several folders ticked the per-world fields do not apply: they are
  // emptied, unticked and disabled, and point at the hint that says why. What
  // was in them is put back when one folder is ticked again.
  const manyHint = document.getElementById("folder-many-hint");
  const fields = [name, select, typed, spaceAge].filter((el) => el);
  const describedBy = new Map(fields.map((el) => [el, el.getAttribute("aria-describedby")]));
  let stashed = null;
  function applyMany(many) {
    fields.forEach((el) => {
      el.disabled = many;
      const ids = [describedBy.get(el), many && manyHint ? manyHint.id : null].filter((id) => id).join(" ");
      if (ids) el.setAttribute("aria-describedby", ids);
      else el.removeAttribute("aria-describedby");
    });
    if (many && !stashed) {
      stashed = {
        name: name ? name.value : "",
        version: typed ? typed.value : "",
        choice: select ? select.value : "",
        spaceAge: spaceAge.checked,
      };
      if (name) name.value = "";
      if (typed) typed.value = "";
      if (select) select.value = "";
      spaceAge.checked = false;
    } else if (!many && stashed) {
      if (name) name.value = stashed.name;
      if (typed) typed.value = stashed.version;
      if (select) select.value = stashed.choice;
      spaceAge.checked = stashed.spaceAge;
      stashed = null;
    }
  }
  applyMany(ticked().length > 1);

  boxes.forEach((box) => {
    box.addEventListener("change", () => {
      const one = only();
      const many = ticked().length > 1;
      applyMany(many);
      const said = [];
      if (many) said.push(t("js.import.many"));
      if (one) {
        said.push(suggestFolderName(one.dataset.name));
        if (same) {
          same.textContent = one.dataset.gameVersion
            ? t("js.import.same.version", { version: one.dataset.gameVersion })
            : t("js.import.same.unreadable");
        }
        if (!spaceAgeEdited && one.dataset.gameVersion) {
          spaceAge.checked = "spaceAge" in one.dataset;
        }
      } else if (same) {
        same.textContent = t("js.import.same");
      }
      said.push(showWarning());
      const text = said.filter((s) => s).join(" ");
      if (text) announce(text);
    });
  });
});
