# Design sync notes (Foreman)

- Shape is CSS-only (Todd's choice, 2026-11-12). Foreman has no JavaScript
  components, so the skill's converter does not apply. The upload layout is
  built from `make design-export` output (dist/design) by a small script; the
  bundle header lists no components and `window.Foreman` is empty.
- No `_ds_sync.json` anchor is written (off-script layout), so every sync
  re-checks all cards. That is 11 cards and takes a minute.
- `[FONT_MISSING]` for "Microsoft YaHei" and "Cascadia Mono" is expected:
  they are fallbacks in the font stacks, not Foreman's fonts (IBM Plex ships).
- The render-check contact sheet crops the dark half of each card; the full
  screenshots at 1200 px show both themes. Not a defect.

## Re-sync steps

1. `make design-export` (writes dist/design).
2. `python3 .design-sync/build-bundle.py dist/design ds-bundle .design-sync/conventions.md`
   (writes the upload layout to ds-bundle/, which git ignores).
3. Run the skill's `package-validate.mjs ds-bundle` with Playwright reachable
   (link e2e/node_modules next to the staged script), then upload.
