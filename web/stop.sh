#!/bin/bash

# Web service stop script

echo "Stopping web service..."

# Find and stop vite process
PID=$(ps aux | grep 'vite' | grep -v grep | awk '{print $2}')

if [ -z "$PID" ]; then
    echo "Web service is not running"
    exit 0
fi

# Stop process
kill $PID

# Wait for process to terminate
sleep 2

# Check if stopped successfully
if ps -p $PID > /dev/null 2>&1; then
    echo "Force stopping web service..."
    kill -9 $PID
    sleep 1
fi

echo "Web service stopped"
