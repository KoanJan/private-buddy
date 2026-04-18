#!/bin/bash

# Server service start script

echo "Starting server service..."

# Change to server directory
cd "$(dirname "$0")"

# Activate virtual environment
source venv/bin/activate

# Start FastAPI service
echo "Server service running at http://localhost:8000"
uvicorn app.main:app --reload --host 0.0.0.0 --port 8000
