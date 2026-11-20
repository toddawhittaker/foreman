// Game settings' sub-pages share one draft (DESIGN 6): edits made on one
// sub-page are kept in this tab's session storage and put back on the next,
// so one Preview changes covers every section. The foot bar counts them and
// the page column marks the sub-pages that hold them. A save or Cancel
// drops the draft; without script each sub-page saves on its own as before.
(() => {
  "use strict";
  const form = document.querySelector("form.settings[data-draft]");
  if (!form) return;
  const key = "foreman.settings-draft:" + new URL(form.action, location.href).pathname;
  const note = document.getElementById("settings-unsaved");
  const strings = JSON.parse(document.getElementById("i18n").textContent);
  const sectionOf = {
    "settings-listing": "listing", "settings-joining": "players", "settings-gameplay": "gameplay",
    "settings-saving": "saving", "settings-network": "network", "settings-admins": "admins",
    "settings-bans": "bans", "settings-whitelist": "whitelist",
  };

  const controls = () => [...form.elements].filter((el) =>
    el.id && el.name && !el.disabled && !["hidden", "submit", "button", "reset", "file"].includes(el.type));
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
      const box = el.closest("form.settings > fieldset[id]");
      if (box && sectionOf[box.id]) sections.add(sectionOf[box.id]);
    }
    const count = Object.keys(draft).length;
    if (count) sessionStorage.setItem(key, JSON.stringify(draft));
    else sessionStorage.removeItem(key);
    if (note) {
      note.hidden = count === 0;
      if (count) {
        const lang = document.documentElement.lang;
        const text = strings["js.settings.unsaved"];
        const form = text[new Intl.PluralRules(lang).select(count)] ?? text.other;
        note.textContent = form.replace("{count}", count.toLocaleString(lang));
      }
    }
    for (const a of document.querySelectorAll(".subnav__sub a")) {
      const s = new URL(a.href, location.href).searchParams.get("section") || "listing";
      a.toggleAttribute("data-unsaved", sections.has(s));
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
