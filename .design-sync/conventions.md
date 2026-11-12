# Foreman design system: how to build with it

Foreman is a self-hosted admin app for Factorio game servers. Its UI is
server-rendered HTML styled by one stylesheet. There are **no JavaScript
components**: `window.Foreman` is empty. Build plain semantic HTML and give it
Foreman's classes; never invent class names or restyle with new colours.

## Setup

- Load `styles.css` (it imports all tokens, the IBM Plex fonts and every component style).
- Theme: colours are `light-dark()` pairs, so `color-scheme` picks the theme.
  Force one with `data-theme="light"` or `data-theme="dark"` on `<html>` or any container.
- Put page content in `<main>`; group content in `<section class="panel">` with an `<h2>`.

## Vocabulary (real classes, BEM-style `block--modifier`)

| Need | Classes |
|---|---|
| Buttons | `button`, `button--primary` (one per page, Factorio orange), `button--quiet`, `button--risky`, `button--danger`, `button--icon` (label in `visually-hidden`); busy: `aria-busy="true"`; off: `disabled` |
| Server state badge | `state` + `state--running`, `--starting`, `--stopping`, `--crashed`, `--crash-looping`, `--unknown`; stopped is plain `state` (markup also adds `state--stopped`) |
| Cards | `panel card`; status card: `card status-card status-card--running` (same suffixes as `state`); key-value list: `<dl class="facts facts--inline">` |
| Forms | `<div class="field">` with `<label>`, `<p class="hint">`, the control, `<p class="error">`; required: `label.required` + `<span class="req">*</span>`; checkbox row: `check` |
| Messages | `message` + `message--success`, `--note`, `--warning`, `--error`; confirm block: `confirm` with `actions` |
| Navigation | `<ul class="tabs">` of links, current one `aria-current="true"`; section nav: `<nav class="subnav">` with `aria-current="page"` |
| Tables, empty states | numeric cells `num`; `<p class="empty">` |
| Icons | `<span class="design-icon" style="--icon: var(--i-start)" aria-hidden="true">`; names: server, worlds, snapshot, console, start, stop, restart, download, upload, trash, settings, mods, map, history, search, plus and others on the Icons card |
| Utilities | `visually-hidden`, `mono`, `nowrap` |

## Tokens

Use `var(--*)` for anything custom: colours `--color-bg`, `--color-surface`,
`--color-raised`, `--color-text`, `--color-text-muted`, `--color-heading`,
`--color-border`, `--color-line`, `--color-primary` (orange), `--color-ok`,
`--color-warn`, `--color-danger`, `--color-info` (each with a `-bg` partner except
primary); spacing `--space-1` to `--space-6`, `--card-pad`; type `--font-sans`,
`--font-mono`, `--text-xs` to `--text-2xl`; shape `--radius`, `--radius-lg`,
`--shadow-card`, `--focus-width`; touch size `--target-min` (44 px).

## Where the truth is

`_ds_bundle.css` is the full stylesheet. Each card under `components/` shows
every state in both themes; its `.prompt.md` has copyable markup.

## Example

```html
<section class="panel card" aria-labelledby="srv">
  <h2 id="srv">Server</h2>
  <p><span class="state state--running">Running</span></p>
  <dl class="facts facts--inline">
    <div><dt>Game version</dt><dd><code>2.1.24</code></dd></div>
  </dl>
  <div class="actions">
    <button type="button" class="button">Take snapshot</button>
    <button type="button" class="button button--risky">Stop</button>
  </div>
</section>
```
