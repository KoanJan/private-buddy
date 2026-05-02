#!/bin/bash

# Private Buddy Docker Deployment Script (China Mirror)

set -e

echo "========================================="
echo "  Private Buddy Docker Deployment"
echo "========================================="
echo ""

# Check if Docker is installed
if ! command -v docker &> /dev/null; then
    echo "Error: Docker is not installed"
    echo "Please install Docker first: https://docs.docker.com/get-docker/"
    exit 1
fi

# Check if Docker Compose is installed
if ! command -v docker-compose &> /dev/null; then
    echo "Error: Docker Compose is not installed"
    echo "Please install Docker Compose first: https://docs.docker.com/compose/install/"
    exit 1
fi

# Check if .env exists, if not create from example
if [ ! -f .env ]; then
    if [ -f .env.example ]; then
        echo "Creating .env file from .env.example..."
        cp .env.example .env
        echo "✓ .env file created"
    else
        echo "Warning: .env.example not found, using default configuration"
    fi
else
    echo "✓ Using existing .env file"
fi

echo ""
echo "Note: If you encounter Docker image pull errors (403 Forbidden),"
echo "please configure Docker mirror registry:"
echo ""
echo "For Docker Desktop on macOS:"
echo "  1. Open Docker Desktop"
echo "  2. Go to Settings -> Docker Engine"
echo "  3. Add registry-mirrors configuration"
echo ""
echo "For Linux, edit /etc/docker/daemon.json:"
echo '  {"registry-mirrors": ["https://docker.mirrors.ustc.edu.cn"]}'
echo ""
read -p "Press Enter to continue or Ctrl+C to configure Docker first..."

echo ""
echo "Cleaning Docker cache..."
docker builder prune -f

echo ""
echo "Building containers..."
echo ""

# Build containers with no cache
docker-compose build --no-cache

echo ""
echo "Starting containers..."
echo ""

# Start containers
docker-compose up -d

echo ""
echo "========================================="
echo "  Deployment Complete!"
echo "========================================="
echo ""
echo "Application is now running:"
echo "  - Web UI: http://localhost"
echo "  - API: http://localhost:8000"
echo ""
echo "Data directory: ~/PBD_trial_docker_and_embedding (inside container)"
echo ""
echo "Useful commands:"
echo "  - View logs: docker-compose logs -f"
echo "  - Stop: docker-compose down"
echo "  - Restart: docker-compose restart"
echo "  - Rebuild: docker-compose build --no-cache && docker-compose up -d"
echo ""