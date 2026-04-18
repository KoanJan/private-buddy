# Database Scripts Directory

This directory contains database initialization scripts and SQL files for the Private Buddy project.

## Directory Structure

```
database/
├── init_db.sh    # Database initialization script
├── sql/          # SQL files directory
│   └── 0.0.1.sql # Version 0.0.1 database structure
└── README.md     # This file
```

## Usage

### One-Click Database Initialization

Run the initialization script:

```bash
cd server/database
./init_db.sh
```

This script will:
1. Read database configuration from `server/.env` file
2. Create database if it doesn't exist
3. Execute SQL files in `sql/` directory in version order
4. Display execution progress and results

### Prerequisites

1. MySQL database installed
2. `server/.env` file created with database connection configuration
3. MySQL client command `mysql` available in terminal

### Environment Configuration

Configure database connection in `server/.env` file:

```env
DATABASE_URL=mysql+pymysql://user:password@localhost:3306/private_buddy
```

**Examples:**
- Without password: `mysql+pymysql://root@localhost:3306/private_buddy`
- With password: `mysql+pymysql://root:password@localhost:3306/private_buddy`

## SQL File Version Management

### Version Naming Convention

SQL files are named using version numbers, format: `major.minor.patch.sql`

**Examples:**
- `0.0.1.sql` - Initial version
- `0.1.0.sql` - Add new tables or major changes
- `0.1.1.sql` - Minor fixes or index optimization

### Execution Order

The script automatically executes SQL files in version order:
- Uses `sort -V` for version number sorting
- Ensures SQL files are executed in correct order

### Adding New Versions

1. Create a new SQL file in `sql/` directory
2. Use correct version number naming
3. Add comments at the beginning of the file to describe changes

**Example:**

```sql
-- Version: 0.1.0
-- Date: 2026-04-20
-- Changes: Add user preferences table

CREATE TABLE user_preferences (
    ...
);
```

## Database Structure

### Table Structure

**llm_configs** - LLM configuration table
- Stores LLM configuration information, including model name, API keys, etc.

**agents** - Agent configuration table
- Stores Agent configuration, associated with LLM configuration and system prompts

**sessions** - Session table
- Stores session information, associated with Agent

**messages** - Message table
- Stores message records, associated with session

### Index Optimization

**agents table:**
- `idx_agents_llm_config_id` - LLM configuration association query
- `idx_agents_updated_at` - Sort by update time

**sessions table:**
- `idx_sessions_created_at` - Sort by creation time
- `idx_sessions_status` - Filter by status
- `idx_sessions_agent_id` - Query by Agent ID
- `idx_sessions_agent_updated` - Composite index, optimize agent session list query

**messages table:**
- `idx_messages_session_id` - Query by session ID
- `idx_messages_created_at` - Sort by creation time
- `idx_messages_status` - Filter by status
- `idx_messages_session_created` - Composite index, optimize message history query
- `idx_messages_session_status` - Composite index, optimize streaming message query

## Database Design Principles

This project follows these database design principles:

1. **No foreign key constraints** - Data constraints should be enforced at application layer
2. **No nullable fields** - All fields should have explicit NOT NULL constraints
3. **Use indexes for query optimization** - Create indexes for frequently queried fields
4. **Prefer composite indexes** - Create composite indexes based on query patterns
5. **Avoid redundant indexes** - Remove duplicate and unnecessary indexes

## Important Notes

1. **Backup data** - Always backup before performing any database operations
2. **Test environment** - Validate database changes in test environment first
3. **Version control** - Commit new SQL files to version control system
4. **Idempotency** - SQL files should support repeated execution (use IF NOT EXISTS, etc.)

## Performance Monitoring

View slow queries:

```sql
-- View slow query log status
SHOW VARIABLES LIKE 'slow_query%';

-- View index usage
SHOW INDEX FROM messages;

-- Analyze query performance
EXPLAIN SELECT * FROM messages WHERE session_id = 1 ORDER BY created_at ASC;
```

## Troubleshooting

### Connection Failed

Check the following configurations:
1. Is MySQL service running
2. Are connection settings in `.env` file correct
3. Does user have sufficient permissions

### SQL Execution Failed

1. Check SQL syntax
2. Confirm if table or field already exists
3. Check MySQL error log

### Permission Issues

Ensure database user has the following permissions:
- CREATE DATABASE
- CREATE TABLE
- ALTER TABLE
- INDEX
