#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

PID_FILE="$SCRIPT_DIR/.pid"
LOG_DIR="$SCRIPT_DIR/logs"
mkdir -p "$LOG_DIR"

if [ -f "$PID_FILE" ]; then
    PID=$(cat "$PID_FILE")
    if kill -0 "$PID" 2>/dev/null; then
        echo "Server is already running (PID: $PID)"
        exit 1
    fi
    rm -f "$PID_FILE"
fi

echo "Building server..."
export GOPROXY=https://goproxy.cn,direct
go build -o private-buddy-server ./cmd/

# RUN_DIR="/tmp/private-buddy-server"
# mkdir -p "$RUN_DIR"
# cp private-buddy-server "$RUN_DIR/"
# cp .env "$RUN_DIR/" 2>/dev/null || true

echo "Starting server..."
# nohup "$RUN_DIR/private-buddy-server" > "$LOG_DIR/server.log" 2>&1 &
nohup "./private-buddy-server" > "$LOG_DIR/server.log" 2>&1 &
PID=$!
echo "$PID" > "$PID_FILE"

sleep 1
if kill -0 "$PID" 2>/dev/null; then
    echo "Server started (PID: $PID)"
else
    echo "Server failed to start. Check $LOG_DIR/server.log"
    rm -f "$PID_FILE"
    exit 1
fi
