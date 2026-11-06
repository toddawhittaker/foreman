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
marketing page, and it should feel at home next to the game it runs.

The look is inspired by Factorio's in-game interface: dark warm-grey
panels with bevelled edges, lighter sunken areas for content, cream
headings, orange for what is selected, green for go and red for
destroy. Foreman is not made by, affiliated with or endorsed by Wube
Software. It uses its own colour values and none of the game's artwork,
icons, logos, fonts or textures, and nothing in it should suggest it is
an official product.

- **Frames, not cards.** The page is built from a few nested frames: a
  raised outer frame, a lighter sunken inner panel for content, and a
  deep sunken box for fields and the log (section 6). The frames hold
  lists and tables; they do not turn each item into a box.
- **Warm grey, with colour that means something.** Neutrals are warm
  greys, like painted steel. Orange marks what is selected or active and
  what is under the pointer. Green is the one main "go" action on a
  screen. Red is the final step of a destructive action. Yellow-orange is
  a warning. None of them decorates.
- **Dark first.** Dark is the main look, as in the game. The light theme
  is the same frames in pale warm grey, not an inverted afterthought.
- **The bevel is the only depth.** Frames, buttons and fields get a 1 px
  light and dark inset edge (section 4, "Bevels"). There are no
  gradients, glows, blurred glass or drop shadows. Corners are 2 px:
  square enough to read as a panel, soft enough not to look sharp.
- **No logo, hero or illustration.** The product name in the header is
  the only branding.
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

Both themes are defined as CSS custom properties. The dark values sit on
`:root`, because dark is the main look; the light values replace them
inside `@media (prefers-color-scheme: light)`. `:root` also sets
`color-scheme: dark light` so native controls (checkboxes, scroll bars,
the file picker) follow the theme. There is no theme switch in the
interface; the system setting decides (SPEC 17.3). Both themes must meet
every ratio below (SPEC 17.1).

Every colour in the CSS comes from a token below. A raw hex value
anywhere else in the stylesheet is a bug.

### Tokens

| Token | Dark | Light | Use |
|---|---|---|---|
| `--color-bg` | `#141312` | `#bab3a8` | Backdrop behind the frames; seen only in the gaps between them |
| `--color-frame` | `#2f2d2a` | `#e3ded6` | Outer frame: the header and `main` |
| `--color-frame-light` | `#57534d` | `#fbf9f6` | Light side of a bevel (decorative) |
| `--color-frame-dark` | `#0c0b0a` | `#8c857b` | Dark side of a bevel, and the 1 px outline of frames (decorative) |
| `--color-inner` | `#3d3a36` | `#f3f0ea` | Inner panel: the sunken, lighter area that holds a section's content |
| `--color-deep` | `#1b1a18` | `#ffffff` | Deep sunken box: form fields, the log and console, the "stopped" badge |
| `--color-text` | `#ebe6dd` | `#1c1a17` | Body text, field text, header links |
| `--color-heading` | `#f5dcb0` | `#4b3010` | Headings and the product name: pale cream in dark, deep brown in light |
| `--color-text-muted` | `#b8b0a4` | `#58534b` | Hints, timestamps, table captions, `dt`, empty states, the "stopped" badge |
| `--color-border` | `#958c80` | `#7a7369` | Edge of fields; edge of secondary buttons in light; table header rule |
| `--color-rule` | `#57534d` | `#cdc7bd` | Decorative dividers between rows; never the only edge of a control |
| `--color-link` | `#ffbd6b` | `#9a4800` | Links (always underlined) |
| `--color-highlight` | `#f39a2a` | `#b05a00` | Selected or active: current nav tab, "Active" badge, checked boxes |
| `--color-on-highlight` | `#1a1004` | `#ffffff` | Text on `highlight` |
| `--color-button` | `#a8a198` | `#d2ccc2` | Secondary button face |
| `--color-on-button` | `#161412` | `#1c1a17` | Secondary button text |
| `--color-button-hover` | `#c4beb5` | `#e6e1d8` | Secondary button hover face |
| `--color-confirm` | `#62b04f` | `#2d7621` | Primary ("go") button face |
| `--color-confirm-hover` | `#7cc768` | `#235f19` | Primary button hover |
| `--color-on-confirm` | `#0c1808` | `#ffffff` | Primary button text |
| `--color-danger` | `#ea6248` | `#b3301d` | Danger button face, "crash-looping" badge background |
| `--color-danger-hover` | `#f2836f` | `#932616` | Danger button hover |
| `--color-on-danger` | `#1a0503` | `#ffffff` | Danger button text, "crash-looping" badge text |
| `--color-danger-text` | `#ff978a` | `#a32a18` | Error words, field error text, invalid field border, "crashed" badge |
| `--color-danger-bg` | `#45201a` | `#f9e3de` | Error message, "crashed" badge and confirmation box background |
| `--color-ok` | `#8fd77c` | `#22662a` | "Done" word, "running" badge |
| `--color-ok-bg` | `#21361b` | `#e2efdc` | Success message and "running" badge background |
| `--color-warn` | `#f7c548` | `#7a5000` | "Warning" word, "starting" and "stopping" badges |
| `--color-warn-bg` | `#3d3112` | `#f6ebcf` | Warning message and "starting"/"stopping" badge background |
| `--color-info` | `#a3c6ff` | `#174ea6` | "Note" word |
| `--color-info-bg` | `#1c2a3e` | `#e3ebf8` | Notice background |
| `--color-focus` | `#8fb8ff` | `#0a56c8` | Focus outline |

How the colours divide the work:

- **Orange means "this one".** The current page's tab, the active world,
  a checked box. It is never the colour of
  an action's face, so a person never wonders whether orange is a
  button.
- **Green means go.** Only the primary button (7.1) is green, plus the
  "Done" word and the "running" badge, which say an action or the server
  succeeded.
- **Red means destroy or failed.** The final button of a destructive
  action, errors, and the crashed states.
- **Yellow-orange means warning**: the "Warning" word and the in-between
  server states. It is lighter and yellower than the orange highlight in
  dark, and browner in light, and it always comes with its word.
- **The focus ring is blue**, the one hue none of the above uses, so it
  cannot be mistaken for selection (orange), go (green) or danger (red).
  It works on every surface in both themes, so the header needs no
  focus colour of its own.

### Contrast

Every ratio below was computed with the WCAG 2.2 relative-luminance
formula from the hex values above, not estimated. Text needs 4.5:1
(1.4.3, normal-size text, since nothing here relies on the large-text
exception). Control edges, focus outlines and the state badge's drawn
shape need 3:1 against what is next to them (1.4.11).

The script that produced them is short enough to rerun when a token
changes: compute each colour's relative luminance L, then
`(L_lighter + 0.05) / (L_darker + 0.05)`.

The lowest text ratio is 5.27:1 in dark (`text-muted` on `inner`) and
4.75:1 in light (`link` on `frame`). The lowest edge or focus ratio is
3.41:1 in dark (`border` on `inner`, and `danger` on `inner`) and 3.18:1
in light (`focus` on `bg`).

**Text pairings (need 4.5:1)**

| Foreground | Background | Dark | Light |
|---|---|---|---|
| `text` | `frame` | 11.05 | 12.97 |
| `text` | `inner` | 9.10 | 15.26 |
| `text` | `deep` | 13.99 | 17.36 |
| `heading` | `frame` | 10.29 | 9.07 |
| `heading` | `inner` | 8.48 | 10.68 |
| `heading` | `danger-bg` (confirmation heading) | 10.68 | 9.87 |
| `text-muted` | `frame` | 6.40 | 5.70 |
| `text-muted` | `inner` | 5.27 | 6.70 |
| `text-muted` | `deep` | 8.10 | 7.63 |
| `link` | `frame` | 8.33 | 4.75 |
| `link` | `inner` | 6.86 | 5.59 |
| `link` | `deep` | 10.55 | 6.36 |
| `on-button` | `button` | 7.19 | 10.88 |
| `on-button` | `button-hover` | 9.96 | 13.33 |
| `on-highlight` | `highlight` | 8.45 | 4.87 |
| `on-confirm` | `confirm` | 6.82 | 5.64 |
| `on-confirm` | `confirm-hover` | 8.89 | 7.71 |
| `on-danger` | `danger` | 5.94 | 6.26 |
| `on-danger` | `danger-hover` | 7.73 | 8.30 |
| `danger-text` | `inner` | 5.41 | 6.36 |
| `danger-text` | `deep` | 8.32 | 7.24 |
| `danger-text` | `danger-bg` | 6.81 | 5.88 |
| `ok` | `inner` | 6.56 | 6.15 |
| `ok` | `ok-bg` | 7.57 | 5.87 |
| `warn` | `inner` | 7.02 | 6.21 |
| `warn` | `warn-bg` | 7.92 | 5.95 |
| `info` | `info-bg` | 8.33 | 6.54 |
| `text` | `danger-bg` | 11.46 | 14.11 |
| `text` | `ok-bg` | 10.51 | 14.56 |
| `text` | `warn-bg` | 10.26 | 14.64 |
| `text` | `info-bg` | 11.65 | 14.47 |
| `link` | `danger-bg` | 8.64 | 5.17 |
| `link` | `ok-bg` | 7.92 | 5.34 |
| `link` | `warn-bg` | 7.74 | 5.36 |
| `link` | `info-bg` | 8.79 | 5.30 |

**Control edges and focus (need 3:1)**

| Foreground | Background | Dark | Light |
|---|---|---|---|
| `border` (field edge) | `frame` | 4.15 | 3.50 |
| `border` | `inner` | 3.41 | 4.12 |
| `border` | `deep` | 5.25 | 4.68 |
| Secondary button edge (see below) | `frame` | 5.37 | 3.50 |
| Secondary button edge | `inner` | 4.42 | 4.12 |
| `highlight` (selected tab, "Active" badge) | `frame` | 6.19 | 3.64 |
| `highlight` | `inner` | 5.10 | 4.29 |
| `confirm` (primary button face) | `frame` | 5.13 | 4.21 |
| `confirm` | `inner` | 4.22 | 4.96 |
| `danger` (danger button face) | `frame` | 4.14 | 4.67 |
| `danger` | `inner` | 3.41 | 5.50 |
| `danger-text` (invalid field border) | `inner` | 5.41 | 6.36 |
| `danger-text` | `deep` | 8.32 | 7.24 |
| `focus` | `bg` | 9.25 | 3.18 |
| `focus` | `frame` | 6.84 | 4.94 |
| `focus` | `inner` | 5.64 | 5.81 |
| `focus` | `deep` | 8.67 | 6.61 |
| `focus` | `danger-bg` (inside a confirmation) | 7.10 | 5.37 |
| `focus` | `ok-bg` | 6.51 | 5.54 |
| `focus` | `warn-bg` | 6.36 | 5.57 |
| `focus` | `info-bg` | 7.22 | 5.51 |
| `ok` (badge border and shape) | `ok-bg` | 7.57 | 5.87 |
| `warn` | `warn-bg` | 7.92 | 5.95 |
| `danger-text` | `danger-bg` | 6.81 | 5.88 |
| `info` (message border) | `info-bg` | 8.33 | 6.54 |
| `text-muted` ("stopped" badge) | `deep` | 8.10 | 7.63 |
| `on-danger` ("crash-looping" shape) | `danger` | 5.94 | 6.26 |

The secondary button's edge is its face in dark (`button`, a light grey
on dark frames) and its 1 px `border` in light, where a pale face on a
pale frame cannot carry the edge alone. The rows above use whichever
one draws the edge in that theme.

Not covered by a ratio, on purpose:

- The bevel colours (`frame-light`, `frame-dark`) and the steps between
  `bg`, `frame`, `inner` and `deep`. They give depth, not information.
  For the record, `inner` on `frame` is 1.21 in dark and 1.18 in light.
  No control relies on them: every control's edge is one of the rows
  above.
- `--color-rule`, which is decorative and never the only thing that
  shows where a control is.
- A disabled control (exempt under 1.4.3; this design avoids disabled
  controls, see 7.1), and native checkboxes and radios, whose edges the
  browser draws for the `color-scheme` in use.

## 4. Spacing

A 4 px base, in `rem` so it scales with text size.

| Token | Value | Typical use |
|---|---|---|
| `--space-1` | 0.25rem (4 px) | Gap between a badge's shape and its word; gap between the header and `main` frames |
| `--space-2` | 0.5rem (8 px) | Gap between a label, its hint and its field; table cell padding block |
| `--space-3` | 0.75rem (12 px) | Gap between buttons in a row; table cell padding inline; message padding; inner panel padding; `main` frame padding at phone width |
| `--space-4` | 1rem (16 px) | Gap between form fields; confirmation box padding; page padding from 40rem up |
| `--space-5` | 1.5rem (24 px) | Gap between sections inside `main`; `main` frame padding from 40rem up |
| `--space-6` | 2rem (32 px) | Gap before a page's closing actions |

Other fixed values, also tokens:

| Token | Value | Use |
|---|---|---|
| `--radius` | 2px | Frames, panels, buttons, fields, messages, badges |
| `--border-width` | 1px | Every border except the ones below |
| `--focus-width` | 3px | Focus outline |
| `--target-min` | 2.75rem (44 px) | Minimum block size of buttons and fields |
| `--measure` | 70ch | Maximum line length of prose |
| `--page-max` | 64rem | Maximum width of `main` |

### Bevels

The frame effect is the one place a shadow is allowed. It is two 1 px
inset `box-shadow`s, never a blurred or offset shadow outside the box:

```css
--bevel-raised: inset 1px 1px 0 var(--color-frame-light),
  inset -1px -1px 0 var(--color-frame-dark);
--bevel-sunken: inset 1px 1px 0 var(--color-frame-dark),
  inset -1px -1px 0 var(--color-frame-light);
```

| Element | Bevel | Outline |
|---|---|---|
| Header and `main` frames | raised | 1px `frame-dark` border |
| Inner panel (a section's content) | sunken | none; 1px `CanvasText` in forced colours only |
| Field, log, console | sunken | 1px `border` (the edge that counts for contrast) |
| Button | raised; sunken while pressed (`:active`) | 1px border, see 7.1 |
| Message, badge, confirmation box | none (flat) | 1px border in their own colour |

The bevel is decoration. In forced-colours mode box-shadows are not
drawn, and the 1 px borders in the "Outline" column are what remain.
That is why every frame and control keeps one. The inner panel has no
border in the normal look, so `@media (forced-colors: active)` gives it a
1px `CanvasText` border (section 6, "Forced colours").

### Rules

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
- The 2 px offset puts the ring on the surface around the control
  (`frame`, `inner`, `deep` or a message background), which is why the
  focus ratios in section 3 are measured against those, not against
  button faces.
- The ring is blue in both themes, so it stays distinct from the orange
  highlight, the green primary button and the red danger button (3,
  "How the colours divide the work"). A focused button that is also
  hovered shows both its lighter grey face and the blue ring.
- A 3 px ring around a control at least 44 px tall meets WCAG 2.2 focus
  appearance (2.4.13, AAA) as well as focus visible (2.4.7, AA).
- Nothing sticky may cover the focused element (2.4.11). The header is
  not sticky.
- In Windows high-contrast mode (`forced-colors: active`) the outline
  still shows. Bevels disappear there, and every frame, button and field
  keeps a 1 px border (section 4, "Bevels") so its edge shows too; the
  inner panel, which has only a bevel, gets its border in that mode.

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

- The header is a flex row with `flex-wrap: wrap` and `gap`. Below 40rem
  it is two rows, about 112 px tall at 320 px: the product name and the
  sign-out form share the first row, and the nav fills the second
  (`order: 1; flex-basis: 100%`), its tabs on one line. The sign-out form
  takes the width the name leaves, and "Signed in as …" is `--text-sm`,
  aligned to the end, and wraps inside it, so Sign out never gets a row
  of its own. Below 40rem the tabs are `--text-sm`, padded by `--space-1`
  and share the row out between them (`flex: 1 1 auto`); that keeps four
  tabs on one line at 320 px even beside a 15 px scrollbar. From 40rem
  the header is one row: name, tabs at `--text-md` padded by `--space-3`,
  then the sign-out form at the end. The focus order is the source order
  (name, tabs, Sign out) at every width; on a phone that means Sign out,
  drawn on the first row, is reached after the tabs. There is no
  hamburger menu: Epics 1 to 3 have at most four nav items, and a menu
  would hide them behind an extra tap. Revisit only above six items.
- The header is a frame (section 4, "Bevels"): background `frame`,
  raised bevel, 1px `frame-dark` border. The product name is `heading`,
  weight 650; nav links and the user name are `text` and `text-muted`.
  The sign-out button is a secondary button (7.1).
- Nav links are tabs: no underline, padded to `--target-min` tall,
  `--radius`. The current page's link has `aria-current="page"` and is
  drawn as the selected tab: background `highlight`, text
  `on-highlight`, bold weight. The filled tab is a shape as well as a
  colour, and the bold weight is a third cue. Other tabs show the grey
  `button-hover` face with `on-button` text on hover (`on-button` on
  `button-hover` is 9.96:1 dark, 13.33:1 light, in the table above), so
  orange means only "selected".
- Sign out is a `POST` form with a button, never a link, so a crawler or
  prefetch cannot sign anyone out.
- The skip link is the first focusable element. It is visually hidden
  until focused, then appears at the top-left of the header.
- `body` is the backdrop (`bg`): a grid with `gap: var(--space-1)` and no
  padding at phone width, so the frames run edge to edge; from 40rem it
  gets `padding: var(--space-4)`.
- `main` is a frame like the header: background `frame`, raised bevel,
  1px `frame-dark` border, `max-inline-size: var(--page-max)`, centred,
  `padding: var(--space-3)` at phone width and `var(--space-5)` from
  40rem. Its `h1` sits on the frame in `heading`. Its children are a
  grid with `gap: var(--space-5)`.
- Each `section` inside `main` is an **inner panel**: background
  `inner`, sunken bevel, `padding: var(--space-3)`, a grid with
  `gap: var(--space-3)`. Its `h2` is inside the panel, in `heading`.
  Panels never nest; inside a panel, structure comes from headings,
  lists and rules. At 320 px this leaves about 270 px for content.
- A page with a single form and no sections (sign-in, setup) puts the
  form itself in one inner panel.
- The `#announcer` region is where htmx swaps short results ("Snapshot
  taken", "Server started") so screen readers hear them. Errors are not
  announced from here; see 7.3.
- Viewport media queries are used only for the shell (`body`, header and `main`
  padding). Every component lays itself out from its container with
  `container-type: inline-size` and `@container` queries.

### Forced colours

Windows high contrast (`forced-colors: active`) replaces every fill and
colour with the user's system colours, so the orange tab would look like
the others. `@media (forced-colors: active)` therefore gives the current
tab a 2px `Highlight` border (every tab carries a transparent 2px border
so nothing shifts) and a 3px underline, so "selected" is still a drawn
shape. The "Active" badge keeps its border and its word, and buttons,
fields and message boxes keep their borders, so they need nothing more.
The inner panel has only a bevel, so it gets a 1px `CanvasText` border in
this mode.
The state badge's shapes (7.5) are redrawn in `CanvasText` so their fills
survive. Anything new that marks a state with a fill alone needs a rule here. Check
a screen with Playwright's `forcedColors: "active"`.

### The 320 px rules

These hold for every page (SPEC 17.2), checked in end-to-end tests at a
320 px wide viewport:

1. `document.documentElement.scrollWidth` is not larger than the
   viewport. No element may force horizontal scrolling of the page.
2. Text wraps. `body` sets `overflow-wrap: anywhere`, so a long name or
   file name breaks instead of widening the page; table cells after the
   first reset it (8.3). The shell grids use `minmax(0, 1fr)` columns.
   Long strings use `overflow-wrap: anywhere`. Flex children
   holding text get `min-width: 0`.
3. Every button and field is at least `--target-min` (44 px) tall and
   44 px wide. Links inside running text are exempt (WCAG 2.5.8 inline
   exception); a link standing alone, such as "Cancel", is padded to 44 px
   tall.
4. Adjacent targets are separated by at least `--space-2`, so two 24 px
   minimum targets can never overlap even when a person's text size
   shrinks things.
   The parts the server swaps in (`[sse-swap]`) and `#console-panel` are
   grids with `gap: var(--space-3)`, so a button is never flush under a
   step list, message or log.
5. Button rows wrap (`flex-wrap: wrap`). Each button may grow to the full
   width when its row wraps, so a phone shows them stacked, primary first.
   A button in a form of its own (`.actions > form`) takes part in the row
   too: the form is `display: contents`, so "Take snapshot" stacks at the
   same width as the links beside it.
6. Tables follow 8.3. The log and console wrap their lines (8.5); they
   scroll vertically inside their own box, never horizontally.
7. Nothing is fixed or sticky to the viewport, so the on-screen keyboard
   never covers a field.

### What a phone shows first

The vision's success test runs from a phone, so the most common task on
each page is on its first screen, at 320 by 640 px, without scrolling.
The order is the source order at every width, so reading order, focus
order and the picture agree.

- **Server page.** After the "Active world" link: "Start and stop" (the
  controls, and while an operation runs its step list and "Cancel
  stop"), then "Status", "Players online" and "Switch to another
  world". The state is still one glance away, and the buttons on offer
  already say whether the game runs.
- **Status facts** (7.5) sit two to a row at 320 px: the grid is
  `repeat(auto-fit, minmax(min(100%, 7rem), 1fr))`.
- **Force stop** is a plain link standing alone (rule 3), after the
  buttons in the control row, never a button face. It leads to its own
  confirmation (7.4), and drawn as a link it weighs less than Stop and
  Restart, so the drastic choice is not the one the eye lands on.
- **Console page.** The "Console" section, with the command field, comes
  before "Game log". The history box under the field is only as tall as
  what it holds (`block-size: auto`), up to `min(40vh, 20rem)`, so the
  newest reply sits close under the field. The game log keeps its fixed
  height (8.5), so new lines never move the page.
- **World list** (8.2). Each row is a grid, `minmax(0, 1fr) auto`: the
  name heading in the first column and, when the world can be switched
  to, its "Switch" button beside it in the second (`.facts ~ .actions`
  moves to row 1). Everything else spans both columns, and the facts sit
  two to a row, so a world takes about 170 px at 320 px instead of 290.
  A long name wraps in its column; Switch keeps its width.

### Motion

Only `opacity` and `transform` may animate, for at most 150 ms. Under
`prefers-reduced-motion: reduce`, nothing animates, including the
indeterminate progress bar, which is hidden in that mode (`.steps
progress { display: none }`) because the browser animates it itself; the
"In progress" word carries the meaning. Nothing on these screens needs motion to be
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

| Kind | Face | Text | Border | Hover | Use |
|---|---|---|---|---|---|
| Primary (green, "go") | `confirm` | `on-confirm` | 1px `confirm` | face `confirm-hover` | The one main action on a screen or in a form: "Start server", "Sign in", "Create world". At most one per form. |
| Secondary (grey) | `button` | `on-button` | 1px `border` | face `button-hover`, text `on-button` | Every other action. The default. |
| Danger (red) | `danger` | `on-danger` | 1px `danger` | face `danger-hover` | Only the final, confirming button of a destructive action (7.4). |

All buttons: `min-block-size: var(--target-min)`, `min-inline-size:
var(--target-min)`, `padding-inline: var(--space-4)`, `--text-md`,
weight 600, `--radius`, `cursor: pointer`, `box-shadow:
var(--bevel-raised)`. While pressed (`:active`) the bevel flips to
`var(--bevel-sunken)`, so the button looks pushed in. Nothing moves and
nothing animates. A label wraps between words inside the button (it
never runs past the edge) when large text or a long translation needs two
lines; only a single word wider than the button is broken (`overflow-wrap:
break-word`, hyphenated where the browser can); the block padding is `--space-2` so a
two-line label is not cramped.

- Hover is a colour change only; it is never the sole sign of anything,
  because a phone has no hover. The secondary button's hover is a
  lighter grey, so orange keeps meaning only "selected".

- **Force stop** on the Server page is not a button at all: it is a plain
  link after the buttons (section 6, "What a phone shows first").
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
  shifts. The busy state is the only time a button is disabled. The one
  exception is a button the person pressed that must keep focus, such as
  Preview map (8.8): it is marked `aria-disabled="true"` and a click on it
  does nothing.
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
- Text inputs, `select` and `textarea` are **sunken boxes**: background
  `deep`, text `text`, 1px `border`, `box-shadow: var(--bevel-sunken)`,
  `--radius`, `min-block-size: var(--target-min)`,
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
  var(--color-highlight)`, so a checked box is orange like any other
  selected thing, and `inline-size` and `block-size` of 1.25rem. The
  input sits inside its `label`, and the label is a flex row with
  `min-block-size: var(--target-min)`, so the whole row is the target.
- A group of radios or checkboxes is a `fieldset` with a `legend` styled
  like a label. "Which save loads" (SPEC 7.11) is a radio group.
- **File inputs** are native, inside the same 44 px sunken box as other
  fields. `::file-selector-button` is styled as a secondary button (7.1):
  face `button`, text `on-button`, weight 600, raised bevel, `button-hover`
  on hover, `padding-inline: var(--space-4)`, a 1px `border` rule on its
  inline end, and filling the box's full height so it stays a 44 px target.
  The hint names the accepted type and size limit.

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
  Messages are flat (no bevel) so they never look like a control. No icon, no left stripe, no close button: a message stays until the
  page changes.
- `role="alert"` only on an error that appears after an action without a
  page load. A message that is part of a freshly loaded page has no role.
  Success messages from htmx go to `#announcer` instead (section 6), with
  the visible message rendered beside the thing that changed.
- On the Server page, the unfinished-switch notice (SPEC 7.10) and the
  game-network warning (SPEC 4.4) are Warnings in the page, outside any
  live region. The notice sits in the control panel with its Switch back
  button after it; the network warning sits with the status facts.
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

- The box is a section, so it takes an inner panel's place (section 6),
  but flat: background `danger-bg`, 1px `danger-text` border, no bevel,
  `--radius`, padding `var(--space-4)`, a grid with
  `gap: var(--space-3)`. Its heading stays `heading`. It is the only
  place where danger colour fills an area larger than a button.
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
delete), switching worlds (7.9), and in later epics destructive player
actions (8.2). Start and restart anyway have no page of their
own: the Warning that reports the refusal on the Server page is the
confirmation (SPEC 7.4). It lists what the game drops, then the steps,
the snapshots and the undo as above, and its danger button posts the
action at once. Restart uses the default 60-second countdown there. A
switch has its own confirmation page (SPEC 7.9, 17.8) that shows the new
world's problems before anything runs, and offers "Switch, leave stopped"
and, when every problem allows it, a danger button "Switch and start
anyway". The page carries the countdown field only when the game is
running. The Server page's own switch box appears only when problems arise
after that page. A graceful stop or restart does not need a danger
confirmation, because nothing is lost. Its button opens a short form at its
own URL (`GET /worlds/{id}/stop` or `/restart`) that asks for the
countdown (SPEC 7.6): a text field for seconds, 0 to 600, filled in with
60, where 0 means no warning (there is no checkbox), and a line naming who
is online. The form is a plain `panel` with a
primary button ("Stop server", "Restart server"), not the danger box, and
its Cancel link says the server keeps running. The result is shown on the
Server page.

### 7.5 Server-state badge

SPEC 7.1 and 17.4: state is never shown by colour alone.

```html
<span class="state state--running">Running</span>
```

| State | Word | Text | Background | Shape (drawn in CSS) |
|---|---|---|---|---|
| stopped | "Stopped" | `text-muted` | `deep` | Hollow square |
| starting | "Starting" | `warn` | `warn-bg` | Square, left half filled |
| running | "Running" | `ok` | `ok-bg` | Filled square |
| stopping | "Stopping" | `warn` | `warn-bg` | Square, right half filled |
| crashed | "Crashed" | `danger-text` | `danger-bg` | Filled diamond (the square turned 45 degrees) |
| crash-looping | "Crash-looping" | `on-danger` | `danger` | Filled square; the whole badge is solid |
| unknown | "Unknown" | `text-muted` | `deep` | Hollow circle; the badge edge is dashed |

"Unknown" is for when Foreman cannot ask Docker (SPEC 7.1). It uses the
same pair of colours as "Stopped" (8.10 and 7.63 on dark and light), so
no new contrast row is needed; the circle and the dashed edge tell the
two apart without colour.

Classes: `state` (the badge, also the "Stopped" look) plus one modifier,
`state--stopped`, `state--starting`, `state--running`, `state--stopping`,
`state--crashed`, `state--crash-looping` or `state--unknown`. The element
holds only the word. A template always writes the modifier, even for
"Stopped", so the class names a state.

- The word is the badge's content. It is real text, read by screen
  readers and kept in forced-colours mode.
- The shape is a 0.625rem square drawn with `::before`, `content: ""`,
  `border: 2px solid currentColor` and a background or
  `linear-gradient` hard stop at 50% for the half fills (a hard-stop fill,
  not a visual gradient). It is a second cue for people scanning a list,
  and it is decorative: the word alone carries the meaning.
- The fills are `background-image`, not `background-color`, because
  forced-colours mode (6) removes background colours. A rule there draws
  the shape in `CanvasText`, so "Running" stays a filled square and
  "Stopped" a hollow one. The solid crash-looping badge gets a 3px edge.
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

  The `dl` is a grid, `repeat(auto-fit, minmax(min(100%, 7rem), 1fr))`, so
  it is two to a row at 320 px and several on a wide screen. `dt` is `--text-sm`
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
  <li><span class="step-state step-state--done">Done</span> <span class="step-name">Snapshot taken</span></li>
  <li aria-current="step"><span class="step-state step-state--progress">In progress</span> <span class="step-name">Waiting for the game to reach InGame</span>
    <progress aria-label="Waiting for the game"></progress></li>
  <li><span class="step-state">Waiting</span> <span class="step-name">Check RCON answers</span></li>
</ol>
```

Step words are "Done", "In progress", "Waiting", "Failed" and "Skipped",
coloured `ok`, `warn`, `text-muted`, `danger-text` and `text-muted`. A step
the pre-start check refused reads "Refused", coloured like its message:
`warn` (`step-state--warning`) when the message offers going ahead anyway,
`danger-text` otherwise. The
in-progress step may carry a native `<progress>` with no value
(indeterminate). A failed step is followed by an error message (7.3)
that says what to do next, as SPEC 7.9 requires for switching.

Classes: `steps` (the `ol`; no numbers, one row per step), `step-state`
(the word) and its modifiers `step-state--done`, `step-state--progress`
and `step-state--failed`, and `step-name` (the step and any time left,
"Warn players: 40 seconds left"). "Waiting" and "Skipped" take no
modifier. The current step has `aria-current="step"` and is bold.

Each `li` is a grid, `6rem minmax(0, 1fr)`: the word in the first column
and the step beside it in the second, at every width, so a step is one
row even at 320 px and an 11-step switch fits in about 300 px rather
than twice that. A long step name wraps inside its own column, under
itself, never under the word. The `<progress>` sits in the second
column, under the step it measures. The list sits under the operation's
heading in the "Start and stop" section, which leads the Server page
(section 6, "What a phone shows first"), so the step in progress and
"Cancel stop" are on a phone's first screen. Steps that are done stay in
the list: it is the record of what happened (SPEC 7.10), and it is
redrawn live, so a collapsed part would close again on every update.
A failed step's message goes inside that `li` as
`<div class="message message--error">` (or `message--warning` for a refusal
with a choice); it spans both columns (`grid-column: 1 / -1`) at any
width, up to the message's reading width. Text inside a message fills that one width,
so a paragraph and a nested list item wrap alike. The words carry the state, so forced
colours needs no rule.

When an operation changes the active world (a switch), the
old world's open page reloads onto the Server page after the operation
ends, moves focus to the control heading, and writes the result sentence
to `#announcer`.

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
"Active" in a badge drawn as selected (text `on-highlight`, background
`highlight`, the same shape as the state badge in 7.5 but with no drawn
square) beside its name, and is listed first. A world that can be switched
to has a "Switch" button beside its name (its accessible name is "Switch to"
and the world's name); section 6 ("What a phone shows first") gives the
row's grid. On a narrow panel the Last played fact takes the whole row
(`.fact--wide`), so its date, clock with zone and age fit in two lines in
every language. The word carries the
meaning; the orange matches the selected tab. A list with a `dl` per item
reflows at any width, where a six-column table would not fit 320 px. The
list sits in one inner panel and its rows are separated by a 1px `rule`
border, not boxed as cards.

### 8.3 Tables (saves, snapshots, players)

`table` with a `caption`, `th scope="col"`, and at most four columns,
one of which may be actions. The name column wraps with
`overflow-wrap: anywhere`; numbers are right-aligned and tabular. Header
row: `--text-sm`, weight 600, bottom border 1px `border`. Body rows:
bottom border 1px `rule`. No zebra stripes. If a table needs more than
four columns, it becomes a list with a `dl` per row (8.2) instead.
A size cell (`class="num"`) never wraps, so "125" is not parted from
"B". A timestamp must not split at its hyphens, but a whole `2026-11-03
14:25 UTC` is too wide beside other columns at 320 px (it crushed the
name column to 24 px), so a template wraps the date and the time each in
`<span class="nowrap">`, and the timestamp breaks only between them.
With four columns even that is too wide at 320 px, so a four-column
table moves its second column (such as a snapshot's reason) into the
first cell, above the label, and drops to three.
A link that stands alone in a cell, such as a save's name, is padded to
`--target-min` tall; a badge may follow it in the same cell. A button in
a cell is an ordinary button (7.1). Sizes have one decimal and a unit ("2.1 GB"). Times use `time` with a
`datetime` attribute and are shown as section 9 describes.
A snapshot table stacks each row (snapshot, then "Taken:", then "Size:")
when its panel is narrower than 14rem, which is 200% text on a 320 px
phone. Changing `display` can drop table semantics in some browsers, so
these tables carry explicit `table`, `rowgroup`, `row`, `columnheader` and
`cell` roles (SPEC 17.12).
A time is three units: the date, the clock and the zone. None breaks inside
itself, and a wide zone such as `GMT+5:45` drops to its own line after the
clock. Each language keeps its own order (Chinese puts the zone first), and
`checkPage` fails a page where any unit is broken.

### 8.4 Long lists

A world's saves can number 30 or more. Lists and tables show everything
up to 50 rows; past that, Epic 2 decides between paging and "Show older".
No infinite scroll.

### 8.5 Log and console (SPEC 8.4, 8.6)

```html
<section aria-labelledby="log-heading">
  <h2 id="log-heading">Game log</h2>
  <div class="log-frame">
    <div class="log" tabindex="0" role="region" aria-label="Game log lines">
      <pre>…</pre>
    </div>
    <button type="button" class="button" hidden>Jump to newest</button>
  </div>
</section>
```

The console section comes before the game log on the page (section 6).
Inside it, the command field comes above its history:

```html
<section aria-labelledby="console-heading">
  <h2 id="console-heading">Console</h2>
  <form class="field" method="post" action="…">
    <label for="command">Command</label>
    <div class="console-row">
      <input id="command" name="command" type="text" autocomplete="off" autocapitalize="off" spellcheck="false">
      <button type="submit" class="button button--primary">Send</button>
    </div>
    <p class="hint">…</p>
  </form>
  <div class="log" tabindex="0" role="region" aria-label="Console history">
    <ol class="console-history">
      <li>
        <p class="console-command"><span class="visually-hidden">You sent </span>/players online</p>
        <pre>Online players (2): …</pre>
      </li>
    </ol>
  </div>
</section>
```

Classes and their contract:

- `log`: the scroll box. Holds one `pre` (the game log) or one `ol.console-history`. It
  must keep `tabindex="0"`, `role="region"` and an `aria-label`, because a
  scrolling box needs a name and a keyboard stop. Nothing inside it may set
  its own width; lines wrap.
- `log-frame`: optional wrapper, used only when the box carries a "Jump to
  newest" button. It is a grid: the box, then the button under it, at the
  end edge, so the button never covers a line (at 320 px an overlay hid the
  last lines). While `hidden`, the button is `visibility: hidden` and keeps
  its row, so showing or hiding it moves nothing below it, and it is out of
  the tab order and the accessibility tree. The script toggles the
  button's `hidden` attribute. The markup is unchanged: the button stays a
  sibling of `log`.
- `console-row`: the field and "Send" on one row. They sit side by side
  and wrap onto two lines when the panel is too narrow. The field's text is
  mono at 16px, so a phone does not zoom in on focus.
- `console-history`: the `ol` of past commands, newest last.
  `console-command` is the sent command, bold, with a drawn "> " prompt
  (ignored by screen readers) and a visually hidden "You sent" in front, so
  a command and its reply are told apart without colour. The reply is a
  `pre` in the same `li`.
- `button[hidden]` is hidden even though `.button` sets `display`.

- A deep sunken box: background `deep`, `box-shadow: var(--bevel-sunken)`,
  1px `border`, text `text`, `--font-mono` at `--text-sm`,
  `white-space: pre-wrap` on the `pre`, `overflow-wrap: anywhere`, so lines
  wrap and never scroll sideways. (The `ol` of the console history keeps
  normal white space, or the markup's line breaks would show as blank lines.)
- The game log's box has a fixed `block-size: min(60vh, 32rem)` and scrolls
  vertically. The console's history box is only as tall as what it holds,
  up to `min(40vh, 20rem)`, then scrolls (section 6). Both are focusable so
  keyboard users can scroll them.
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

### 8.6 Generate (SPEC 5.6)

The task is "make a new world". From the Server page it takes three
clicks: Worlds, "Generate world" (beside "Import worlds" on the Worlds
page), then "Generate world" on the form. Everything but the name has a
default.

One page, `GET /generate`, one `form.panel`, in this order:

1. Name (7.2, required, the only field with no default).
2. Game version: the same picker as Import (SPEC 10.7): downloaded
   versions and Docker Hub's list, with the "download the game image"
   checkbox. Preview needs a downloaded image; its refusal (8.8) covers
   that.
3. Space Age: a checkbox, checked when the version supports it, as on
   Import.
4. "How to make the map": a `fieldset` of three radios, each followed
   in order by its own `fieldset` of fields:
   - **Preset** (checked): a `select` of the game line's presets with
     "Default" selected, and a Seed field (mono, `inputmode="numeric"`,
     `20ch`, hint "Leave blank for a random seed").
   - **Map exchange string**: a mono `textarea`, 6 rows,
     `spellcheck="false"`, `autocapitalize="off"`. Its hint says where
     the game shows one. A bad string is refused on this page with the
     string's version and the reason (7.2 error).
   - **Custom settings**: the map-gen form (8.7).
5. The action row: "Generate world" (primary), "Preview map" (secondary,
   8.8), then a Cancel link back to the Worlds page.

CSS shows only the chosen mode's `fieldset`
(`form:has(#mode-preset:checked) .mode-exchange { display: none }` and
so on); `display: none` also takes hidden fields out of the tab order.
Without `:has()` all three show, and the server reads only the chosen
one, so the page still works with no script. After a failed submit the
chosen radio stays chosen and every value is kept.

On success the browser lands on the new world's page with a Done message
(7.3): "Generated Family base 2026. Its map is made when it first
starts." The next action, Switch, is already on that page.

### 8.7 The map-gen form (SPEC 5.6, 11.5)

Plain HTML, no script. It is long, so it is grouped and mostly closed.

```html
<fieldset class="mode-custom">
  <legend>Custom settings</legend>
  <fieldset><legend>Map</legend> … seed, width, height, starting area,
    water, cliffs, peaceful mode …</fieldset>
  <details open>
    <summary>Nauvis: resources and terrain</summary>
    <fieldset class="autoplace">
      <legend>Iron ore</legend>
      <div class="field"><label for="nauvis-iron-ore-frequency">Frequency</label>
        <select id="nauvis-iron-ore-frequency" name="nauvis.iron-ore.frequency">…</select></div>
      … size, richness …
    </fieldset>
    …
  </details>
  <details><summary>Vulcanus: resources and terrain</summary> … </details>
</fieldset>
```

- **Groups.** First a "Map" fieldset, always open: seed, width and
  height, starting area, water (scale and coverage), cliffs (frequency
  and continuity), and peaceful mode. Then one `details` per planet the
  world will have, in the game's order (only Nauvis on 1.1 or without
  Space Age, and then the summary says just "Resources and terrain").
  The first planet is open, the others closed. A `details` holding a
  field with an error is rendered `open`, so the error summary's link
  (7.2) reaches a visible field.
- **One fieldset per autoplace control**, its legend the game's name
  for it ("Iron ore", "Trees", "Enemy bases"). Its fields (frequency,
  size, richness; two where the control has no richness) sit in a grid,
  `repeat(auto-fit, minmax(8rem, 1fr))`, sized by the container: one
  row on a wide panel, stacked at 320 px. A screen reader hears "Iron
  ore, Frequency, 100%".
- **Selects of named steps, not sliders or free numbers.** Frequency,
  size, richness, starting area, water and cliffs are each a `select` of
  the game's own steps for that game line, read from the game data and
  labelled as the game labels them, with the default step selected and
  marked "(default)". Where the game allows zero, "None" is first. A
  range input is hard to set exactly from the keyboard and hides its
  value; a free number lets in values the game refuses.
- **Numbers.** Seed, width and height are text inputs with
  `inputmode="numeric"`, mono, `20ch`. Width and height hint "0 means
  no limit". Peaceful mode is a checkbox (7.2).
- The form opens filled with the chosen preset's values for the game
  line, so "Custom" means "a preset, then changed". Choosing another
  preset in the Preset mode refills this form (the select posts to the
  refresh route; only the seed is kept), and a hint at the top of the form
  says which preset it holds. A preset value that is on none of the game's
  steps (rail world's water coverage of 150%, ribbon world's cliff
  continuity of 75%) shows as an extra step in that one select, so opening
  the form never changes a preset. Keys the fields do not cover, such as an
  island's terrain type, fill the raw editor below. Without script the form
  opens at the defaults and the person sets what they want.
- **Words for terrain.** A terrain control (water, trees, rocks, moisture)
  has Scale and Coverage, as the game's terrain tab words them; Scale is
  the inverse of the stored frequency. Resources and enemy bases have
  Frequency and Size, resources also Richness; cliffs Frequency and
  Continuity.
- **Raw map-gen JSON** (SPEC 11.5). After the planet `details`, a closed
  `details` "Other map-gen settings (JSON)" holds the raw editor of 8.10
  (mono `textarea`, `wrap="off"`, errors by line in the error summary,
  `details` rendered `open` when it has an error). It is prefilled with
  the game line's defaults for only the keys the form does not cover.
  On submit the raw object is read first and the form fields are laid
  over it, so a form field wins for every key it covers; a raw key the
  form covers is ignored, and the hint says so.

### 8.8 Map preview (SPEC 5.6, P1)

Preview is optional and adds no screen to Generate.

- "Preview map" is a secondary submit button in Generate's action row.
  It posts the same form with `formaction="/generate/preview"`.
- The answer is **the same Generate page**, every value kept, a blank
  seed now holding the random seed the picture used, and a "Map preview"
  `section` (h2) directly above the action row. The pictures sit next to
  the two buttons that follow from them: change a field and press
  "Preview map" again, or press "Generate world" to make exactly this
  map. There is no separate "Back" or "Use these settings" step. The
  section's first line: "Made from the settings above, seed 123456.
  Change anything and preview again to see it."
- **Pictures.** A `ul` (no bullets) of `figure`s, one per planet, each
  with a `figcaption` naming the planet. The `img` has `width` and
  `height` attributes, `inline-size: 100%`, `max-inline-size: 32rem`
  and `aspect-ratio: 1`, so its space is held before it loads. Alt text:
  "Map preview of Nauvis, seed 123456". The list is a grid,
  `repeat(auto-fit, minmax(16rem, 1fr))`: one picture per row on a
  phone, two or more on a wide panel. Each picture links to its
  full-size file.
- **While it runs** (about one second per planet). Without script the
  browser's own loading state shows and the page arrives complete. With
  htmx, the button posts with `hx-target` on the preview section; the
  section reserves the height of one picture and shows "Making the map
  preview for 5 planets, about 5 seconds." with an indeterminate
  `<progress>` (7.6). Both action buttons are marked `aria-disabled`
  while it runs, never `disabled`, because a disabled button loses focus
  and the browser does not give it back; a click on a busy button does
  nothing, which also stops the Enter key's implicit submit. When the
  pictures arrive focus is still on Preview map and `#announcer` says
  "Map preview ready." A redraw that leaves the form in place removes an
  old error summary, so focus never lands on errors already fixed. If the
  request itself fails (a 4xx or 5xx answer, or no connection), htmx swaps
  nothing, so the script puts a sentence in the preview section and in
  `#announcer`: "The map preview request failed..." The sentence comes
  from a `template` in the page, so it is translated with the page.
- **Refusals** are errors (7.3) in the preview section, each with its
  next step as a control: the game image is not downloaded (a "Download
  2.0.77" button that posts the Versions page's download action); another preview is
  running ("Try again in a few seconds", with Preview map right below);
  the game failed (its last log lines in a `pre`; Generate still works).
- Pictures are deleted after 15 minutes. An expired preview says "This
  preview expired. Preview map again." and the images are dropped.

### 8.9 Clone (SPEC 5.7, P1)

A world's page has "Clone world" (secondary). It opens
`GET /worlds/{id}/clone`, a plain `form.panel`, not the danger box,
since nothing is lost. From the Server page: the active world's link,
"Clone world", then "Clone world" on the form: three clicks.

- Heading "Clone Family base 2025", then a short `ul` of what is copied:
  "Its 38 mods, its settings, game version 2.0.77 and Space Age."
- Name, filled in with "Family base 2025 (copy)".
- A "Map" `fieldset` with two radios:
  - **Keep the save** (checked when the world has one). The label names
    the save and its time: "Keep autosave_3.zip, saved `<time>`", using
    section 9's time rules. It is the save the world would load (SPEC
    7.11).
  - **Start a fresh map**, hint "A new map with a random seed, made when
    the world first starts."
  A world with no save shows only the fresh-map line, as text, not a
  lone radio.
- "Clone world" (primary) and a Cancel link back to the world. The
  result is the new world's page with a Done message naming both worlds.

### 8.10 Game settings (SPEC 11)

A world's page and the Server page (for the active world) each have a
"Settings" link. From the Server page, changing a setting takes three
clicks: Settings, "Preview changes", "Save settings", plus Restart when
the game is running.

**The form** (`GET /worlds/{id}/settings`) is one `form.panel` of
fieldsets, in this order:

- "Listing": name, description (`textarea`, 3 rows), and visibility as
  two radios, "On the local network" and "Hidden".
- "Joining": game password (below), maximum players (numeric, hint "0
  means no limit"), require user verification (checkbox).
- "Saving": autosave interval in minutes, autosave slots, pause when no
  one is online (checkbox).
- "Admins": a `textarea`, one player name per line, as its hint says. On
  a world that cannot be changed it is `readonly`, not disabled, so the
  keyboard can focus and scroll a long list; the other controls stay
  disabled.

Then "Preview changes" (primary) and Cancel. Below the form, one plain
link per editable file: "Edit server-settings.json as JSON".

**Write-only fields** (SPEC 11.4). No value ever reaches the browser.

```html
<fieldset>
  <legend>Game password</legend>
  <p class="hint" id="pw-state">A password is set.</p>
  <div class="field">
    <label for="pw-new">New password</label>
    <p class="hint" id="pw-hint">Leave blank to keep the current password.</p>
    <input id="pw-new" name="game_password" type="password"
           autocomplete="new-password" aria-describedby="pw-state pw-hint">
  </div>
  <label class="check"><input type="checkbox" name="game_password_remove"> Remove the password</label>
</fieldset>
```

With no password set, the state reads "No password is set. Anyone who
can reach the server can join.", the hint "Leave blank to keep no
password", and there is no remove checkbox. A new password and remove
together is a field error: "Enter a new password or remove it, not
both." The factorio.com username, password and token follow the same
pattern if a page ever shows them.

**The raw editor** (`GET /worlds/{id}/settings/raw/{file}`):

- One `textarea` in `--font-mono` at 16px, `rows="24"`, `wrap="off"`,
  `spellcheck="false"`, `autocapitalize="off"`, `autocomplete="off"`,
  full panel width. Long lines scroll inside it; the page never scrolls
  sideways. No line-number gutter and no syntax colour: both need script.
- Secret keys are left out; the hint says "The game password is not
  shown here. It is kept unless you change it on the form."
- Errors use the error summary (7.2), all at once. Each names the line,
  the reason and the line's text: "Line 12: max_players must be a whole
  number. The line reads `"max_players": "ten",`". The `textarea` gets
  `aria-invalid` and `aria-describedby` pointing at the summary list,
  and keeps the text exactly as typed.
- "Preview changes" (primary) and Cancel, as on the form.

**The preview of changes** follows the 7.4 structure in a plain panel,
not the danger box, because the snapshot makes it undoable.

- Heading: "Save these changes to Family base 2025?"
- A `ul.changes`, one `li` per key: a bold leading word, "Added",
  "Changed" or "Removed" (like a step word in 7.6, so the kind never
  rests on colour), the key in `code`, then `<del>` old value and
  `<ins>` new value on lines of their own, each led by a visually hidden
  "Before:" or "After:". Values wrap with `overflow-wrap: anywhere`; no
  red or green fills. A secret shows no value, only "will be set",
  "changed" or "removed". An unknown key adds "Foreman does not check
  this key; it is saved as written." Admins read "Added admin Alice",
  "Removed admin Bob".
- What will happen, in order: save the running game (only when it
  runs), take a snapshot labelled for the settings change, write the
  file. Then the undo line naming the Snapshots page.
- When the world is running, a Note (7.3): "The server is running. These
  changes take effect after a restart." When only admins changed, it
  says instead that admins change at once.
- "Save settings" (primary) and "Go back and edit", a link back to the
  form or raw editor with the edits kept.
- No changes: "Nothing changed." with a link back and no save button.
- If the file changed since the preview, confirming shows this page
  again with a Warning: "server-settings.json changed since you
  previewed. These are the changes against the file as it is now." The
  Warning takes focus, as the "nothing left" note does, so the new diff
  is announced.

**After saving**, the browser returns to the settings form with a Done
message naming the snapshot, linked. When a restart is needed the
message ends with a "Restart server" link to the restart form (7.4), so
the next step is a control.

## 9. Writing

- Plain words, sentence case, no exclamation marks, no "Oops".
- Name things the way the game and the spec do: world, save, snapshot,
  mod, RCON, "InGame".
- Errors say what happened and what to do next, in that order.
- Never show a secret: no RCON password, token or game password appears
  in any message, field value or log view (SPEC 14.8).
- Numbers carry units ("2.1 GB", "60 seconds"), and times say whose zone
  when it matters (scheduled restarts).
- Times (SPEC 17.11): the server writes UTC in a `time` element with a
  `datetime` attribute, and `time.js` rewrites it with `Intl` into the
  viewer's zone, such as "Nov 3, 2:25 PM EST (2 days ago)". The year shows
  only when it is not the current year. The relative part is visible text
  only and is refreshed every minute. A time inside `.visually-hidden`
  text, such as a link's spoken name, gets the absolute local time and no
  relative part, so the name stays short and never goes stale. Without
  JavaScript the UTC text stays. A table cell keeps its date and clock in
  separate `nowrap` spans, and the zone is its own unit after the
  clock, in each language's order (8.3). Durations are not rewritten.
- Text and translations (SPEC 22): every visible word comes from the
  catalogue in eight languages. docs/I18N.md holds the glossary, the
  address rule per language and the rules for writing a string. Leave room
  for text about 40 percent longer than English, and put no text in CSS.

## 10. Checks every screen passes

The end-to-end tests for each screen check, in both themes
(`colorScheme: 'dark'` and `'light'` in Playwright):

- an axe scan with no violations (SPEC 17.1);
- at 320 px wide, no horizontal scroll (6, rule 1);
- every button and field at least 44 by 44 CSS pixels;
- the page can be used with the keyboard alone, with a visible focus ring;
- state badges contain their word.

## 11. CSS file layout

One stylesheet, `internal/web/static/foreman.css`, in this order: tokens
(dark on `:root`, then the light media query, then the two bevel
tokens), a small reset, base elements
(`body`, headings, links, `code`, `table`), the shell, then one block per
component in the order of section 7. No preprocessor, no `@import`.

## 12. Deliberately deferred

Left out because no Epic 1 to 3 screen needs it. Add each when a real
screen does, in this document first.

- A manual theme switch. The system setting decides (SPEC 17.3).
- Icons of any kind.
- Modal dialogs, pop-overs, menus, tab panels and toasts. (The nav links
  only look like tabs; they are ordinary links.) The confirmation step
  (7.4) and the message (7.3) cover Epics 1 to 3.
- Charts for CPU and memory. Numbers in the status `dl` are enough for
  SPEC 7.2.
- Pagination. See 8.4.
- Mod search results, dependency plans and the mod list (Epic 8).
- The world-switch progress screen beyond the step list in 7.6 (Epic 4).
- Chat (Epic 9).
- A print stylesheet.
