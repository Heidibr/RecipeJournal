# Recipe Journal

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
