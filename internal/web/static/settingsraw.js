// Puts the caret on the line an error names when its link in the summary is
// followed (SPEC 11.2). The page lists the lines in data-error-lines, one per
// link in the summary and in the same order; 0 means the error names none.
// Without this script the link still moves to the editor, as it did before.
document.addEventListener("DOMContentLoaded", () => {
  const area = document.getElementById("content");
  const summary = document.getElementById("error-summary");
  if (!area || !summary || !area.dataset.errorLines) return;
  const lines = area.dataset.errorLines.split(",").map((n) => parseInt(n, 10) || 0);

  function placeCaret(line) {
    const rows = area.value.split("\n");
    if (line > rows.length) return;
    let at = 0;
    for (let i = 0; i < line - 1; i++) at += rows[i].length + 1;
    area.focus();
    area.setSelectionRange(at, at);
    // Scrolls the line to the middle of the box; a textarea does not follow
    // the caret by itself when it is set from a script.
    const share = (line - 1) / rows.length;
    area.scrollTop = Math.max(0, share * area.scrollHeight - area.clientHeight / 2);
  }

  summary.querySelectorAll("a").forEach((link, i) => {
    if (!lines[i]) return;
    link.addEventListener("click", (e) => {
      e.preventDefault();
      placeCaret(lines[i]);
    });
  });
});
