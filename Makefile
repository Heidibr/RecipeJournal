.PHONY: dev css css-watch build clean test tidy

# Run the server with a freshly built CSS bundle.
dev: css
	go run ./cmd/server

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
