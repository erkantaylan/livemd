# LiveMD design

How this app should look and behave, and why. Read this before changing
`static/style.css` or the markup that hangs off it.

## What the app is

A window onto a file you are editing somewhere else. The document is the
product; everything else is a way to get to it and get out of the way. Two
consequences run through every decision below:

- **The document surface outranks the chrome.** When the two compete for
  attention, the chrome loses. Controls earn their contrast by being needed,
  not by being available.
- **Nothing should move on its own.** A live-reloading page that reflows,
  animates, or re-centres while you read is worse than a static one. Updates
  replace content in place and hold scroll position.

## Light only

There is no dark theme, and adding one is a deliberate decision to revisit,
not an oversight to fix. The surface is warm paper: documents are read for
minutes at a time, and a slightly warm off-white is easier on the eye than
pure white without ever looking like a themed "product".

This means no `prefers-color-scheme` blocks, no theme toggle, and no
theme-conditional tokens. One palette, defined once on `:root`.

## Tokens

Every colour, radius and step is a custom property on `:root` in
`static/style.css`. Nothing in the stylesheet should contain a raw hex value
outside that block — if a new colour is needed, it becomes a token or it
reuses one.

### Surfaces

| Token | Value | Use |
| --- | --- | --- |
| `--paper` | `#fdfcf8` | The document. The warmest, lightest surface; nothing else uses it. |
| `--chrome` | `#f2f1ec` | Sidebar, toolbars, footers. Sits back from the paper. |
| `--sunken` | `#e9e7e0` | Row hover, input wells, inset areas. |
| `--raised` | `#ffffff` | Buttons and fields that sit *on* chrome. |
| `--selected` | `#ece3d0` | The selected file row. Warm sand, not blue — it belongs to the paper family. |

### Ink

| Token | Value | Use |
| --- | --- | --- |
| `--ink` | `#1c1a17` | Body text, headings, filenames. |
| `--ink-muted` | `#6b675e` | Paths, timestamps, secondary labels. |
| `--ink-faint` | `#9a958a` | Placeholders, disabled, decorative glyphs. |
| `--rule` | `#e2dfd6` | Hairlines between regions. |
| `--rule-strong` | `#cec9bc` | Borders on interactive things. |

### Meaning

| Token | Value | Use |
| --- | --- | --- |
| `--accent` | `#2b6cb0` | Selection marker, links, focus. The one cool colour, used sparingly. |
| `--accent-weak` | `#dce7f2` | Accent backgrounds. |
| `--danger` | `#a8332a` | Remove, delete, deleted-file state. Warm red, never neon. |
| `--success` | `#3f7a4f` | "Copied", connection live. |
| `--warn` | `#8a6516` | Warnings in the log. |

### Space and shape

A 4px base. Use `--s-1` through `--s-6` (4, 8, 12, 16, 24, 32) rather than
arbitrary pixel values, so vertical rhythm survives edits.

Radii: `--r-sm` 4px for small controls, `--r-md` 6px for buttons and fields,
`--r-lg` 10px for panels. Nothing is fully rounded — pills read as marketing.

## Typography

Two families, both system stacks. No web fonts: this is a local tool that must
render instantly and work offline.

- `--font-ui` — system sans, for all chrome.
- `--font-mono` — system mono, for code, paths, and the log.

The document scale is deliberately tighter than a marketing page. `h1` at
1.75rem, not 2.5rem: a document's own title competes with its content, and
most files start with one.

| Element | Size | Line height |
| --- | --- | --- |
| Document body | 1rem (16px) | 1.65 |
| `h1` | 1.75rem | 1.25 |
| `h2` | 1.375rem | 1.3 |
| `h3` | 1.125rem | 1.4 |
| `h4`–`h6` | 1rem | 1.4 |
| Chrome labels | 12px | 1.4 |
| Log, paths | 11px mono | 1.5 |

### Measure

Prose is capped at `--measure` (72ch) and centred. This is the single biggest
readability win available: without it, a maximised window runs lines past 200
characters and the eye loses its place on every wrap.

Things that are not prose opt out, because narrowing them destroys them:

- Code blocks and tables get `--measure-wide` (96ch) — they scroll rather than
  wrap, and a cramped table is unreadable.
- Media (images, PDF, audio, video) and the HTML preview iframe go full bleed
  via `.media-figure`, `.html-preview`. A PDF squeezed into 72ch is useless.

## Components

### Hit targets

Any control a person clicks is at least **24×24px**, including its padding,
even when the glyph inside is 12px. The folder Refresh button was originally a
bare 12px `⟳` with 4px of padding; it was both hard to hit and hard to see.

Icon-only controls always carry a `title`, because an unlabelled glyph is a
guess until you hover it.

### Controls that appear on hover

Row controls (remove `✕`, folder Refresh `⟳`) are invisible until the row is
hovered or something inside it has keyboard focus. The tree is a reading
surface first: a column of red `✕` glyphs down the right edge competes with
the filenames for attention and makes the list look like a list of errors.

Three rules make this safe:

1. The control occupies its space whether or not it is visible — it fades with
   `opacity`, never `display`, so nothing reflows on hover.
2. It becomes visible on `:focus-within` as well as `:hover`, so it is
   reachable by keyboard.
3. It never overlaps another control. `.folder-name` flexes to fill the row,
   so anything after it is pushed against the absolutely-positioned remove
   button — reserve the width explicitly.

### The tree

Folder chains with a single child collapse into one row: `skills/aspire/
references` rather than four nested rows. Real trees are mostly corridor —
`Desktop/projects/alternet/planets/sharp-skills` cost fourteen rows and
fourteen levels of indent to say nothing. Indentation is 12px per *rendered*
level, so a compacted chain costs one level, not four.

Folder rows are quieter than file rows: the files are the destinations.

### Feedback

State that results from an action appears next to the control that caused it,
not in a corner: `+3` / `nothing new` beside the Refresh button that was
clicked, `Copied!` on the Copy button. It stays for about 2.5 seconds.

Feedback that must survive a re-render lives in JS state keyed by the thing it
describes, never on the DOM node — the element is usually replaced before the
message is read.

## What to avoid

- **A CSS framework.** Bulma was carried for four component styles and cost a
  CDN round trip, a flash of unstyled content offline, and a fight with its
  defaults in half the rules here. The document styling now lives in this repo
  where it can be reasoned about.
- **Animation beyond 150ms**, and none at all on content. Transitions are for
  hover and focus states on controls.
- **New colours outside the token block.** If something needs a colour that is
  not there, the palette is wrong and should change deliberately.
- **Making the chrome prettier at the document's expense.** If a change makes
  the sidebar more striking, it is probably a regression.
