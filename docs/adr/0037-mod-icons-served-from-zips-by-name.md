# 0037. Mod icons are served from the world's mod zips, by object name

- **Status**: Accepted
- **Date**: 2026-11-23
- **References**: SPEC.md sections 5.6, 14.5, 14.6 and 14.10; issue #914;
  amends ADR 0035

## Context

ADR 0035 allowed images only from Foreman, plus the Factorio wiki as an
admin's opt-in, and left mod icons to a later change. A mod's icon is a file
inside the mod's zip, in a path the mod chose. Serving a file by a path in
the address would hand the browser a way to name any file in a zip, and the
file's bytes could be anything with a `.png` name.

## Decision

- A mod icon is asked for by the object, never by a path:
  `/worlds/{id}/icons/{kind}/{name}.png`, for a signed-in user. The kind is
  on a fixed list and the name matches the prototype-name rule.
- The server looks the object up in the world's cached mod settings run
  (ADR 0036), finds the mod, its version and the path there, checks the path
  again, and reads that one entry from the installed zip without unpacking
  it. The mod must be listed as active, at the zip's exact version. It
  starts no run: no cached run is a 404 and the page shows the name.
- Reads are limited (SPEC 14.6): a size cap on the entry and on the picture,
  one decode at a time with a short queue, and an 8 MiB cache.
- The answer is a freshly encoded, scaled PNG of the first mipmap, never the
  zip's bytes.
- Base-game art is never shipped, since Wube's terms forbid it, and icon
  paths under the game's own mods are refused at export and again at read.
  A base-game object gets the wiki's picture only while the wiki setting is
  on (ADR 0035).
- For an object tag the order is the mod's icon, the wiki's icon, then the
  name. Pages send `Referrer-Policy: no-referrer` while the wiki setting is
  on, because the star map's wiki pictures are SVG images, which cannot
  carry a referrer policy of their own.

## Consequences

`img-src` stays `'self' data:` (plus the wiki while on); a mod icon is not
an outbound request. The address cannot be used to read other files, and a
crafted PNG cannot reach the browser as it was stored. Only plain icons
show; layered ones show the name. Folder mods and 1.1 worlds get no icons.
A browser may show an old icon for up to 5 minutes after the mods change.

Rejected: serving by path with a check (the path would be user-chosen
input to the zip); unpacking mods to disk first (disk use and a stale copy
to manage); passing the zip's bytes through (a polyglot file could reach
the browser); and drawing every layer in Go (about twice the code and more
decodes per icon; layered icons are a known gap).
