#!/bin/bash

# Server environment setup script

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VENV_DIR="$SCRIPT_DIR/venv"

echo "========================================="
echo "  Private Buddy Server Environment Setup"
echo "========================================="
echo ""

# Find Python interpreter
PYTHON_CMD=""
for cmd in python3.12 python3.11 python3; do
    if command -v $cmd &> /dev/null; then
        PYTHON_CMD=$cmd
        break
    fi
done

if [ -z "$PYTHON_CMD" ]; then
    echo "Error: Python 3 not found"
    exit 1
fi

PYTHON_VERSION=$($PYTHON_CMD --version 2>&1 | awk '{print $2}')
echo "Using Python: $PYTHON_CMD (version $PYTHON_VERSION)"

if [[ ! "$PYTHON_VERSION" =~ ^3\.1[1-9] ]]; then
    echo "Warning: Python 3.11 or higher is recommended"
    read -p "Continue? (y/N): " -n 1 -r
    echo ""
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
fi

# Create virtual environment
if [ -d "$VENV_DIR" ]; then
    echo "Virtual environment already exists: $VENV_DIR"
    read -p "Recreate? (y/N): " -n 1 -r
    echo ""
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        echo "Removing old virtual environment..."
        rm -rf "$VENV_DIR"
    else
        echo "Using existing virtual environment"
    fi
fi

if [ ! -d "$VENV_DIR" ]; then
    echo "Creating virtual environment with $PYTHON_CMD..."
    $PYTHON_CMD -m venv "$VENV_DIR"
    echo "✓ Virtual environment created successfully"
fi

# Activate virtual environment
echo "Activating virtual environment..."
source "$VENV_DIR/bin/activate"

# Upgrade pip
echo "Upgrading pip..."
pip install --upgrade pip setuptools wheel

# Install dependencies
echo ""
echo "Installing project dependencies..."
if [ -f "$SCRIPT_DIR/pyproject.toml" ]; then
    echo "Using pyproject.toml to install dependencies..."
    pip install -e "$SCRIPT_DIR"
    echo "✓ Core dependencies installed"
    
    read -p "Install development dependencies? (y/N): " -n 1 -r
    echo ""
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        echo "Installing development dependencies..."
        pip install -e "$SCRIPT_DIR[dev]"
        echo "✓ Development dependencies installed"
    fi
else
    echo "Error: pyproject.toml not found"
    exit 1
fi

echo ""
echo "========================================="
echo "  Environment setup complete!"
echo "========================================="
echo ""
echo "Virtual environment location: $VENV_DIR"
echo ""
echo "Next steps:"
echo "  1. Activate virtual environment: source venv/bin/activate"
echo "  2. Configure environment variables: cp .env.example .env (and edit)"
echo "  3. Initialize database: cd database && ./init_db.sh"
echo "  4. Start service: ./start.sh"
echo ""
