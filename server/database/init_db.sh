#!/bin/bash

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SERVER_DIR="$(dirname "$SCRIPT_DIR")"
ENV_FILE="$SERVER_DIR/.env"
SQL_DIR="$SCRIPT_DIR/sql"

echo "========================================="
echo "  Private Buddy Database Initialization"
echo "========================================="
echo ""

if [ ! -f "$ENV_FILE" ]; then
    echo "Error: Environment configuration file not found: $ENV_FILE"
    echo "Please create .env file and configure database connection"
    exit 1
fi

echo "Reading database configuration..."
DB_HOST="localhost"
DB_PORT="3306"
DB_USER="root"
DB_PASS=""
DB_NAME="private_buddy"

while IFS='=' read -r key value; do
    case "$key" in
        DATABASE_URL)
            if [[ $value =~ mysql\+pymysql://([^:]*):?([^@]*)@([^:]*):([0-9]*)/(.*) ]]; then
                DB_USER="${BASH_REMATCH[1]}"
                DB_PASS="${BASH_REMATCH[2]}"
                DB_HOST="${BASH_REMATCH[3]}"
                DB_PORT="${BASH_REMATCH[4]}"
                DB_NAME="${BASH_REMATCH[5]}"
            elif [[ $value =~ mysql\+pymysql://([^@]*)@([^:]*):([0-9]*)/(.*) ]]; then
                DB_USER="${BASH_REMATCH[1]}"
                DB_HOST="${BASH_REMATCH[2]}"
                DB_PORT="${BASH_REMATCH[3]}"
                DB_NAME="${BASH_REMATCH[4]}"
            fi
            ;;
    esac
done < "$ENV_FILE"

echo "Database configuration:"
echo "  Host: $DB_HOST:$DB_PORT"
echo "  User: $DB_USER"
echo "  Database: $DB_NAME"
echo ""

if [ ! -d "$SQL_DIR" ]; then
    echo "Error: SQL directory not found: $SQL_DIR"
    exit 1
fi

SQL_FILES=($(ls "$SQL_DIR"/*.sql 2>/dev/null | sort -V))

if [ ${#SQL_FILES[@]} -eq 0 ]; then
    echo "Error: No SQL files found in $SQL_DIR directory"
    exit 1
fi

echo "Found SQL files:"
for file in "${SQL_FILES[@]}"; do
    echo "  - $(basename "$file")"
done
echo ""

read -p "Continue to initialize database? (y/N): " -n 1 -r
echo ""
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Initialization cancelled"
    exit 0
fi

echo ""
echo "Step 1: Creating database (if not exists)..."
mysql -h "$DB_HOST" -P "$DB_PORT" -u "$DB_USER" ${DB_PASS:+-p"$DB_PASS"} -e "CREATE DATABASE IF NOT EXISTS \`$DB_NAME\` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;"
echo "✓ Database created or already exists"

echo ""
echo "Step 2: Executing SQL files..."
for file in "${SQL_FILES[@]}"; do
    filename=$(basename "$file")
    echo "  Executing: $filename"
    
    if mysql -h "$DB_HOST" -P "$DB_PORT" -u "$DB_USER" ${DB_PASS:+-p"$DB_PASS"} "$DB_NAME" < "$file"; then
        echo "  ✓ $filename executed successfully"
    else
        echo "  ✗ $filename execution failed"
        exit 1
    fi
done

echo ""
echo "========================================="
echo "  Database initialization complete!"
echo "========================================="
echo ""
echo "Database structure created:"
echo "  - llm_configs (LLM configuration table)"
echo "  - agents (Agent configuration table)"
echo "  - sessions (Session table)"
echo "  - messages (Message table)"
echo ""
echo "Next step: Start server service"
echo "  cd $SERVER_DIR"
echo "  ./start.sh"
echo ""
