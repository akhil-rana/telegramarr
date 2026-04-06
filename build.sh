#!/bin/bash
# Production build script - creates optimized production binaries and frontend assets
# Output goes to build/prod directory for Docker container deployment

set -e

PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD_DIR="$PROJECT_DIR/build/prod"
UI_DIR="$PROJECT_DIR/src/ui"

echo "=================================================="
echo "Telegramarr Production Build"
echo "=================================================="
echo ""

# Create build directory
echo "Creating build directory at: $BUILD_DIR"
mkdir -p "$BUILD_DIR"

# Build frontend
echo ""
echo "Building React frontend..."
cd "$UI_DIR"

# Install dependencies if needed
if [ ! -d "node_modules" ]; then
  echo "Installing npm dependencies..."
  npm install --production
fi

# Build optimized production bundle
npm run build
echo "✓ Frontend build complete"

# Copy frontend dist to build directory (Go will serve this)
echo "Copying frontend assets to build directory..."
rm -rf "$BUILD_DIR/dist"
cp -r dist "$BUILD_DIR/dist"
cd "$PROJECT_DIR"

# Build Go backend
echo ""
echo "Building Go backend (optimized)..."
cd "$PROJECT_DIR/src"

# Build with optimizations: strip symbols and disable debug
GOOS=linux GOARCH=amd64 go build \
  -ldflags="-s -w" \
  -o "$BUILD_DIR/telegramarr" \
  ./cmd/telegramarr

echo "✓ Backend build complete"
cd "$PROJECT_DIR"

# Print build summary
echo ""
echo "=================================================="
echo "Build Complete!"
echo "=================================================="
echo ""
echo "Build directory: $BUILD_DIR"
echo "Contents:"
echo "  - telegramarr (Go binary - serves both frontend and API)"
echo "  - dist/ (React frontend assets)"
echo ""
echo "The Go binary will:"
echo "  - Serve frontend (index.html, CSS, JS) on root path (/)"
echo "  - Serve API endpoints on /api/* paths"
echo "  - Listen on configured port (default: 8080)"
echo ""
echo "To run Docker container:"
echo "  docker build -t telegramarr ."
echo "  docker run -v \$(pwd)/config.yaml:/app/config.yaml \\
           -v \$(pwd)/data:/app/data \\
           -v /path/to/movies:/movies \\
           -v /path/to/tvshows:/tvshows \\
           -p 8080:8080 telegramarr"
echo ""

