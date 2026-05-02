#!/bin/bash

# Private Buddy Docker Deployment Script (Using China Mirror)

set -e

echo "========================================="
echo "  Private Buddy Docker Deployment"
echo "  (Using China Mirror)"
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
echo "Using China mirror Dockerfiles..."
echo ""

# Backup original Dockerfiles
cp server/Dockerfile server/Dockerfile.backup
cp web/Dockerfile web/Dockerfile.backup

# Use China mirror Dockerfiles
cp server/Dockerfile.cn server/Dockerfile
cp web/Dockerfile.cn web/Dockerfile

echo "Building containers..."
echo ""

# Build containers (with cache for faster builds)
docker-compose build

echo ""
echo "Starting containers..."
echo ""

# Start containers
docker-compose up -d

# Restore original Dockerfiles
mv server/Dockerfile.backup server/Dockerfile
mv web/Dockerfile.backup web/Dockerfile

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
echo "  - Rebuild: ./deploy-cn.sh"
echo ""