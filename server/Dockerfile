# Build stage
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /app

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Build
COPY . .
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

# Copy binary
COPY --from=builder /app/server .
COPY --from=builder /app/docs ./docs

# Create data directory
RUN mkdir -p /data && chown caldav:caldav /data

USER caldav

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

ENTRYPOINT ["./server"]
