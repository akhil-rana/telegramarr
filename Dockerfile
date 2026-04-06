# Telegramarr Production Image
# Multi-architecture support (amd64, arm64)
# 
# IMPORTANT: This Dockerfile assumes pre-built artifacts in build/prod/
# NO COMPILATION happens when building this image!
# 
# Use ./build-prod.sh to compile frontend and backend first
# Then: docker build -t telegramarr:0.7.0-beta .

FROM alpine:latest

WORKDIR /app

# Install runtime dependencies ONLY (minimal set)
# - ca-certificates: For HTTPS/TLS support
# - tzdata: For timezone support  
# - tini: For proper signal handling
# - p7zip: For 7z compression tool (always needed)
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    tini \
    p7zip

# Install RAR only on x86_64 (not on ARM64)
# ARM64 uses 7z format by default
RUN if [ "$(apk --print-arch)" != "aarch64" ]; then \
    apk add --no-cache curl tar && \
    echo "Installing RAR for $(apk --print-arch)..." && \
    curl -LsSf https://www.rarlab.com/rar/rarlinux-x64-720.tar.gz > /tmp/rarlinux.tar.gz && \
    tar xf /tmp/rarlinux.tar.gz -C /tmp && \
    install -v -m755 /tmp/rar/unrar /usr/local/bin && \
    install -v -m755 /tmp/rar/rar /usr/local/bin && \
    rm -rf /tmp/rarlinux.tar.gz /tmp/rar && \
    which unrar && which rar && \
    apk del --no-cache curl tar; \
    else \
    echo "Skipping RAR installation on ARM64 (uses 7z)"; \
    fi

# Create non-root user for security
RUN addgroup -g 1000 -S telegramarr && \
    adduser -u 1000 -S telegramarr -G telegramarr

# Copy PRE-BUILT Go binary (no compilation!)
COPY build/prod/telegramarr /app/

# Copy PRE-BUILT frontend artifacts (no compilation!)
COPY build/prod/dist /app/src/ui/dist

# Create necessary directories with correct permissions
RUN mkdir -p /app/data /app/temp && \
    chown -R telegramarr:telegramarr /app && \
    chmod 755 /app/data /app/temp /app/telegramarr && \
    chmod +x /app/telegramarr

# Switch to non-root user
USER telegramarr

# Expose single port for both frontend and API
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=40s --retries=3 \
    CMD wget --quiet --tries=1 --spider http://localhost:8080/ || exit 1

# Use tini as entrypoint to handle signals properly
ENTRYPOINT ["/sbin/tini", "--"]

# Default command
CMD ["/app/telegramarr", "-config", "/app/config.yaml"]

# ============================================
# Build Instructions (NO COMPILATION IN DOCKER)
# ============================================
#
# 1. Pre-compile everything locally:
#    ./build-prod.sh
#
# 2. Build Docker image (no compilation happens here!):
#    docker build -t telegramarr:0.7.0-beta .
#
# 3. Push to Docker Hub (both architectures):
#    docker buildx build --platform linux/amd64,linux/arm64 \
#      -t yourusername/telegramarr:0.7.0-beta \
#      -t yourusername/telegramarr:latest \
#      --push .
#
# 4. Run container:
#    docker run -d \
#      --name telegramarr \
#      -p 8080:8080 \
#      -v $(pwd)/config.yaml:/app/config.yaml:ro \
#      -v $(pwd)/data:/app/data \
#      -v /mnt/movies:/movies \
#      -v /mnt/tvshows:/tvshows \
#      -e TZ=UTC \
#      telegramarr:0.7.0-beta
#
# ============================================
# Image Details
# ============================================
# Base: Alpine Linux (~7MB)
# Single port: 8080 (frontend + API)
# 
# What's included:
#   ✓ Pre-built Go binary (~15-20MB)
#   ✓ Pre-built React frontend (~300KB)
#   ✓ 7z tool (~5MB)
#   ✓ RAR tools on x86_64 only (~7MB)
#   ✓ Runtime dependencies (minimal)
#
# What's NOT included:
#   ✗ Go toolchain
#   ✗ Node.js
#   ✗ npm/yarn
#   ✗ C compiler
#   ✗ Build tools
#
# Expected image size: 80-120MB
#
# Important: NO COMPILATION happens when running docker build!
# This is a pure artifact copy image.
# ============================================
