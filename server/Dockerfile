# syntax=docker/dockerfile:1
# NOTE: build context is the REPO ROOT (so the web stage can see ../webinterface).
# The compose files set `context: ..` / `dockerfile: server/Dockerfile`; to build
# by hand, run from the repo root: `docker build -f server/Dockerfile .`

# --- Web UI build stage ---
FROM node:22-alpine AS webbuilder
RUN npm i -g pnpm@10
WORKDIR /web
COPY webinterface/package.json webinterface/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY webinterface/ .
# Refresh the npm attribution manifest from the installed node_modules (#101).
# Tolerant on purpose: public/open-source.json is committed and up to date, so a
# generator hiccup must not break the image — it only means the manifest stays at
# its committed state.
RUN pnpm run gen:licenses || echo "WARNING: frontend license generation failed; using the committed public/open-source.json"
# NUXT_PUBLIC_API_BASE_URL deliberately NOT set -> apiBaseUrl '' -> same-origin.
RUN pnpm generate

# --- Go build stage ---
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /app

# Cache dependencies
COPY server/go.mod server/go.sum ./
RUN go mod download

# Build
COPY server/ .
# Refresh the Go attribution manifest that internal/usecase/about embeds (#101).
# Tolerant on purpose: the manifest is committed, so the endpoint works even if
# this step is skipped or the module cache lacks a LICENSE file — a failure here
# only means the embedded list stays at its committed state.
RUN go run ./tools/genlicenses || echo "WARNING: Go license generation failed; using the committed internal/usecase/about/open_source_go.json"
# Install and run swag
RUN go install github.com/swaggo/swag/cmd/swag@latest
RUN /go/bin/swag init -g cmd/server/main.go --parseDependency --parseInternal
RUN CGO_ENABLED=1 GOOS=linux go build -a -ldflags '-linkmode external -extldflags "-static"' -o server ./cmd/server

# Runtime stage
FROM alpine:3.23

RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 caldav && \
    adduser -u 1000 -G caldav -s /bin/sh -D caldav

WORKDIR /app

# Copy binary + docs
COPY --from=builder /app/server .
COPY --from=builder /app/docs ./docs
# Copy the built SPA so the Go server serves it same-origin (see registerWebUI).
COPY --from=webbuilder /web/.output/public ./public

# Create data directory
RUN mkdir -p /data && chown caldav:caldav /data

USER caldav

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

ENTRYPOINT ["./server"]
