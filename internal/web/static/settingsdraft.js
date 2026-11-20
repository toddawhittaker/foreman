// Game settings' sub-pages share one draft (DESIGN 6): edits made on one
// sub-page are kept in this tab's session storage and put back on the next,
// so one Preview changes covers every section. The foot bar counts them and
// the page column marks the sub-pages that hold them. A save or Cancel
// drops the draft; without script each sub-page saves on its own as before.
// The draft is kept per person, and a password is never put in it (SPEC 11.4).
(() => {
  "use strict";
  const form = document.querySelector("form.settings[data-draft]");
  if (!form) return;
  const key = "foreman.settings-draft:" + form.dataset.draft + ":" + new URL(form.action, location.href).pathname;
  const note = document.getElementById("settings-unsaved");
  const strings = JSON.parse(document.getElementById("i18n").textContent);

  const controls = () => [...form.elements].filter((el) =>
    el.id && el.name && !el.disabled && !["hidden", "submit", "button", "reset", "file", "password"].includes(el.type));
  const changed = (el) => {
    if (el.type === "checkbox" || el.type === "radio") return el.checked !== el.defaultChecked;
    if (el.tagName === "SELECT") return [...el.options].some((o) => o.selected !== o.defaultSelected);
    return el.value !== el.defaultValue;
  };
  const read = () => {
    try { return JSON.parse(sessionStorage.getItem(key) || "{}"); } catch { return {}; }
  };

  // Put back what was typed on another sub-page.
  const saved = read();
  for (const el of controls()) {
    if (!(el.id in saved)) continue;
    const v = saved[el.id];
    if (el.type === "checkbox" || el.type === "radio") el.checked = v;
    else el.value = v;
    el.dispatchEvent(new Event("input", { bubbles: true }));
    el.dispatchEvent(new Event("change", { bubbles: true }));
  }

  const update = () => {
    const draft = {};
    const sections = new Set();
    for (const el of controls()) {
      if (!changed(el)) continue;
      draft[el.id] = el.type === "checkbox" || el.type === "radio" ? el.checked : el.value;
      const box = el.closest("form.settings > fieldset[data-section]");
      if (box) sections.add(box.dataset.section);
    }
    const count = Object.keys(draft).length;
    if (count) sessionStorage.setItem(key, JSON.stringify(draft));
    else sessionStorage.removeItem(key);
    if (note) {
      // The note is a status region: it stays in the page and is written only
      // when its words change, so a screen reader hears each change once.
      let text = "";
      if (count) {
        const lang = document.documentElement.lang;
        const forms = strings["js.settings.unsaved"];
        const form = forms[new Intl.PluralRules(lang).select(count)] ?? forms.other;
        text = form.replace("{count}", count.toLocaleString(lang));
      }
      if (note.textContent !== text) note.textContent = text;
    }
    for (const a of document.querySelectorAll(".subnav__sub a[data-section]")) {
      const has = sections.has(a.dataset.section);
      a.toggleAttribute("data-unsaved", has);
      const words = a.querySelector("[data-unsaved-note]");
      if (words) words.hidden = !has;
    }
  };

  form.addEventListener("input", update);
  form.addEventListener("change", update);
  form.addEventListener("submit", () => sessionStorage.removeItem(key));
  for (const a of form.querySelectorAll("[data-draft-drop]")) {
    a.addEventListener("click", () => sessionStorage.removeItem(key));
  }
  update();
})();
