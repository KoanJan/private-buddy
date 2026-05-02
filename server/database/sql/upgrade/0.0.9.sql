-- Trial: Docker and Embedding
-- This migration removes embedding configuration support and uses built-in BGE-base-zh model

-- Step 1: Remove embedding_config_id from agents table
-- SQLite does not support DROP COLUMN, so we need to recreate the table

CREATE TABLE agents_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name VARCHAR(255) NOT NULL,
    character_settings TEXT NOT NULL DEFAULT '',
    llm_config_id INTEGER NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    avatar VARCHAR(500) NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO agents_new (id, name, character_settings, llm_config_id, description, avatar, created_at, updated_at)
SELECT id, name, character_settings, llm_config_id, description, avatar, created_at, updated_at
FROM agents;

DROP TABLE agents;
ALTER TABLE agents_new RENAME TO agents;

CREATE INDEX IF NOT EXISTS idx_agents_llm_config_id ON agents(llm_config_id);
CREATE INDEX IF NOT EXISTS idx_agents_updated_at ON agents(updated_at);

-- Step 2: Drop embedding_configs table
DROP TABLE IF EXISTS embedding_configs;

-- Step 3: Update version
INSERT INTO db_versions (version, description)
VALUES ('0.0.9', 'Removed embedding configuration, using built-in BGE-base-zh model');
