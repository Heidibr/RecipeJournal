# syntax=docker/dockerfile:1.7

# ---- Tailwind stage: build the CSS bundle ---------------------------------
FROM debian:bookworm-slim AS css
ARG TARGETARCH
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY static/css/ ./static/css/
COPY internal/ui/templates/ ./internal/ui/templates/
RUN case "$TARGETARCH" in \
      amd64) TW=tailwindcss-linux-x64 ;; \
      arm64) TW=tailwindcss-linux-arm64 ;; \
      *) echo "unsupported arch $TARGETARCH"; exit 1 ;; \
    esac \
 && curl -fsSL -o /usr/local/bin/tailwindcss \
      "https://github.com/tailwindlabs/tailwindcss/releases/latest/download/${TW}" \
 && chmod +x /usr/local/bin/tailwindcss \
 && tailwindcss -i static/css/input.css -o static/css/styles.css --minify

# ---- Go build stage -------------------------------------------------------
FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# ---- Runtime stage --------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/server /app/server
# Everything under static/ comes from the build stage, which has the whole
# repo: js, img, sw.js, the manifest. The css stage only carries the Tailwind
# bundle, so copy that single file over the top -- it is .dockerignore'd and
# therefore absent from the build stage.
#
# This used to copy all of /app/static from the css stage, which only ever
# contained static/css. htmx, the service worker, the manifest and every
# image were 404 in production as a result.
COPY --from=build /src/static/ /app/static/
COPY --from=css /src/static/css/styles.css /app/static/css/styles.css
ENV ADDR=":8080" \
    DB_PATH="/data/recipes.db" \
    STATIC_DIR="/app/static"
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/server"]
