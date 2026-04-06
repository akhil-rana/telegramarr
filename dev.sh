#!/bin/bash
# Development server - Builds frontend completely and runs backend with static files

set -e

PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD_DIR="$PROJECT_DIR/build"
BINARY_PATH="$BUILD_DIR/telegramarr"
UI_DIR="$PROJECT_DIR/src/ui"

# Source nvm if it exists
export NVM_DIR="$HOME/.nvm"
if [ -s "$NVM_DIR/nvm.sh" ]; then
  \. "$NVM_DIR/nvm.sh"
  \. "$NVM_DIR/bash_completion"
fi

# Read port from config.yaml
if command -v yq &> /dev/null; then
  PORT=$(yq eval '.server.port' config.yaml 2>/dev/null || echo "8987")
else
  # Fallback if yq is not available
  PORT=$(grep "port:" config.yaml | head -1 | grep -oE '[0-9]+' || echo "8987")
fi

cleanup() {
  kill $GO_PID 2>/dev/null || true
  wait 2>/dev/null || true
}
trap cleanup EXIT INT TERM

# Kill any existing telegramarr processes
echo "Cleaning up any existing processes..."
pkill -f "telegramarr" 2>/dev/null || true

# Kill processes using the configured port (ignore errors if port not in use)
lsof -ti:$PORT 2>/dev/null | xargs kill -9 2>/dev/null || true

sleep 1

# Create build directory if it doesn't exist
mkdir -p "$BUILD_DIR"

# Build frontend (production build - same as Docker)
echo "Building React frontend (production build)..."
cd "$UI_DIR"
if [ ! -d "node_modules" ]; then
  echo "Installing npm dependencies..."
  npm install
fi
echo "Running: npm run build"
npm run build
if [ ! -d "dist" ] || [ -z "$(ls -A dist 2>/dev/null)" ]; then
  echo "ERROR: Frontend build failed or dist folder is empty!"
  exit 1
fi
echo "✓ Frontend build complete"
ls -lh dist/
cd "$PROJECT_DIR"

# Build Go backend
echo ""
echo "Building Go backend..."
cd "$PROJECT_DIR/src"
go build -o "$BINARY_PATH" ./cmd/telegramarr
cd "$PROJECT_DIR"
echo "✓ Backend build complete"

echo ""
echo "Starting Telegramarr Development Server"
echo ""

# Start backend using config port (serves static files from dist)
echo "Starting Go backend on port $PORT (from config.yaml)..."
echo "Static files served from: $UI_DIR/dist/"
echo "API endpoint: http://localhost:$PORT/api/*"
"$BINARY_PATH" &
GO_PID=$!

echo ""
echo "✓ Frontend static files built at: $UI_DIR/dist"
echo "✓ Backend running on http://localhost:$PORT"
echo "✓ Binary at: $BINARY_PATH"
echo ""
echo "Press Ctrl+C to stop"
echo ""

wait

