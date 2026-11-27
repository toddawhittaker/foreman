// Text for the page scripts from the page's #i18n block (layout.html): a
// plural entry is picked by the page's language, {name} placeholders are
// filled in, and numbers are written the language's way.
(() => {
  "use strict";

  const lang = document.documentElement.lang;
  let strings = null;
  window.foremanText = (key, args = {}) => {
    strings ??= JSON.parse(document.getElementById("i18n").textContent);
    let text = strings[key];
    if (typeof text === "object") text = text[new Intl.PluralRules(lang).select(args.count)] ?? text.other;
    return text.replace(/\{(\w+)\}/g, (_, name) => (typeof args[name] === "number" ? args[name].toLocaleString(lang) : args[name]));
  };
})();
