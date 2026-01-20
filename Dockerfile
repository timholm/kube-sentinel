# Build stage
FROM golang:1.22-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build arguments for version info
ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildDate=${BUILD_DATE}" \
    -o kube-sentinel ./cmd/kube-sentinel

# Final stage
FROM alpine:3.19

# Install ca-certificates for HTTPS and tzdata for timezone support
RUN apk --no-cache add ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 sentinel && \
    adduser -u 1000 -G sentinel -s /bin/sh -D sentinel

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/kube-sentinel /app/kube-sentinel

# Copy default config and rules
COPY config.yaml /etc/kube-sentinel/config.yaml
COPY rules.yaml /etc/kube-sentinel/rules.yaml

# Set ownership
RUN chown -R sentinel:sentinel /app /etc/kube-sentinel

# Switch to non-root user
USER sentinel

# Expose web dashboard port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Default command
ENTRYPOINT ["/app/kube-sentinel"]
CMD ["--config", "/etc/kube-sentinel/config.yaml"]
