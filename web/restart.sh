#!/bin/bash

# Web service restart script

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "Restarting web service..."

# Stop service
"$SCRIPT_DIR/stop.sh"

# Wait for service to fully stop
sleep 1

# Start service
"$SCRIPT_DIR/start.sh"
