#!/bin/bash

# Server service stop script

echo "Stopping server service..."

# Find and stop uvicorn process
PID=$(ps aux | grep 'uvicorn app.main:app' | grep -v grep | awk '{print $2}')

if [ -z "$PID" ]; then
    echo "Server service is not running"
    exit 0
fi

# Stop process
kill $PID

# Wait for process to terminate
sleep 2

# Check if stopped successfully
if ps -p $PID > /dev/null 2>&1; then
    echo "Force stopping server service..."
    kill -9 $PID
    sleep 1
fi

echo "Server service stopped"
