#!/bin/bash

# Web service start script

echo "Starting web service..."

# Change to web directory
cd "$(dirname "$0")"

# Start development server
echo "Web service running at http://localhost:5173"
npm run dev
