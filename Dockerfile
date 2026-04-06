# Minimal runtime-only Dockerfile
# Expects pre-built artifacts from build.sh in build/prod/

FROM alpine:latest

WORKDIR /app

# Install runtime dependencies
# - libc6-compat: C library compatibility for pre-built binaries
# - libstdc++: C++ standard library for various tools
# - p7zip: For 7z compression tool
# - ca-certificates: For HTTPS/TLS support
# - tzdata: For timezone support
# - tini: For proper process signal handling
# - curl: For downloading RAR tools
# - tar: For extracting RAR archive
RUN apk add --no-cache \
    libc6-compat \
    libstdc++ \
    p7zip \
    ca-certificates \
    tzdata \
    tini \
    curl \
    tar

# Install RAR (unrar binary) from official source
# Download RAR tools, extract unrar binary, and clean up
RUN curl -LsSf https://www.rarlab.com/rar/rarlinux-x64-720.tar.gz > /tmp/rarlinux.tar.gz && \
    tar xf /tmp/rarlinux.tar.gz -C /tmp --strip-components=1 && \
    install -v -m755 /tmp/unrar /usr/local/bin && \
    install -v -m755 /tmp/rar /usr/local/bin && \
    rm -rf /tmp/rarlinux.tar.gz /tmp/rar /tmp/unrar && \
    unrar -? 2>&1 | head -3 && \
    rar -? 2>&1 | head -3

# Create non-root user for security
RUN addgroup -g 1000 -S telegramarr && \
    adduser -u 1000 -S telegramarr -G telegramarr

# Copy pre-built Go binary from build/prod/
COPY build/prod/telegramarr /app/

# Copy pre-built frontend artifacts from build/prod/dist/
COPY build/prod/dist /app/src/ui/dist

# Create necessary directories with correct permissions
RUN mkdir -p /app/data /app/temp && \
    chown -R telegramarr:telegramarr /app && \
    chmod 755 /app/data /app/temp /app/telegramarr

# Make binary executable
RUN chmod +x /app/telegramarr

# Switch to non-root user
USER telegramarr

# Expose port for the application (single port serves both frontend and API)
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=40s --retries=3 \
    CMD wget --quiet --tries=1 --spider http://localhost:8080/ || exit 1

# Use tini as entrypoint to handle signals properly
ENTRYPOINT ["/sbin/tini", "--"]

# Default command - runs the application
# Go binary serves:
#   - Frontend (index.html, CSS, JS) on /
#   - API endpoints on /api/*
CMD ["/app/telegramarr", "-config", "/app/config.yaml"]

# ============================================
# Volume mounts (for docker run/compose):
# ============================================
# -v /path/to/config.yaml:/app/config.yaml         # Configuration file (required, mounted read-only recommended)
# -v /path/to/data:/app/data                       # Persistent data (session.json, session.db)
# -v /path/to/movies:/movies                       # Radarr movie library path
# -v /path/to/tvshows:/tvshows                     # Sonarr TV shows library path
# -v /path/to/temp:/app/temp                       # Temporary directory for RAR splits (optional)
#
# Example docker-compose.yaml:
# services:
#   telegramarr:
#     image: telegramarr:latest
#     container_name: telegramarr
#     ports:
#       - "8080:8080"
#     volumes:
#       - ./config.yaml:/app/config.yaml:ro         # Read-only config
#       - ./data:/app/data                          # Persistent storage
#       - /mnt/media/movies:/movies                 # Radarr movies
#       - /mnt/media/tvshows:/tvshows               # Sonarr TV shows
#       - ./temp:/app/temp                          # Temp directory
#     environment:
#       - TZ=UTC
#     restart: unless-stopped
#     healthcheck:
#       test: ["CMD", "wget", "--quiet", "--tries=1", "--spider", "http://localhost:8080/"]
#       interval: 30s
#       timeout: 10s
#       retries: 3
#
# ============================================
# Build and run instructions:
# ============================================
# 1. Build production artifacts locally:
#    ./build.sh
#
# 2. Build Docker image:
#    docker build -t telegramarr:latest .
#
# 3. Run container (single port 8080):
#    docker run -d \
#      --name telegramarr \
#      -p 8080:8080 \
#      -v $(pwd)/config.yaml:/app/config.yaml:ro \
#      -v $(pwd)/data:/app/data \
#      -v /mnt/movies:/movies \
#      -v /mnt/tvshows:/tvshows \
#      -e TZ=UTC \
#      telegramarr:latest
#
# 4. Access:
#    - Frontend: http://localhost:8080
#    - API: http://localhost:8080/api/*
#    - Webhooks: http://localhost:8080/api/webhooks/radarr
#
# ============================================
# Tools available in container:
# ============================================
# - unrar: Extract RAR files
# - rar: Create/modify RAR files
# - 7z: Create/extract 7z archives
# - Go binary: Compiled application binary (no Go toolchain)
#
# Image size: ~100-150MB (Alpine + Go binary + frontend + rar tools)
# Build time: ~2-3 minutes (no compilation, just copying + rar download)
# No Node.js or Go toolchain in final image - runtime only!

