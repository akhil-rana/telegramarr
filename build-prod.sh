#!/bin/bash

# Production Build Script for Telegramarr
# This script compiles both frontend and backend for production
# and prepares them for Docker image building
# 
# Usage: ./build-prod.sh
# 
# Output: build/prod/ directory with:
#   - telegramarr (Go binary)
#   - dist/ (compiled React frontend)
#
# The Docker image will ONLY copy pre-built artifacts - NO compilation happens in Docker!

set -e

echo "=================================="
echo "  Telegramarr Production Build"
echo "=================================="
echo ""

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Directories
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD_DIR="$SCRIPT_DIR/build/prod"
FRONTEND_DIR="$SCRIPT_DIR/src/ui"
GO_DIR="$SCRIPT_DIR"

# Ensure build directory exists
mkdir -p "$BUILD_DIR"

# ============================================
# Step 1: Build Frontend (React with Vite)
# ============================================
echo -e "${BLUE}Step 1: Building React Frontend...${NC}"
cd "$FRONTEND_DIR"

if [ ! -d "node_modules" ]; then
    echo "Installing Node.js dependencies..."
    npm ci
fi

echo "Building frontend with Vite..."
npm run build

# Copy compiled frontend to build/prod/dist
mkdir -p "$BUILD_DIR/dist"
rm -rf "$BUILD_DIR/dist"/*
cp -r "$FRONTEND_DIR/dist"/* "$BUILD_DIR/dist/"
echo -e "${GREEN}✓ Frontend built: $BUILD_DIR/dist${NC}"
echo ""

# ============================================
# Step 2: Build Go Backend
# ============================================
echo -e "${BLUE}Step 2: Building Go Backend...${NC}"
cd "$GO_DIR/src"  # Go module is in src/ directory

# Detect current platform
PLATFORM=$(uname -s)
ARCH=$(uname -m)

# Map architecture to Go GOARCH
case "$ARCH" in
    x86_64)
        GOARCH="amd64"
        ;;
    aarch64)
        GOARCH="arm64"
        ;;
    *)
        echo "Error: Unsupported architecture: $ARCH"
        exit 1
        ;;
esac

echo "Building for: $PLATFORM / $GOARCH"

# Build Go binary with optimizations
# CGO_ENABLED=0: No C dependencies (fully static on Linux)
# -ldflags="-w -s": Strip debug symbols (smaller binary)
# -trimpath: Remove build path from binary (reproducible builds)
CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH" \
    go build \
    -ldflags="-w -s -X main.Version=0.7.0-beta" \
    -trimpath \
    -o "$BUILD_DIR/telegramarr" \
    ./cmd/telegramarr

# Verify binary was built
if [ ! -f "$BUILD_DIR/telegramarr" ]; then
    echo "Error: Binary not found at $BUILD_DIR/telegramarr"
    exit 1
fi

# Make executable
chmod +x "$BUILD_DIR/telegramarr"

# Show binary info
BINARY_SIZE=$(du -h "$BUILD_DIR/telegramarr" | cut -f1)
echo -e "${GREEN}✓ Backend built: $BUILD_DIR/telegramarr ($BINARY_SIZE)${NC}"
echo ""

# ============================================
# Step 3: Verify Build
# ============================================
echo -e "${BLUE}Step 3: Verifying Build...${NC}"

# Check binary is executable
if [ ! -x "$BUILD_DIR/telegramarr" ]; then
    echo "Error: Binary is not executable"
    exit 1
fi

# Check frontend files exist
if [ ! -f "$BUILD_DIR/dist/index.html" ]; then
    echo "Error: Frontend index.html not found"
    exit 1
fi

echo "Frontend files:"
ls -lh "$BUILD_DIR/dist/" | head -10
echo ""

echo "Backend binary:"
file "$BUILD_DIR/telegramarr"
echo ""
