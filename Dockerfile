# Stage 1: Build the Go application
FROM golang:1.21-alpine AS go-builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY pkg/ ./pkg/

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags '-extldflags "-static"' -o lemmy-scraper ./cmd/scraper

# Stage 2: Build the web UI (optional - only if web directory exists)
FROM node:20-alpine AS web-builder

WORKDIR /build

# Copy web directory if it exists
COPY web* ./web/ 2>/dev/null || true

# Build web UI if package.json exists
RUN if [ -f web/package.json ]; then \
      cd web && \
      npm ci && \
      npm run build; \
    else \
      echo "Web UI not found, skipping build"; \
      mkdir -p web/build; \
    fi

# Stage 3: Final runtime image
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 scraper && \
    adduser -D -u 1000 -G scraper scraper

# Create application directories
RUN mkdir -p /app /config /downloads && \
    chown -R scraper:scraper /app /config /downloads

WORKDIR /app

# Copy binary from builder
COPY --from=go-builder /build/lemmy-scraper .

# Copy web UI from builder (if it exists)
COPY --from=web-builder --chown=scraper:scraper /build/web/build ./web/build

# Switch to non-root user
USER scraper

# Define volumes for persistent data
VOLUME ["/config", "/downloads"]

# Expose web UI port
EXPOSE 8080

# Set environment variables with defaults
ENV CONFIG_PATH=/config/config.yaml

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD pgrep -x lemmy-scraper || exit 1

# Run the application
ENTRYPOINT ["/app/lemmy-scraper"]
CMD ["-config", "/config/config.yaml"]
