// The note that an http:// webhook address sends its messages unencrypted
// (DESIGN 8.16). It shows while the typed address starts with http://, and
// joins the field's description so a screen reader hears it. Without script
// the note shows only on the edit page of an http:// webhook.
(() => {
  "use strict";
  const field = document.getElementById("webhook-url");
  const note = document.getElementById("webhook-url-plain");
  if (!field || !note) return;
  // An empty field keeps the stored address, so the note then says what the page first said.
  const kept = !note.hidden;
  const update = () => {
    const typed = field.value.trim();
    const plain = typed === "" ? kept : /^http:\/\//i.test(typed);
    note.hidden = !plain;
    const ids = (field.getAttribute("aria-describedby") || "").split(" ").filter((id) => id && id !== note.id);
    if (plain) ids.push(note.id);
    if (ids.length) field.setAttribute("aria-describedby", ids.join(" "));
    else field.removeAttribute("aria-describedby");
  };
  field.addEventListener("input", update);
})();
