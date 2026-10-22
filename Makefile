.PHONY: dev css css-watch build clean test tidy logos

# Run the server with a freshly built CSS bundle.
dev: css
	go run ./cmd/server

# Re-copy the served logo/icon files from the design pack. Run this whenever
# the pack in den-lille-kokeboken-logo/ is replaced.
#
# It mirrors every SVG in the pack rather than naming them one by one, so a
# renamed file does not break the refresh -- it just shows up under a new
# name. Files the templates reference are listed in README; if the pack
# renames one of those, the template has to be updated too, and the orphan
# warning below is what tells you.
LOGO_SRC := den-lille-kokeboken-logo
logos:
	@test -d $(LOGO_SRC)/svg || { echo "no $(LOGO_SRC)/svg -- is the pack unpacked?"; exit 1; }
	@cp $(LOGO_SRC)/svg/*.svg static/img/
	@test -f $(LOGO_SRC)/favicon.ico && cp $(LOGO_SRC)/favicon.ico static/favicon.ico || echo "  warn: favicon.ico missing from pack"
	@for s in 180 192 512; do \
	   for v in app-ikon-$$s app-ikon-mork-$$s; do \
	     test -f $(LOGO_SRC)/png/app-ikon/$$v.png \
	       && cp $(LOGO_SRC)/png/app-ikon/$$v.png static/img/ \
	       || echo "  warn: $$v.png missing from pack"; \
	   done; \
	 done
	@echo "logos refreshed from $(LOGO_SRC)"
	@echo "files the templates reference:"
	@for f in $$(grep -rhoE '/static/img/[a-z0-9.-]+' internal/ui/templates | sed 's|/static/img/||' | sort -u); do \
	   if [ -f $(LOGO_SRC)/svg/$$f ] || [ -f $(LOGO_SRC)/png/app-ikon/$$f ]; then \
	     echo "  ok     $$f"; \
	   elif [ -f static/img/$$f ]; then \
	     echo "  STALE  $$f  <- served copy only; the pack no longer has this name"; \
	   else \
	     echo "  BROKEN $$f  <- not in the pack and not served"; \
	   fi; \
	 done

# One-shot Tailwind build.
css:
	./tailwindcss -i static/css/input.css -o static/css/styles.css --minify

# Watch templates for changes and rebuild CSS live.
css-watch:
	./tailwindcss -i static/css/input.css -o static/css/styles.css --watch

build: css
	CGO_ENABLED=0 go build -o recipejournal ./cmd/server

test:
	go test ./...

tidy:
	go mod tidy

clean:
	rm -f recipejournal recipes.db recipes.db-shm recipes.db-wal static/css/styles.css
