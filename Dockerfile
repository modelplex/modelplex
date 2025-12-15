# Multi-stage build for minimal, secure image
FROM golang:1.25.0-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy source code
COPY . .

# Build the application with optimizations
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -a -installsuffix cgo \
    -ldflags='-w -s -extldflags "-static"' \
    -o modelplex ./cmd/modelplex

# Final stage - minimal alpine image
FROM alpine:latest

# Install security updates and required packages
RUN apk update && apk add --no-cache ca-certificates tzdata && \
    addgroup -g 1001 -S modelplex && \
    adduser -u 1001 -S modelplex -G modelplex

WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /app/modelplex .

# Copy default config
COPY config.toml .

# Create directory for socket with proper permissions
RUN mkdir -p /tmp/modelplex && \
    chown -R modelplex:modelplex /app /tmp/modelplex

# Use non-root user for security
USER modelplex

# Expose the socket directory as volume
VOLUME ["/tmp/modelplex"]

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD ["/app/modelplex", "--version"]

ENTRYPOINT ["./modelplex"]
CMD ["--config", "config.toml", "--socket", "/tmp/modelplex/modelplex.socket"]
