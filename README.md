# Den lille kokeboken

A personal, Moleskine-inspired recipe journal — written in Go, served as HTML
with HTMX, persisted in SQLite.

## Stack

- **Go** (chi router, `html/template`, `database/sql`)
- **SQLite** via `modernc.org/sqlite` (pure-Go, no CGO)
- **HTMX** for in-place editing, **Tailwind CSS v4** for styling
- **alexedwards/scs** for cookie sessions, **bcrypt** for passwords
- Deploys as a single binary on Fly.io

## Run it locally

Prereqs: Go 1.23+ and the included `tailwindcss` binary (downloaded once
to the repo root; it is git-ignored).

```bash
make css      # build static/css/styles.css
make dev      # builds CSS then runs go run ./cmd/server
```

The server boots on `http://localhost:8080` and writes `./recipes.db` next to
the binary.

### Iterating on the UI

```bash
make css-watch    # in one terminal — Tailwind watches templates
go run ./cmd/server   # in another
```

## Layout

```
cmd/server/          entry point
internal/auth/       signup, login, sessions, middleware
internal/handlers/   HTTP routes wiring auth + store + ui
internal/models/     domain types
internal/store/      SQL + migrations (embedded)
internal/ui/         templates (embedded) + renderer
static/              CSS, JS (htmx), images served from disk
```

## Colours

Defined once as Tailwind tokens in `static/css/input.css`. Use the token names
(`text-brick`, `border-ribbon`), never a raw hex in a template.

**Three signals.** A colour always means the same thing, so a reader learns it
once:

| Signal | Token | Use it for |
| --- | --- | --- |
| Structure | `ribbon` (green) | the logo, links, **filled buttons**, active chips, focus rings, success notices |
| Content | `terracotta` | accents on what the user wrote, and "you need to do something" notices |
| Problem | `brick` | error messages, delete, revoke — anything destructive |

Anything that is none of the three is neutral: `ink` for body text, `muted` for
secondary text, `cream`/`paper`/`sand` and the two light tints for surfaces.
A new element that does not fit a signal is neutral — don't invent a fourth.

**The split in practice.** Green carries the structure, warm carries the
content. A recipe card is bounded by a thin `sand` line; the step numbers
inside the method are terracotta because they belong to the recipe.

**Cards vs notices.** A card is a container: 1px `border-sand`, `rounded-xl`,
no shadow. A notice is a message: it keeps a 4px coloured left edge, and the
colour is the signal — `ribbon` for a confirmation, `terracotta` for something
the reader has to act on. Only `layouts/base.html` draws notices.

**Form controls are the one exception to sand.** Field underlines stay on
`border-muted/40`. `sand` on paper measures 1.25:1, and WCAG 1.4.11 asks for
3:1 on the boundary of an interactive control, so a sand underline would make
the fields effectively invisible. Decorative lines may be faint; the edge of
something you type into may not.

**No black fills.** `ink` is a text colour. Buttons, active chips and selected
states are `ribbon`, pressing/hovering goes to `pine`, and the light tints
(`sage-light`, `clay-light`) carry notice backgrounds. The app had 24 black
fills before this; it has none now.

**Combinations that work**

| Foreground | On | Ratio | Notes |
| --- | --- | --- | --- |
| `ink` | `cream` / `paper` | 15.4:1 | body text |
| `muted` | `cream` / `paper` | 5.8:1 | labels, captions |
| `ribbon` | `cream` / `paper` | 7.5:1 | links, headings |
| `terracotta` | `cream` / `paper` | 5.5:1 | accents |
| `brick` | `cream` / `paper` | 7.3:1 | errors |
| `cream` | `ribbon` / `pine` / `terracotta` / `brick` / `cocoa` / `sage` | 5.3–10.6:1 | any filled button |
| `ink` | `sand` / `sage-light` / `clay-light` | ~13:1 | tinted panels, notices |
| `cream` | `ribbon` → `pine` on hover | 7.5 → 10.6:1 | the standard button |
| `sage` border | `paper` | 5.8:1 | outlined controls (clears the 3:1 rule) |
| `terracotta` dot | `paper` | 6.1:1 | difficulty dots, list bullets |

Every pair above clears WCAG AA for body text (4.5:1).

**Two pairs to avoid.** Both have nearly identical luminance, so they differ in
hue but not in weight — they vanish in greyscale and for colour-blind readers:

- `brick` on `ribbon` (1.02:1) — never red text on a green surface, or the
  reverse. Keep `brick` on cream, where it is 7.3:1.
- `terracotta` next to `muted` (1.05:1) — readable separately, but equal weight
  side by side. Separate them with size or weight, not colour alone.

**Spare tokens.** `pine` (hover/pressed on a green surface), `sage` (secondary
green, borders), `cocoa` (brown bridge, deep borders) and the light tints are
defined but not yet used. Tailwind v4 only emits utilities the templates
actually reference, so an unused token costs nothing in the bundle.

## Typography

One family throughout, Light as the default weight:

```css
font-family: "Helvetica Neue", Helvetica, Arial, sans-serif;
```

| Role | Weight |
| --- | --- |
| Body, most UI | 300 |
| Headings | 300, with `letter-spacing: -0.02em` on `h1` |
| Buttons, tabs, small labels (under ~14px) | 400 |
| Bold (600–700) | avoid unless something must break out |

`body` is set to 300 in the base layer, and `.uppercase` steps to 400 — every
small label in this app is `uppercase tracking-widest`, so that one rule covers
them. A `font-*` utility on an element still wins over both.

**Helvetica Neue ships on Apple devices only.** Everywhere else this falls back
to Arial, which has no 300 weight and renders at 400, so Windows and Android
see a slightly heavier, wider UI. No font files are bundled — we have no
licence for them. The tokens `--font-display` and `--font-body` both point at
the same stack; they are kept separate so a display face can be introduced in
one place later.

## Logo and icons

Source pack: `den-lille-kokeboken-logo/` (kept in the repo, excluded from the
Docker build context). The files the app serves are copied into `static/img/`.

Run `make logos` after replacing the pack. It mirrors every SVG plus the icon
PNGs into `static/img/`, then prints each file the templates reference and
flags any that the pack no longer carries — which is how a rename shows up
instead of a broken image.

| File | Used for |
| --- | --- |
| `logo-en-linje.svg` | header at `lg` and up, 32px tall |
| `logo-to-linjer.svg` | header `sm`–`lg`, where one line would wrap |
| `logo-merke.svg` | header on phones, where the name does not fit |
| `logo-stablet.svg` | login, signup, empty state |
| `*-mork-bakgrunn.svg` | the same, for dark `pine`/`ribbon` surfaces — not used yet |
| `favicon.svg`, `favicon.ico` | favicon; the `.ico` is also served from `/favicon.ico` |
| `app-ikon-mork-180.png` | `apple-touch-icon` |
| `app-ikon-mork-192/512.png`, `app-ikon-mork.svg` | web manifest, including `maskable` |
| `app-ikon*.png`, `app-ikon.svg` | the light variant, on a `ribbon` ground — available, unused |

The app icon comes in two grounds: the unsuffixed files sit on `ribbon`
(#2f5540), the `-mork` files on `pine` (#1f3d2b). The manifest uses `-mork`
because `background_color` is #1f3d2b, so the icon melts into the splash
screen instead of drawing a slightly different green square on it. A manifest
cannot carry both, so this is a choice, not a default.

Rules: never stretch it (height only, width `auto`), no shadow, gradient or
recoloured variants, and keep clear space of at least half the mark's height on
every side. In the header that works out exactly: a 32px logo with 16px header
padding and a 16px page gutter.

The wordmark in `logo-horisontal.svg` and `logo-stablet.svg` is **live `<text>`,
not outlines**, set in the same Helvetica Neue stack at weight 300. On a
non-Apple device it renders in Arial, so the logo itself changes shape — see
"Typography".

## Deploy to Fly.io

One-time setup:

```bash
brew install flyctl
fly auth login
fly launch --copy-config --no-deploy   # accept the app name when prompted
fly volumes create recipes_data --size 1 --region ams
```

Then any deploy is:

```bash
fly deploy
```

The Dockerfile builds CSS, compiles the binary statically (`CGO_ENABLED=0`),
and runs it from a distroless image as a non-root user. SQLite lives on the
mounted `recipes_data` volume at `/data/recipes.db`.

## Default categories

When a user signs up, twelve categories are seeded:

> Breakfast & Brunch · Appetizers & Snacks · Main Dishes · Side Dishes ·
> Soups & Stews · Desserts & Baking · Beverages · Family Favorites ·
> Quick & Easy · Recipes to Try · Vegetarian · Holidays & Special Events

Users can rename, delete, reorder, and add their own at `/categories`.

## Out of scope (v1)

- Recipe photos
- Multiple "books" per user
- Sharing between users
- Search beyond category filtering
- Import / export

These are kept off the initial build to stay focused. Add them after the
journal feels good in daily use.
