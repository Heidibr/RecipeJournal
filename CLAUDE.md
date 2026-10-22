# Working in this repo

Two systems here are easy to break by accident, because nothing fails loudly
when you do. Read these before touching UI or copy.

## Colours

Tokens live in `static/css/input.css` and are documented in README under
"Colours". Use token names in templates (`text-brick`, `border-ribbon`), never
a raw hex.

Three signals, and a colour always means the same thing:

- **`ribbon` (green) — structure and action.** The logo, links, filled
  buttons, active chips, focus rings, success notices. `pine` is its
  hover/pressed state.
- **`terracotta` — content.** Accents on what the user wrote, and notices that
  mean "you need to do something".
- **`brick` — problem.** Error messages, delete, revoke, anything destructive.

Anything else is neutral: `ink`, `muted`, and the surface colours. If a new
element fits none of the three, it is neutral. Do not invent a fourth signal.

**`ink` is a text colour, never a fill.** No black buttons, chips or selected
states — those are `ribbon`. Notice backgrounds use `sage-light` (confirmation)
and `clay-light` (attention).

Two pairs are forbidden because their luminance is nearly identical:
`brick` on `ribbon` (1.02:1) and `terracotta` immediately beside `muted`
(1.05:1). Both differ in hue but not in weight, so they fail in greyscale and
for colour-blind readers.

New colour on a cream background needs 4.5:1 to carry body text, and the
boundary of an interactive control needs 3:1 (WCAG 1.4.11). Check it rather
than eyeballing it. This is why form-field underlines stay on
`border-muted/40` instead of `sand`: sand measures 1.25:1 on paper, which is
fine for a decorative line and not fine for the edge of a text field.

## Shape and type

The logo is thin rounded strokes with one filled accent. The UI follows it:

- **Lines, not shadows.** 1px `border-sand`. No box-shadows.
- **Cards:** `border border-sand rounded-xl` (12px). Buttons and fields are
  less round (`rounded-md`, 6px).
- **Notices keep a 4px coloured left edge** — that edge is the signal, so it
  stays `ribbon` or `terracotta`. Cards never have one.
- **Icons:** linear, `stroke-width="1.5"`, `stroke-linecap="round"`.
- **Lists:** ingredients use `.dotted-list`, which draws the logo's 5px filled
  terracotta dot. Numbered steps keep their numbers — the order is information
  — with the numerals in terracotta.
- **Type:** `"Helvetica Neue", Helvetica, Arial, sans-serif`, weight 300.
  Small uppercase labels go to 400; avoid 600–700. `h1` gets
  `letter-spacing: -0.02em`. No font files are bundled, and no webfont CDN is
  used -- do not add one without a licence.
- **Logo:** `make logos` refreshes the served copies from the pack and reports
  renames. Height only, never stretched, no shadow or recolouring, and clear
  space of at least half the mark's height. Files in `static/img/`, source
  pack in `den-lille-kokeboken-logo/`. README has the full table.

Rebuild the bundle after editing styles or adding a class: `make css`
(`make dev` does it for you).

## User-facing text

Every string a user sees comes from the catalogue in
`internal/i18n/catalog.go`, in **both** English and Norwegian. In templates it
is `{{T "some.key"}}`, or `{{Tf "key" .Value}}` when a value is interpolated.
Never hardcode a visible string in a template or handler.

`go test ./internal/i18n/` fails if a key exists in one language but not the
other, or if the `%s`/`%d` verbs differ between them. Run it after adding
strings.

**Never translate what the user typed.** Recipe names, ingredients, process,
wine pairings and notes are shown verbatim, always. The same goes for any
category the user created or renamed — that is what the nullable
`categories.i18n_key` tracks: a key means "this is one of our seeded defaults,
translate it", and NULL means "this is the user's own words, leave it alone".
`store.UpdateCategoryName` clears the key for exactly this reason.

Translation is bound at template-parse time (one template set per language in
`internal/ui/ui.go`), not passed through request data, so HTMX partials
rendered against a bare model get the right language too. If you add a
template func that depends on language, add it in `langFuncs`.

## Static files and the service worker

`static/` is served from disk, not embedded. Two things follow:

- Adding a file the app shell needs means adding it to `SHELL_URLS` in
  `static/sw.js` **and** bumping `SHELL_CACHE` — installed clients keep the old
  list otherwise, and a precached URL that 404s makes the whole install fail.
- The Dockerfile copies `static/` from the *build* stage and only
  `styles.css` from the css stage. Copying all of `static/` from the css stage
  (which holds only `static/css`) is what once left htmx, the service worker,
  the manifest and every image 404 in production.

## Database

Schema changes go in a new numbered migration under
`internal/store/migrations/`. They run at server startup, in order, once each.
Never edit an applied migration, and never delete the local `recipes.db` — it
holds real recipes.
