#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PID_FILE="$SCRIPT_DIR/.pid"

SHUTDOWN_TIMEOUT=25  # Seconds to wait for graceful shutdown before force kill

# Try to stop via PID file first
if [ -f "$PID_FILE" ]; then
    PID=$(cat "$PID_FILE")
    if kill -0 "$PID" 2>/dev/null; then
        echo "Stopping server (PID: $PID)..."
        kill "$PID"

        # Wait for graceful shutdown with polling
        elapsed=0
        while kill -0 "$PID" 2>/dev/null && [ "$elapsed" -lt "$SHUTDOWN_TIMEOUT" ]; do
            sleep 1
            elapsed=$((elapsed + 1))
        done

        if kill -0 "$PID" 2>/dev/null; then
            echo "Server did not stop gracefully after ${SHUTDOWN_TIMEOUT}s, force killing..."
            kill -9 "$PID"
        else
            echo "Server stopped gracefully (waited ${elapsed}s)"
        fi
    else
        echo "Stale PID file (process $PID not running)"
    fi
    rm -f "$PID_FILE"
fi

# Fallback: find and kill process by port
PORT="${PORT:-8000}"
PID_BY_PORT=$(lsof -ti :"$PORT" 2>/dev/null || true)
if [ -n "$PID_BY_PORT" ]; then
    echo "Found process occupying port $PORT (PID: $PID_BY_PORT), signalling shutdown..."
    kill $PID_BY_PORT 2>/dev/null || true

    elapsed=0
    while kill -0 "$PID_BY_PORT" 2>/dev/null && [ "$elapsed" -lt "$SHUTDOWN_TIMEOUT" ]; do
        sleep 1
        elapsed=$((elapsed + 1))
    done

    if kill -0 "$PID_BY_PORT" 2>/dev/null; then
        echo "Process on port $PORT did not stop after ${SHUTDOWN_TIMEOUT}s, force killing..."
        kill -9 $PID_BY_PORT 2>/dev/null || true
    else
        echo "Process on port $PORT stopped (waited ${elapsed}s)"
    fi
    echo "Port $PORT freed"
fi
