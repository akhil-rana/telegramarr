# ============================================
# STAGE 1: Build Frontend (Node.js + Vite)
# ============================================
FROM node:22-alpine AS frontend-builder

WORKDIR /build

# Copy only frontend source and package files
COPY src/ui/package*.json ./
COPY src/ui/vite.config.* ./
COPY src/ui/tsconfig* ./
COPY src/ui/index.html ./
COPY src/ui/src ./src

# Install dependencies and build
RUN npm ci && npm run build

# ============================================
# STAGE 2: Build Go Backend
# ============================================
FROM golang:1.25-alpine AS go-builder

WORKDIR /build

# Install build dependencies (minimal)
RUN apk add --no-cache git jq

# Copy only Go source files needed for compilation
COPY src/go.mod src/go.sum ./
COPY src/cmd ./cmd
COPY src/internal ./internal
COPY version.json /build/

# Extract version from version.json
RUN VERSION=$(jq -r '.version' /build/version.json) && \
    echo "Building version: $VERSION"

# Build binary for target platform
RUN VERSION=$(jq -r '.version' /build/version.json) && \
    CGO_ENABLED=0 go build \
    -ldflags="-w -s -X main.Version=${VERSION}" \
    -trimpath \
    -o telegramarr \
    ./cmd/telegramarr

# ============================================
# STAGE 3: Runtime Image (Ultra-minimal)
# ============================================
FROM alpine:latest

WORKDIR /app

# Install wget and tini for healthcheck and signal handling
RUN apk add --no-cache wget tini \
    && rm -rf /var/cache/apk/*

# Install RAR only on amd64 (ARM64 uses 7z)
RUN if [ "$(apk --print-arch)" = "x86_64" ]; then \
    apk add --no-cache curl tar && \
    curl -LsSf https://www.rarlab.com/rar/rarlinux-x64-720.tar.gz -o /tmp/rar.tar.gz && \
    tar xf /tmp/rar.tar.gz -C /tmp && \
    install -m755 /tmp/rar/rar /tmp/rar/unrar /usr/local/bin/ && \
    rm -rf /tmp/rar* && \
    apk del --no-cache curl tar; \
    fi

# Create non-root user
RUN addgroup -g 1000 -S telegramarr && \
    adduser -u 1000 -S telegramarr -G telegramarr

# Copy ONLY the compiled binary from go-builder stage
COPY --from=go-builder /build/telegramarr /app/telegramarr

# Copy ONLY the compiled frontend from frontend-builder stage
COPY --from=frontend-builder /build/dist /app/src/ui/dist

# Create directories and set permissions
RUN mkdir -p /app/data /app/temp && \
    chown -R telegramarr:telegramarr /app && \
    chmod 755 /app/data /app/temp && \
    chmod +x /app/telegramarr && \
    chmod -R 755 /app/src/ui/dist

# Switch to non-root user
USER telegramarr

# Expose port (set in config.yaml, default: 8987)
EXPOSE 8987

# Health check using wget
HEALTHCHECK --interval=30s --timeout=10s --start-period=40s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8987/api/auth/status || exit 1

# Tini entrypoint for proper signal handling
ENTRYPOINT ["/sbin/tini", "--"]

# Start application
CMD ["/app/telegramarr", "-config", "/app/config.yaml"]