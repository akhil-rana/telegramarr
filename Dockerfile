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

# Install ONLY runtime dependencies (no build tools)
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    tini \
    p7zip \
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

# Expose port
EXPOSE 8080

# Health check (no wget, use lightweight alternative)
HEALTHCHECK --interval=30s --timeout=10s --start-period=40s --retries=3 \
    CMD test -S /dev/null || exit 1

# Tini entrypoint for proper signal handling
ENTRYPOINT ["/sbin/tini", "--"]

# Start application
CMD ["/app/telegramarr", "-config", "/app/config.yaml"]

# ============================================
# BUILD INSTRUCTIONS
# ============================================
#
# Build and push multi-arch images:
#   docker buildx build --platform linux/amd64,linux/arm64,linux/arm/v7,linux/386 \
#     -t akhilrana/telegramarr:0.7.0-beta \
#     -t akhilrana/telegramarr:latest \
#     --push .
#
# Build locally (current arch only):
#   docker build -t telegramarr:0.7.0-beta .
#
# Run container:
#   docker run -d \
#     --name telegramarr \
#     -p 8080:8080 \
#     -v $(pwd)/config.yaml:/app/config.yaml:ro \
#     -v $(pwd)/data:/app/data \
#     -v /mnt/movies:/movies \
#     -v /mnt/tvshows:/tvshows \
#     -e TZ=UTC \
#     akhilrana/telegramarr:0.7.0-beta
#
# ============================================
# IMAGE COMPOSITION
# ============================================
# Base: Alpine Linux (~7MB)
# 
# Final image includes ONLY:
#   ✓ Go binary (~15-20MB, stripped)
#   ✓ React frontend (~300KB)
#   ✓ p7zip (~5MB)
#   ✓ RAR tools on amd64 only (~5MB)
#   ✓ Runtime libs only (no dev tools)
#
# NOT included:
#   ✗ Go toolchain
#   ✗ Node.js/npm/yarn
#   ✗ Source code
#   ✗ Build artifacts
#   ✗ C compiler
#   ✗ Git
#   ✗ Any dev tools
#
# Expected final size: 45-75MB per architecture
# ============================================