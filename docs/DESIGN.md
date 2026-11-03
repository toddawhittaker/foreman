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
| Inner panel (a section's content) | sunken | none |
| Field, log, console | sunken | 1px `border` (the edge that counts for contrast) |
| Button | raised; sunken while pressed (`:active`) | 1px border, see 7.1 |
| Message, badge, confirmation box | none (flat) | 1px border in their own colour |

The bevel is decoration. In forced-colours mode box-shadows are not
drawn, and the 1 px borders in the "Outline" column are what remain.
That is why every frame and control keeps one.

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
  keeps a 1 px border (section 4, "Bevels") so its edge shows too.

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
nothing animates.

- Hover is a colour change only; it is never the sole sign of anything,
  because a phone has no hover. The secondary button's hover is a
  lighter grey, so orange keeps meaning only "selected".

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
| stopped | "Stopped" | `text-muted` | `deep` | Hollow square |
| starting | "Starting" | `warn` | `warn-bg` | Square, left half filled |
| running | "Running" | `ok` | `ok-bg` | Filled square |
| stopping | "Stopping" | `warn` | `warn-bg` | Square, right half filled |
| crashed | "Crashed" | `danger-text` | `danger-bg` | Filled diamond (the square turned 45 degrees) |
| crash-looping | "Crash-looping" | `on-danger` | `danger` | Filled square; the whole badge is solid |
| unknown | "Unknown" | `text-muted` | `deep` | Hollow circle; the badge edge is dashed |

"Unknown" is for when Foreman cannot ask Docker (Epic 3 R16). It uses the
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
  <li><span class="step-state step-state--done">Done</span> Snapshot taken</li>
  <li aria-current="step"><span class="step-state step-state--progress">In progress</span> Waiting for the game to reach InGame
    <progress aria-label="Waiting for the game"></progress></li>
  <li><span class="step-state">Waiting</span> Check RCON answers</li>
</ol>
```

Step words are "Done", "In progress", "Waiting", "Failed" and "Skipped",
coloured `ok`, `warn`, `text-muted`, `danger-text` and `text-muted`. The
in-progress step may carry a native `<progress>` with no value
(indeterminate). A failed step is followed by an error message (7.3)
that says what to do next, as SPEC 7.9 requires for switching.

Classes: `steps` (the `ol`; no numbers, one row per step), `step-state`
(the word, a fixed 6rem column so the step text lines up) and its
modifiers `step-state--done`, `step-state--progress` and
`step-state--failed`. "Waiting" and "Skipped" take no modifier. The
current step has `aria-current="step"` and is bold. A failed step's
message goes inside that `li` as `<div class="message message--error">`;
it takes its own full-width line. The words carry the state, so forced
colours needs no rule.

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
square) beside its name, and is listed first. The word carries the
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
`datetime` attribute; the displayed format is Epic 2's choice.

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

Console, above its history:

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
  newest" button. It is `position: relative` and the button is drawn over
  the box's lower corner (not fixed to the viewport, so 6, rule 7 holds),
  which means showing or hiding it moves nothing. The script toggles the
  button's `hidden` attribute.
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
- Mod search results, dependency plans and the mod list (Epic 5).
- The world-switch progress screen beyond the step list in 7.6 (Epic 4).
- Chat (Epic 8).
- A print stylesheet.
