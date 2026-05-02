# Private Buddy Database

Private Buddy uses SQLite for data storage with SQLAlchemy ORM.

## Database Schema

### Tables

- **llm_configs**: LLM provider configurations
- **agents**: AI assistant configurations
- **sessions**: Chat sessions
- **messages**: Chat messages
- **interactions**: Agent interaction records
- **historical_summaries**: Conversation summaries
- **search_config**: Search engine configuration
- **db_versions**: Schema version tracking

### Key Features

- **No Foreign Keys**: Following project coding rules, all data constraints are handled at the application layer
- **No Nullable Fields**: All database fields are non-nullable
- **Automatic Schema Creation**: Tables are created automatically on application startup

## Data Directory

All application data is unified under `~/PBD_trial_docker_and_embedding/`:

```
~/PBD_trial_docker_and_embedding/
    db/                 -- SQLite database (private_buddy.db)
    chroma/             -- ChromaDB vector store (using built-in BGE-base-zh embedding)
    workspace/          -- Agent task workspace
    avatars/            -- Agent avatar images
```

The `DATA_ROOT` can be configured via `.env` file (defaults to `~/PBD_trial_docker_and_embedding`).

## SQL File Management

### Full Init SQL (`sql/full_init.sql`)

Contains the **complete schema** for the current version. Updated with each release to reflect the full database structure. Used for fresh database creation only.

### Upgrade SQL (`sql/upgrade/`)

Contains **incremental** schema changes between versions. Each file represents the delta from one version to the next.

**Naming convention:** `major.minor.patch.sql` (e.g., `0.0.9.sql`, `0.1.0.sql`)

**Execution order:** Files are sorted by version number (`sort -V`) and applied sequentially.

### Adding a New Version

1. Modify tables as needed
2. Create an incremental SQL file in `sql/upgrade/` (e.g., `0.0.9.sql`)
3. Update `sql/full_init.sql` to reflect all changes
4. Add comments at the beginning of the upgrade file to describe changes
5. The upgrade SQL should also insert a record into `db_versions`

## Database Initialization

### Automatic Initialization

The application automatically creates all tables on startup using `Base.metadata.create_all()`. Manual initialization is optional.

### Manual Initialization

Run the initialization script:

```bash
cd database
./init_db.sh
```

This creates the SQLite database at `~/PBD_trial_docker_and_embedding/db/private_buddy.db`.

### Database Upgrade

To upgrade an existing database:

```bash
cd database
./init_db.sh upgrade
```

This applies all pending upgrade SQL files from `sql/upgrade/`.

## Version History

- **0.0.9**: Removed embedding configuration, using built-in BGE-base-zh model
- **0.0.8**: Initial SQLite schema after MySQL migration

## Backup and Restore

### Backup

```bash
cp ~/PBD_trial_docker_and_embedding/db/private_buddy.db ~/backup/private_buddy_$(date +%Y%m%d).db
```

### Restore

```bash
cp ~/backup/private_buddy_20260101.db ~/PBD_trial_docker_and_embedding/db/private_buddy.db
```

## Troubleshooting

### Database Locked

If you encounter "database is locked" errors:

1. Ensure only one application instance is running
2. Check for zombie processes: `ps aux | grep uvicorn`
3. Restart the application

### Schema Mismatch

If you encounter schema mismatch errors:

1. Check the current version: `sqlite3 ~/PBD_trial_docker_and_embedding/db/private_buddy.db "SELECT * FROM db_versions ORDER BY id DESC LIMIT 1;"`
2. Run upgrade script: `cd database && ./init_db.sh upgrade`
3. If upgrade fails, backup and reinitialize: `rm ~/PBD_trial_docker_and_embedding/db/private_buddy.db && cd database && ./init_db.sh`

## Development

### Creating Migrations

When modifying the database schema:

1. Update the SQLAlchemy models in `app/models/`
2. Create an upgrade SQL file in `database/sql/upgrade/`
3. Update `database/sql/full_init.sql` with the complete schema
4. Test the migration on a copy of production data

### Testing

Always test migrations on a copy of production data before applying to production.
