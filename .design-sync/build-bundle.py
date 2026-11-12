"""Turn Foreman's dist/design export into the Claude Design upload layout (CSS-only)."""
import html, json, re, shutil, sys
from pathlib import Path

src = Path(sys.argv[1]); out = Path(sys.argv[2]); header = Path(sys.argv[3])
if out.exists(): shutil.rmtree(out)
(out / "fonts").mkdir(parents=True)
for f in (src / "vendor").iterdir():
    shutil.copy(f, out / "fonts" / f.name)
css = (src / "foreman.css").read_text()
css = css.replace('url("vendor/', 'url("fonts/')
(out / "_ds_bundle.css").write_text(css)
(out / "styles.css").write_text('/* Foreman: tokens, fonts and every component style. */\n@import "./_ds_bundle.css";\n')

cards = []
for f in sorted(src.glob("*.html")):
    text = f.read_text()
    first = text.split("\n", 1)[0]
    group = re.search(r'group="([^"]*)"', first).group(1)
    name = f.stem.capitalize()
    title = re.search(r"<h2[^>]*>(.*?)</h2>", text).group(1)
    d = out / "components" / group.lower() / name
    d.mkdir(parents=True)
    (d / f"{name}.html").write_text(text.replace('href="foreman.css"', 'href="../../../styles.css"'))
    classes = sorted({c for a in re.findall(r'class="([^"]*)"', text) for c in a.split() if not c.startswith("design-")})
    tokens = sorted(set(re.findall(r"<code>(--[a-z0-9-]+)</code>", text)))
    # Light pane markup, as the agent's copyable example.
    m = re.search(r'data-theme="light">(.*?)</div>\s*<div class="design-pane" data-theme="dark"', text, re.S)
    example = re.sub(r"<h3>.*?</h3>", "", m.group(1)).strip() if m else ""
    lines = [f"{html.unescape(title)}: Foreman's {title.lower()} as plain HTML with foreman.css classes (no JavaScript component).", "",
             f"Group: {group}. Load `styles.css`; set `color-scheme` or `data-theme=\"light\"|\"dark\"` on an ancestor to pick the theme.", ""]
    if classes: lines += ["Classes shown on this card: " + ", ".join(f"`{c}`" for c in classes) + ".", ""]
    if tokens: lines += ["Tokens shown on this card: " + ", ".join(f"`{t}`" for t in tokens) + ".", ""]
    if example: lines += ["Markup (light pane of the card, copy and adapt; the `<code>` labels are only captions):", "", "```html", example, "```", ""]
    (d / f"{name}.prompt.md").write_text("\n".join(lines))
    cards.append((group, name, title))

idx = ["", "## Cards", ""] + [f"- `components/{g.lower()}/{n}/{n}.html` ({g}): {t}. Usage notes in `{n}.prompt.md`." for g, n, t in cards]
(out / "README.md").write_text(header.read_text().rstrip() + "\n" + "\n".join(idx) + "\n")
(out / "_ds_bundle.js").write_text("/* @ds-bundle: " + json.dumps({"namespace": "Foreman", "components": [], "sourceHashes": {}, "inlinedExternals": []}) + " */\n"
    "// Foreman is server-rendered HTML styled by styles.css; it ships no JavaScript components.\nwindow.Foreman = window.Foreman || {};\n")
(out / ".ds-build-meta.json").write_text(json.dumps({"componentCount": len(cards), "shape": "css-only"}))
print(f"{len(cards)} cards")
