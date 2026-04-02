#!/bin/bash
# Development server - Builds and runs both frontend and backend

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

cleanup() {
  kill $GO_PID 2>/dev/null || true
  kill $VITE_PID 2>/dev/null || true
  wait 2>/dev/null || true
}
trap cleanup EXIT INT TERM

# Kill any existing telegramarr or node processes on dev ports
echo "Cleaning up any existing processes..."
pkill -f "telegramarr" 2>/dev/null || true
pkill -f "vite.*8008" 2>/dev/null || true
pkill -f "vite.*8009" 2>/dev/null || true
pkill -f "vite.*8010" 2>/dev/null || true
sleep 1

# Create build directory if it doesn't exist
mkdir -p "$BUILD_DIR"

# Build frontend
echo "Building React frontend..."
cd "$UI_DIR"
if [ ! -d "node_modules" ]; then
  echo "Installing npm dependencies..."
  npm install
fi
npm run build
cd "$PROJECT_DIR"

# Build Go backend
echo "Building Go backend..."
cd "$PROJECT_DIR/src"
go build -o "$BINARY_PATH" ./cmd/telegramarr
cd "$PROJECT_DIR"

echo "Starting Telegramarr Development Server"
echo ""

# Start Vite dev server on 8008
echo "Starting Vite dev server on port 8008..."
cd "$UI_DIR"
npm run dev -- --host 0.0.0.0 --port 8008 &
VITE_PID=$!
cd "$PROJECT_DIR"

# Start backend on 8009
echo "Starting Go backend on port 8009..."
PORT=8009 "$BINARY_PATH" &
GO_PID=$!

echo ""
echo "✓ Frontend running on http://localhost:8008"
echo "✓ Backend running on http://localhost:8009"
echo "✓ Binary at: $BINARY_PATH"
echo ""
echo "Press Ctrl+C to stop"
echo ""

wait

