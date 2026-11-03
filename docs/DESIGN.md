# Foreman: design

This document sets the type, colour, spacing, focus style and components
that every Foreman screen uses (SPEC 17.6). It covers what the screens of
Epics 1 to 3 need: sign-in and setup, the world list, saves and
snapshots, server state, the log and the console, and confirmations for
risky actions. Section 12 lists what is deliberately left for later.

The CSS is hand-written, with no framework and no build step (STACK 3).
Nothing loads from outside the binary (SPEC 3.7), so there are no web
fonts, no icon fonts and no CDN.

When this document and a screen disagree, change the screen. When this
document is wrong, change it in the same pull request and say why.

## 1. Character

Foreman is a tool for keeping a game server alive, used in a hurry, often
from a phone. It should look like a well-labelled control panel, not a
marketing page.

- **Warm grey and one amber.** Neutrals are warm greys, like painted
  steel. The single accent is an amber taken from the game's hazard
  colour. It marks the one main action on a screen and links; it never
  decorates.
- **A dark header bar in both themes**, with a thin amber rule under it.
  That is the only branding. There is no logo, no hero and no
  illustration.
- **Flat.** No gradients, glows, blurred glass or shadows. Edges come from
  1 px borders. Corners are 3 px, which is enough to soften a button
  without turning it into a pill.
- **Monospace for what the game prints**: versions, file names, world
  ids, seeds, the console and the log. Everything a person wrote, or
  Foreman wrote for a person, is in the system sans-serif.
- **Dense, not cramped.** Lists and tables, not grids of cards. A world
  list is a list. A save list is a table.
- **Words, not icons.** No emoji and no icon set. State, actions and
  messages are words. Where a shape helps scanning (the state badge) it
  is drawn in CSS and always sits beside its word.

## 2. Type

### Font stacks

```css
--font-sans: system-ui, -apple-system, "Segoe UI", Roboto, "Noto Sans",
  "Liberation Sans", Arial, sans-serif;
--font-mono: ui-monospace, "SF Mono", "Cascadia Mono", "Segoe UI Mono",
  "DejaVu Sans Mono", "Liberation Mono", Menlo, Consolas, monospace;
```

These are the fonts the visitor's system already has. Nothing is
downloaded.

### Scale

The root size is left at the browser default (normally 16 px) so that a
person's own text-size setting works. Every size is in `rem`. The steps
are a ratio of about 1.125, which keeps headings close to body text: on a
320 px screen, big headings waste lines.

| Token | Size | Line height | Weight | Use |
|---|---|---|---|---|
| `--text-sm` | 0.875rem (14 px) | 1.45 | 400 | Hints, table captions, timestamps, the log and console |
| `--text-md` | 1rem (16 px) | 1.5 | 400 | Body, form controls, buttons, table cells |
| `--text-lg` | 1.125rem (18 px) | 1.35 | 600 | `h3`, the world name in a list item |
| `--text-xl` | 1.25rem (20 px) | 1.3 | 600 | `h2`, section headings |
| `--text-2xl` | 1.5rem (24 px) | 1.25 | 650 | `h1`, one per page |

Rules:

- Nothing is smaller than 14 px. Form controls are 16 px so phones do not
  zoom in when a field is focused.
- Body text is limited to `max-inline-size: 70ch`. Tables and the log are
  not.
- Numbers that line up in columns (sizes, times, uptime, player counts)
  use `font-variant-numeric: tabular-nums`.
- Monospace text is set at `--text-sm`, because monospace faces look
  larger than sans at the same size.
- Long unbroken strings (world ids, file names, 60-character world names,
  log lines) use `overflow-wrap: anywhere` so they wrap instead of
  pushing the page wider than 320 px.
- Copy is sentence case. Buttons start with a verb ("Start server",
  "Delete world", "Take snapshot"), never "Submit" or "OK".
- Headings never skip a level. Each page has exactly one `h1`.

## 3. Colour

### How themes work

Both themes are defined as CSS custom properties. The light values sit on
`:root`; the dark values replace them inside
`@media (prefers-color-scheme: dark)`. `:root` also sets
`color-scheme: light dark` so native controls (checkboxes, scroll bars,
the file picker) follow the theme. There is no theme switch in the
interface; the system setting decides (SPEC 17.3).

Every colour in the CSS comes from a token below. A raw hex value
anywhere else in the stylesheet is a bug.

### Tokens

| Token | Light | Dark | Use |
|---|---|---|---|
| `--color-bg` | `#f5f4f0` | `#161513` | Page background |
| `--color-surface` | `#ffffff` | `#201f1c` | Form fields, secondary buttons, table body, dialogs |
| `--color-sunken` | `#ebe9e4` | `#0e0d0c` | Log and console, the "stopped" badge, secondary button hover |
| `--color-text` | `#1d1c1a` | `#ebe8e1` | Body text, headings, secondary button text |
| `--color-text-muted` | `#57544e` | `#aaa59c` | Hints, timestamps, table captions, the "stopped" badge |
| `--color-border` | `#7c7870` | `#87827a` | Borders of fields and secondary buttons, table header rule |
| `--color-rule` | `#d6d3cc` | `#34322e` | Decorative dividers between rows; never the only edge of a control |
| `--color-link` | `#9a4300` | `#f5aa5c` | Links (always underlined) |
| `--color-accent` | `#a84a00` | `#eba13c` | Primary button background |
| `--color-accent-hover` | `#8c3d00` | `#f2b866` | Primary button hover |
| `--color-on-accent` | `#ffffff` | `#1a1105` | Primary button text |
| `--color-danger` | `#b0261c` | `#f4848e` | Danger button background, "crash-looping" badge background |
| `--color-danger-hover` | `#921d15` | `#f8a3ab` | Danger button hover |
| `--color-on-danger` | `#ffffff` | `#1f0604` | Danger button text, "crash-looping" badge text |
| `--color-danger-text` | `#a3231a` | `#ff9d93` | Error words, field error text, invalid field border, "crashed" badge |
| `--color-danger-bg` | `#fbe6e3` | `#3a1714` | Error message and "crashed" badge background, confirmation box |
| `--color-ok` | `#1d6a33` | `#74d08c` | "Done" word, "running" badge |
| `--color-ok-bg` | `#e3f1e6` | `#16301d` | Success message and "running" badge background |
| `--color-warn` | `#7f5300` | `#e9bb52` | "Warning" word, "starting" and "stopping" badges |
| `--color-warn-bg` | `#f8eed9` | `#33280f` | Warning message and "starting"/"stopping" badge background |
| `--color-info` | `#174ea6` | `#9cc2ff` | "Note" word |
| `--color-info-bg` | `#e6eefb` | `#14243d` | Notice background |
| `--color-focus` | `#0a56c8` | `#8ab4ff` | Focus outline |
| `--color-header-bg` | `#1d1c1a` | `#0e0d0c` | Header bar |
| `--color-header-text` | `#f5f4f0` | `#ebe8e1` | Header links and product name |
| `--color-header-muted` | `#b8b3a9` | `#aaa59c` | Signed-in user name in the header |
| `--color-header-rule` | `#d9822b` | `#eba13c` | The 3 px rule under the header |
| `--color-header-focus` | `#8ab4ff` | `#8ab4ff` | Focus outline inside the header |

The focus colour is blue, not amber, on purpose: amber is already the
primary button and links, and red is danger, so a blue ring is never
mistaken for either. Inside the dark header the light theme's blue is
too dark, so the header sets `--color-focus: var(--color-header-focus)`
for its own children.

### Contrast

Every ratio below was computed with the WCAG 2.2 relative-luminance
formula from the hex values above, not estimated. Text needs 4.5:1
(1.4.3, normal-size text, since nothing here relies on the large-text
exception). Control edges, focus outlines and the state badge's drawn
shape need 3:1 against what is next to them (1.4.11).

The script that produced them is short enough to rerun when a token
changes: compute each colour's relative luminance L, then
`(L_lighter + 0.05) / (L_darker + 0.05)`.

**Text pairings (need 4.5:1)**

| Foreground | Background | Light | Dark |
|---|---|---|---|
| `text` | `bg` | 15.47 | 14.91 |
| `text` | `surface` | 17.03 | 13.47 |
| `text` | `sunken` | 14.03 | 15.87 |
| `text-muted` | `bg` | 6.86 | 7.45 |
| `text-muted` | `surface` | 7.54 | 6.73 |
| `text-muted` | `sunken` | 6.22 | 7.93 |
| `link` | `bg` | 6.01 | 9.37 |
| `link` | `surface` | 6.61 | 8.47 |
| `link` | `sunken` | 5.45 | 9.97 |
| `on-accent` | `accent` | 5.76 | 8.61 |
| `on-accent` | `accent-hover` | 7.55 | 10.50 |
| `on-danger` | `danger` | 6.69 | 7.86 |
| `on-danger` | `danger-hover` | 8.72 | 9.96 |
| `danger-text` | `bg` | 6.78 | 9.12 |
| `danger-text` | `surface` | 7.47 | 8.24 |
| `danger-text` | `danger-bg` | 6.23 | 8.00 |
| `ok` | `bg` | 6.04 | 9.68 |
| `ok` | `surface` | 6.64 | 8.75 |
| `ok` | `ok-bg` | 5.69 | 7.56 |
| `warn` | `bg` | 6.08 | 10.16 |
| `warn` | `surface` | 6.69 | 9.18 |
| `warn` | `warn-bg` | 5.80 | 8.06 |
| `info` | `info-bg` | 6.72 | 8.58 |
| `text` | `danger-bg` | 14.21 | 13.08 |
| `text` | `ok-bg` | 14.59 | 11.64 |
| `text` | `warn-bg` | 14.78 | 11.83 |
| `text` | `info-bg` | 14.59 | 12.72 |
| `text-muted` | `ok-bg` | 6.46 | 5.81 |
| `link` | `danger-bg` | 5.52 | 8.22 |
| `link` | `ok-bg` | 5.66 | 7.31 |
| `link` | `warn-bg` | 5.74 | 7.43 |
| `link` | `info-bg` | 5.66 | 7.99 |
| `header-text` | `header-bg` | 15.47 | 15.87 |
| `header-muted` | `header-bg` | 8.16 | 7.93 |

**Control edges and focus (need 3:1)**

| Foreground | Background | Light | Dark |
|---|---|---|---|
| `border` (field, secondary button) | `bg` | 3.99 | 4.78 |
| `border` | `surface` | 4.40 | 4.32 |
| `accent` (primary button edge) | `bg` | 5.24 | 8.43 |
| `accent` | `surface` | 5.76 | 7.61 |
| `danger` (danger button edge) | `bg` | 6.08 | 7.42 |
| `danger` | `surface` | 6.69 | 6.70 |
| `danger-text` (invalid field border) | `surface` | 7.47 | 8.24 |
| `focus` | `bg` | 6.00 | 8.74 |
| `focus` | `surface` | 6.61 | 7.89 |
| `focus` | `sunken` | 5.45 | 9.30 |
| `focus` | `danger-bg` (inside a confirmation) | 5.52 | 7.66 |
| `focus` | `ok-bg` | 5.66 | 6.82 |
| `focus` | `warn-bg` | 5.73 | 6.93 |
| `focus` | `info-bg` | 5.66 | 7.45 |
| `header-focus` | `header-bg` | 8.15 | 9.30 |
| `header-rule` | `header-bg` | 5.82 | 8.96 |

The "crash-looping" badge uses `on-danger` on `danger` (6.69 light, 7.86
dark). `--color-rule` is decorative and has no ratio to meet; it is
never the only thing that shows where a control is.

Not covered by a ratio, on purpose: a disabled control (exempt under
1.4.3, and this design avoids disabled controls, see 7.1), and native
checkboxes and radios, whose edges the browser draws for the
`color-scheme` in use.

## 4. Spacing

A 4 px base, in `rem` so it scales with text size.

| Token | Value | Typical use |
|---|---|---|
| `--space-1` | 0.25rem (4 px) | Gap between a badge's shape and its word |
| `--space-2` | 0.5rem (8 px) | Gap between a label, its hint and its field; table cell padding block |
| `--space-3` | 0.75rem (12 px) | Gap between buttons in a row; table cell padding inline; message padding |
| `--space-4` | 1rem (16 px) | Gap between form fields; page padding at phone width |
| `--space-5` | 1.5rem (24 px) | Gap between sections inside `main` |
| `--space-6` | 2rem (32 px) | Page padding from 40rem up; gap before a page's closing actions |

Other fixed values, also tokens:

| Token | Value | Use |
|---|---|---|
| `--radius` | 3px | Buttons, fields, messages, badges |
| `--border-width` | 1px | Every border except the ones below |
| `--focus-width` | 3px | Focus outline |
| `--target-min` | 2.75rem (44 px) | Minimum block size of buttons and fields |
| `--measure` | 70ch | Maximum line length of prose |
| `--page-max` | 64rem | Maximum width of `main` |

Rules:

- A component never sets its own outer margin. The parent spaces its
  children with `gap` (flex or grid). The one exception is prose flow
  inside a block of text, which uses the default paragraph margins reset
  to `margin-block: 0 var(--space-3)`.
- Use logical properties (`padding-inline`, `margin-block`,
  `inset-inline-start`), never left and right.

## 5. Focus

```css
:focus-visible {
  outline: var(--focus-width) solid var(--color-focus);
  outline-offset: 2px;
}
```

- Every focusable element shows this ring: links, buttons, fields,
  `summary`, and scrollable regions given `tabindex="0"`.
- Never `outline: none` without this replacement. Fields show the ring on
  any focus, because browsers treat text fields as `:focus-visible`
  whether reached by mouse or keyboard.
- The 2 px offset puts the ring on the background around the control,
  which is why the focus ratios in section 3 are measured against
  backgrounds, not against button fills.
- A 3 px ring around a control at least 44 px tall meets WCAG 2.2 focus
  appearance (2.4.13, AAA) as well as focus visible (2.4.7, AA).
- Nothing sticky may cover the focused element (2.4.11). The header is
  not sticky.
- In Windows high-contrast mode (`forced-colors: active`) the outline
  still shows, and every button keeps a 1 px border (transparent in
  normal mode) so its edge shows too.

## 6. Page layout

### Shell

```html
<body>
  <a class="skip" href="#main">Skip to content</a>
  <header class="site">
    <a href="/" class="product">Foreman</a>
    <nav aria-label="Main">
      <ul>
        <li><a href="/" aria-current="page">Server</a></li>
        <li><a href="/worlds">Worlds</a></li>
      </ul>
    </nav>
    <form method="post" action="/signout">
      <span class="user">Signed in as todd</span>
      <button type="submit">Sign out</button>
    </form>
  </header>
  <main id="main" tabindex="-1">
    <h1>…</h1>
    …
  </main>
  <div id="announcer" class="visually-hidden" aria-live="polite"></div>
</body>
```

- The header is a flex row with `flex-wrap: wrap` and `gap`. At 320 px
  the product name sits on the first line, and the nav and the sign-out
  form wrap below it. There is no hamburger menu: Epics 1 to 3 have at
  most four nav items, and a menu would hide them behind an extra tap.
  Revisit only above six items.
- The current page's nav link has `aria-current="page"`, shown as a 2 px
  underline in `--color-header-rule` plus bold weight, so it is not
  colour alone.
- Sign out is a `POST` form with a button, never a link, so a crawler or
  prefetch cannot sign anyone out.
- The skip link is the first focusable element. It is visually hidden
  until focused, then appears at the top-left of the header.
- `main` is a single column, `max-inline-size: var(--page-max)`, centred,
  with `padding-inline: var(--space-4)` at narrow widths and
  `var(--space-6)` from 40rem. Its children are a grid with
  `gap: var(--space-5)`.
- The `#announcer` region is where htmx swaps short results ("Snapshot
  taken", "Server started") so screen readers hear them. Errors are not
  announced from here; see 7.3.
- Viewport media queries are used only for the shell (header and `main`
  padding). Every component lays itself out from its container with
  `container-type: inline-size` and `@container` queries.

### The 320 px rules

These hold for every page (SPEC 17.2), checked in end-to-end tests at a
320 px wide viewport:

1. `document.documentElement.scrollWidth` is not larger than the
   viewport. No element may force horizontal scrolling of the page.
2. Text wraps. Long strings use `overflow-wrap: anywhere`. Flex children
   holding text get `min-width: 0`.
3. Every button and field is at least `--target-min` (44 px) tall and
   44 px wide. Links inside running text are exempt (WCAG 2.5.8 inline
   exception); a link standing alone, such as "Cancel", is padded to 44 px
   tall.
4. Adjacent targets are separated by at least `--space-2`, so two 24 px
   minimum targets can never overlap even when a person's text size
   shrinks things.
5. Button rows wrap (`flex-wrap: wrap`). Each button may grow to the full
   width when its row wraps, so a phone shows them stacked, primary first.
6. Tables follow 8.3. The log and console wrap their lines (8.5); they
   scroll vertically inside their own box, never horizontally.
7. Nothing is fixed or sticky to the viewport, so the on-screen keyboard
   never covers a field.

### Motion

Only `opacity` and `transform` may animate, for at most 150 ms. Under
`prefers-reduced-motion: reduce`, nothing animates, including the
indeterminate progress bar. Nothing on these screens needs motion to be
understood.

## 7. Components

Each component lists its markup first; the styling follows from the
tokens. Classes are short, flat and named for what the thing is (`button`,
`field`, `message`, `state`). There is no utility-class system.

### 7.1 Buttons

```html
<button type="submit" class="button button--primary">Start server</button>
<button type="button" class="button">Take snapshot</button>
<button type="submit" class="button button--danger">Delete world</button>
```

| Kind | Background | Text | Border | Hover | Use |
|---|---|---|---|---|---|
| Primary | `accent` | `on-accent` | 1px `accent` | `accent-hover` | The one main action on a screen or in a form. At most one per form. |
| Secondary | `surface` | `text` | 1px `border` | background `sunken` | Every other action. The default. |
| Danger | `danger` | `on-danger` | 1px `danger` | `danger-hover` | Only the final, confirming button of a destructive action (7.4). |

All buttons: `min-block-size: var(--target-min)`, `min-inline-size:
var(--target-min)`, `padding-inline: var(--space-4)`, `--text-md`,
weight 600, `--radius`, `cursor: pointer`.

- A button that leads to a confirmation step, such as the first "Delete
  world" on a world's page, is **secondary**, not danger. Red is kept for
  the moment the thing actually happens, so it still means something.
- Use `<a class="button">` only for navigation that looks like an action
  ("Upload a save" leading to the upload page). Anything that changes
  state is a `<button>` inside a `<form method="post">`.
- **No disabled buttons to hide a reason.** If an action is not available
  ("Delete world" on the active world, SPEC 5.10), leave the button out
  and say why in text: "This is the active world. Switch to another world
  before deleting it." Disabled buttons cannot be focused and cannot
  explain themselves.
- **While a request runs**, htmx sets `disabled` on the button
  (`hx-disabled-elt="this"`) and Foreman changes its label to the
  present participle ("Starting…"). The button keeps its size, so nothing
  shifts. The busy state is the only time a button is disabled.
- A row of buttons is a flex container with `gap: var(--space-3)` and
  `flex-wrap: wrap`. The primary button comes first in source and visual
  order; the cancel link comes last.

### 7.2 Form fields

```html
<div class="field">
  <label for="world-name">World name</label>
  <p class="hint" id="world-name-hint">Shown in the world list. You can change it later.</p>
  <input id="world-name" name="name" type="text" required
         aria-describedby="world-name-hint world-name-error"
         aria-invalid="true" autocomplete="off">
  <p class="error" id="world-name-error"><strong>Error:</strong> Enter a world name.</p>
</div>
```

- The `div.field` is a grid with `gap: var(--space-2)`: label, hint,
  field, error, top to bottom. The hint sits above the field so it is read
  before typing, and so a phone keyboard does not cover it.
- The label is always visible, `--text-md`, weight 600. Never a
  placeholder in place of a label. Placeholders are not used.
- Text inputs, `select` and `textarea`: background `surface`, text
  `text`, 1px `border`, `--radius`, `min-block-size: var(--target-min)`,
  `padding-inline: var(--space-3)`, `--text-md`, `inline-size: 100%` up
  to a `max-inline-size` that suits the content (`20ch` for a seed or
  port, `40ch` for names).
- Fields that hold game text (a world id, a seed, a map exchange string, a
  console command) use `--font-mono`.
- Hint: `--text-sm`, `text-muted`. Only present when it says something
  the label cannot.
- Error: shown only after a submit fails. `--text-sm`, `danger-text`,
  starting with the bold word "Error:" so the message is not colour alone.
  The field gets `aria-invalid="true"` and a 2px `danger-text` border
  (the extra width keeps the edge visible without colour).
  The error says what to do: "Enter a world name", "Choose a save file
  that ends in .zip", "Passwords must be at least 12 characters", never
  "Invalid input".
- Use the `type`, `autocomplete` and `inputmode` that match the data
  (`autocomplete="username"`, `"current-password"`, `"new-password"`;
  `inputmode="numeric"` for a port) so phones show the right keyboard
  and password managers work.
- **Checkboxes and radios** are native, with `accent-color:
  var(--color-accent)` and `inline-size` and `block-size` of 1.25rem. The
  input sits inside its `label`, and the label is a flex row with
  `min-block-size: var(--target-min)`, so the whole row is the target.
- A group of radios or checkboxes is a `fieldset` with a `legend` styled
  like a label. "Which save loads" (SPEC 7.11) is a radio group.
- **File inputs** are native. `::file-selector-button` is styled as a
  secondary button. The hint names the accepted type and size limit.

**Error summary.** When a submitted form has errors, the page shows a
message (7.3, error kind) at the top of the form, headed "Fix N problems
to continue" (or "Fix this problem to continue"), with a list of links,
one to each field with an error, using the same words as the field's
error. On a full page load the summary receives focus (`tabindex="-1"`);
after an htmx swap, the swapped fragment moves focus to it with
`hx-on::after-swap`. Focus on the summary is how a screen reader user
learns the submit failed.

### 7.3 Messages

One component with four kinds. Each starts with a bold word, so the kind
never depends on colour.

```html
<div class="message message--error" role="alert">
  <p><strong>Error:</strong> The server did not answer over RCON within 5 minutes.
  It is still running. <a href="/logs">Read the last log lines</a>, then try Restart.</p>
</div>
```

| Kind | Word | Word colour | Background | Border | When |
|---|---|---|---|---|---|
| Error | "Error:" | `danger-text` | `danger-bg` | 1px `danger-text` | Something failed. Says what, and what to do next. |
| Warning | "Warning:" | `warn` | `warn-bg` | 1px `warn` | Something will go wrong unless acted on ("Snapshots use 41 GB and the disk has 3 GB free"). |
| Success | "Done:" | `ok` | `ok-bg` | 1px `ok` | An action finished and the result is not otherwise visible. |
| Note | "Note:" | `info` | `info-bg` | 1px `info` | Context that changes how a person reads the page ("Snapshots are using full copies, not hardlinks", SPEC 6.8). |

- Body text inside a message is `text`, not the kind colour; only the
  leading word is coloured. Links inside are `link` and underlined.
- Padding `var(--space-3)`, `--radius`, `max-inline-size: var(--measure)`.
  No icon, no left stripe, no close button: a message stays until the
  page changes.
- `role="alert"` only on an error that appears after an action without a
  page load. A message that is part of a freshly loaded page has no role.
  Success messages from htmx go to `#announcer` instead (section 6), with
  the visible message rendered beside the thing that changed.
- **Empty states** are plain paragraphs in `text-muted`, never an
  illustration: "No worlds yet. Import a folder of saves or upload a save
  file to make your first world." followed by the action buttons. The
  empty state always names the next step.
- **Loading.** Server-rendered pages arrive complete, so most screens
  have no loading state. Where htmx loads part of a page later (live
  status, player list), the slot reserves its final height with
  `min-block-size` and shows "Loading…" in `text-muted`, so nothing below
  it moves when the content arrives.

### 7.4 Confirming risky actions

SPEC 17.5: a destructive action says exactly what it will do and which
snapshot will be taken first, and needs a confirmation. SPEC 6.2: the
snapshot comes first, and if it fails nothing else happens.

The confirmation is **a step, not a pop-up**. The first button
(secondary, 7.1) goes to a confirmation page at its own URL, such as
`GET /worlds/{id}/delete`. With htmx the same fragment can replace the
action area in place (`hx-get`, `hx-target`, `hx-push-url`), and focus
moves to its heading. Without JavaScript the page still works. No
`window.confirm()`, which cannot say enough, and no modal dialog, which
is harder to get right on a phone and adds nothing here.

```html
<section class="confirm" aria-labelledby="confirm-heading">
  <h2 id="confirm-heading" tabindex="-1">Delete the world "Family base 2025"?</h2>
  <p>This will:</p>
  <ul>
    <li>Delete the folder <code>worlds/family-base-2025</code>, with its 14 saves,
        38 mods and settings (2.1 GB).</li>
    <li>Keep its snapshots. They stay listed under Snapshots and can be restored.</li>
  </ul>
  <form method="post" action="/worlds/family-base-2025/delete">
    <label class="check">
      <input type="checkbox" name="final_snapshot" checked>
      Take a final snapshot first (about 2.1 GB, kept until you delete it)
    </label>
    <div class="field">
      <label for="confirm-name">Type the world's name to confirm</label>
      <input id="confirm-name" name="confirm_name" type="text" required autocomplete="off">
    </div>
    <div class="actions">
      <button type="submit" class="button button--danger">Delete world</button>
      <a href="/worlds/family-base-2025">Cancel, keep the world</a>
    </div>
  </form>
</section>
```

Rules:

- The box: background `danger-bg`, 1px `danger-text` border, `--radius`,
  padding `var(--space-4)`, a grid with `gap: var(--space-3)`. It is the
  only place where danger colour fills an area larger than a button.
- **The heading is a question that names the thing**: the world, the
  snapshot, the save. Never "Are you sure?".
- **The list says what will happen, in order, with real numbers**: which
  files, how many, how large, whether the server stops and for how long,
  what players will see. If anything is lost, say so plainly: "Progress
  since the last save, 4 minutes ago, will be lost" (forced stop, SPEC
  7.7).
- **The snapshot line says which snapshot.** Where the snapshot is
  automatic (SPEC 6.2), it is a plain list item: "First, Foreman takes a
  snapshot of Family base 2025, labelled 'Before restore'. If the
  snapshot fails, nothing else happens." Where the spec lets the admin
  choose (the final snapshot on delete, SPEC 5.10), it is a checkbox,
  checked by default. The exact label format belongs to Epic 2.
- **The confirming button repeats the verb** of the first button ("Delete
  world", "Restore snapshot", "Force stop"), styled danger. "Cancel" is a
  plain link back, padded to 44 px tall, and its text says what cancelling
  keeps where that helps.
- **Typed confirmation** is only for deleting a world (SPEC 5.10). The
  server compares the typed name and, if it differs, re-renders the page
  with a field error ("Type Family base 2025 exactly, or cancel"). Other
  confirmations are one click, so typing does not become a habit.
- **Undo is named.** When the action can be undone from a snapshot, the
  confirmation says where: "To undo, restore that snapshot from the
  Snapshots page."
- After the action, the result page shows a Done message (7.3) naming
  the snapshot that was taken, linked.

Which actions need this: every operation SPEC 6.2 counts as risky, plus
delete (5.10), restore (6.5), forced stop (7.7), "start anyway" after a
failed pre-start check (7.4, which must name exactly what the game will
delete), and in later epics switching (7.9) and destructive player
actions (8.2). A graceful stop or restart does not need a confirmation
page; its button says "Stop server (players get a 60-second warning)".

### 7.5 Server-state badge

SPEC 7.1 and 17.4: state is never shown by colour alone.

```html
<span class="state state--running">Running</span>
```

| State | Word | Text | Background | Shape (drawn in CSS) |
|---|---|---|---|---|
| stopped | "Stopped" | `text-muted` | `sunken` | Hollow square |
| starting | "Starting" | `warn` | `warn-bg` | Square, left half filled |
| running | "Running" | `ok` | `ok-bg` | Filled square |
| stopping | "Stopping" | `warn` | `warn-bg` | Square, right half filled |
| crashed | "Crashed" | `danger-text` | `danger-bg` | Filled diamond (the square turned 45 degrees) |
| crash-looping | "Crash-looping" | `on-danger` | `danger` | Filled square; the whole badge is solid |

- The word is the badge's content. It is real text, read by screen
  readers and kept in forced-colours mode.
- The shape is a 0.625rem square drawn with `::before`, `content: ""`,
  `border: 2px solid currentColor` and a background or
  `linear-gradient` hard stop at 50% for the half fills (a hard-stop fill,
  not a visual gradient). It is a second cue for people scanning a list,
  and it is decorative: the word alone carries the meaning.
- `display: inline-flex`, `gap: var(--space-1)`, `--text-sm`, weight 600,
  padding `0.125rem var(--space-2)`, 1px border in `currentColor`,
  `--radius`. Sentence case, never all capitals.
- Crash-looping is filled solid because it is the one state that needs a
  person to act (SPEC 8.9: Foreman has stopped the game and will not
  restart it alone).
- On the server page the badge sits in a `dl` with the other status facts
  (SPEC 7.2):

  ```html
  <dl class="facts">
    <div><dt>State</dt><dd><span class="state state--running">Running</span></dd></div>
    <div><dt>Uptime</dt><dd><time datetime="PT3H12M">3 h 12 min</time></dd></div>
    <div><dt>Active world</dt><dd>Family base 2025</dd></div>
    <div><dt>Game version</dt><dd><code>2.0.72</code></dd></div>
    …
  </dl>
  ```

  The `dl` is a grid, `repeat(auto-fit, minmax(10rem, 1fr))`, so it is
  one column at 320 px and several on a wide screen. `dt` is `--text-sm`
  `text-muted`; `dd` is `--text-md` `text`.
- When the state changes live (SPEC 16.3), the server sends the new
  badge, and a short sentence goes to `#announcer` ("Server is now
  running"). Uptime, CPU and memory update silently; they must never be
  in a live region, or a screen reader would talk every few seconds.

### 7.6 Long-running operations

Start can take up to 5 minutes (SPEC 7.3); restore and, later, switching
run several steps. While one runs, the page shows an ordered list of the
steps, each with a word:

```html
<ol class="steps">
  <li><span class="step-state">Done</span> Snapshot taken</li>
  <li aria-current="step"><span class="step-state">In progress</span> Waiting for the game to reach InGame</li>
  <li><span class="step-state">Waiting</span> Check RCON answers</li>
</ol>
```

Step words are "Done", "In progress", "Waiting", "Failed" and "Skipped",
coloured `ok`, `warn`, `text-muted`, `danger-text` and `text-muted`. The
in-progress step may carry a native `<progress>` with no value
(indeterminate). A failed step is followed by an error message (7.3)
that says what to do next, as SPEC 7.9 requires for switching.

## 8. Patterns for later screens

These are not built in Epic 1. They are here so Epics 2 and 3 start from
the same rules.

### 8.1 Headings and sections

Each page is `h1`, then `section` elements each with an `h2`. A section
with actions puts its button row directly under its heading or at its
end, never floating in a corner.

### 8.2 The world list (SPEC 5.4)

A `ul`, one `li` per world, each holding an `h2` or `h3` with the world's
name (a link to the world's page), then a `dl.facts` with game version,
Space Age, last played and disk use. The active world carries the word
"Active" in a plain badge (text `text`, background `sunken`) beside its
name, and is listed first. A list with a `dl` per item reflows at any
width, where a six-column table would not fit 320 px. Rows are separated
by a 1px `rule` border, not boxed as cards.

### 8.3 Tables (saves, snapshots, players)

`table` with a `caption`, `th scope="col"`, and at most four columns,
one of which may be actions. The name column wraps with
`overflow-wrap: anywhere`; numbers are right-aligned and tabular. Header
row: `--text-sm`, weight 600, bottom border 1px `border`. Body rows:
bottom border 1px `rule`. No zebra stripes. If a table needs more than
four columns, it becomes a list with a `dl` per row (8.2) instead.
Sizes have one decimal and a unit ("2.1 GB"). Times use `time` with a
`datetime` attribute; the displayed format is Epic 2's choice.

### 8.4 Long lists

A world's saves can number 30 or more. Lists and tables show everything
up to 50 rows; past that, Epic 2 decides between paging and "Show older".
No infinite scroll.

### 8.5 Log and console (SPEC 8.4, 8.6)

```html
<section aria-labelledby="log-heading">
  <h2 id="log-heading">Game log</h2>
  <div class="log" tabindex="0" role="region" aria-label="Game log lines">
    <pre>…</pre>
  </div>
</section>
```

- Background `sunken`, text `text`, `--font-mono` at `--text-sm`,
  `white-space: pre-wrap`, `overflow-wrap: anywhere`, so lines wrap and
  never scroll sideways.
- The box has a fixed `block-size: min(60vh, 32rem)` and scrolls
  vertically. It is focusable so keyboard users can scroll it.
- It is not a live region. A game log can print many lines a second; a
  screen reader announcing each would make the page unusable. Crashes
  and state changes are announced through the badge (7.5) instead.
- New lines are appended, not re-rendered, and the DOM keeps a fixed
  maximum (Epic 3 sets the number; 1,000 lines is a reasonable start).
  Older lines are dropped from the top.
- Auto-scroll follows new lines only while the view is at the bottom; if
  a person scrolls up to read, it stops following, and a "Jump to newest"
  button appears.
- The console is a `form` with a labelled mono text field ("Command") and
  a "Send" button, above an `ol` of past commands and responses, newest
  last, in the same sunken mono box. The response to the person's own
  command goes to `#announcer`, because they asked for it.

## 9. Writing

- Plain words, sentence case, no exclamation marks, no "Oops".
- Name things the way the game and the spec do: world, save, snapshot,
  mod, RCON, "InGame".
- Errors say what happened and what to do next, in that order.
- Never show a secret: no RCON password, token or game password appears
  in any message, field value or log view (SPEC 14.8).
- Numbers carry units ("2.1 GB", "60 seconds"), and times say whose zone
  when it matters (scheduled restarts).

## 10. Checks every screen passes

The end-to-end tests for each screen check, in both themes
(`colorScheme: 'light'` and `'dark'` in Playwright):

- an axe scan with no violations (SPEC 17.1);
- at 320 px wide, no horizontal scroll (6, rule 1);
- every button and field at least 44 by 44 CSS pixels;
- the page can be used with the keyboard alone, with a visible focus ring;
- state badges contain their word.

## 11. CSS file layout

One stylesheet, `internal/web/static/foreman.css`, in this order: tokens
(light, then the dark media query), a small reset, base elements
(`body`, headings, links, `code`, `table`), the shell, then one block per
component in the order of section 7. No preprocessor, no `@import`.

## 12. Deliberately deferred

Left out because no Epic 1 to 3 screen needs it. Add each when a real
screen does, in this document first.

- A manual theme switch. The system setting decides (SPEC 17.3).
- Icons of any kind.
- Modal dialogs, pop-overs, menus, tabs and toasts. The confirmation step
  (7.4) and the message (7.3) cover Epics 1 to 3.
- Charts for CPU and memory. Numbers in the status `dl` are enough for
  SPEC 7.2.
- Pagination. See 8.4.
- Mod search results, dependency plans and the mod list (Epic 5).
- The world-switch progress screen beyond the step list in 7.6 (Epic 4).
- Chat (Epic 8).
- A print stylesheet.
