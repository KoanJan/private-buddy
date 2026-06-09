// Package database provides SQLite database initialization and migration.
//
// This package handles:
//   - Database connection setup with WAL mode for concurrent access
//   - Auto-migration of all model tables
//   - Default data seeding (search config, DB version)
//
// SQLite configuration:
//   - WAL journal mode for better concurrent read performance
//   - 5-second busy timeout for write contention
//   - Immediate transaction locking to prevent deadlocks
//   - Single connection pool (SQLite limitation)
package database

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"private-buddy-server/internal/config"
	applogger "private-buddy-server/internal/logger"
	"private-buddy-server/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// DB is the global database connection instance.
//
// NOTE: *gorm.DB exposes full database capabilities including dangerous operations
// (Raw, Exec, Migrator, DB, Callback, etc.) that violate the principle of least
// privilege. For an internal application this risk is acceptable, but if stricter
// access control is needed in the future, consider encapsulating *gorm.DB within
// this package and exposing only business-semantic functions (e.g. FindByID,
// CreateEntity, UpdateEntity) so that *gorm.DB never leaks outside this package.
var DB *gorm.DB

// Init initializes the SQLite database connection.
// Creates the database directory if it doesn't exist.
// Configures WAL mode, busy timeout, and immediate transaction locking.
func Init() {
	settings := config.Get()

	dbDir := filepath.Join(settings.DataRoot, "db")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		panic(fmt.Sprintf("Failed to create database directory: %v", err))
	}

	dbPath := settings.DatabaseURL()
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_txlock=immediate"

	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
		NowFunc: func() time.Time {
			return time.Now().Local()
		},
	})
	if err != nil {
		panic(fmt.Sprintf("Failed to connect to database: %v", err))
	}

	sqlDB, err := db.DB()
	if err != nil {
		panic(fmt.Sprintf("Failed to get underlying sql.DB: %v", err))
	}
	sqlDB.SetMaxOpenConns(1)

	DB = db
	applogger.L.Info("Database initialized", "path", dbPath)
}

// AutoMigrate creates all database tables and adds missing columns.
// For new tables: uses GORM AutoMigrate to create the full schema.
// For existing tables: only adds missing columns via ALTER TABLE ADD COLUMN,
// avoiding the table-recreation path that SQLite uses when schema differs
// (which can fail on NOT NULL constraints during data copy).
func AutoMigrate() {
	models := []interface{}{
		&model.LLMConfig{},
		&model.EmbeddingConfig{},
		&model.Agent{},
		&model.Session{},
		&model.Message{},
		&model.Interaction{},
		&model.HistoricalSummary{},
		&model.SearchConfig{},
		&model.DBVersion{},
		&model.KnowledgeBase{},
		&model.Document{},
		&model.DocumentChunk{},
		&model.Work{},
		&model.MessageDraft{},
		&model.ParticipantSession{},
		&model.ScheduledEvent{},
	}

	// Run structural migrations BEFORE addMissingColumns, because some
	// migrations rebuild tables (e.g., changing NOT NULL columns) which
	// cannot be done via ALTER TABLE ADD COLUMN.
	migrateInteractionsTable()
	migrateHistoricalSummariesTable()

	for _, m := range models {
		if DB.Migrator().HasTable(m) {
			addMissingColumns(m)
			// Also ensure AUTOINCREMENT for existing tables that were created without it
			ensureAutoIncrement(m)
		} else {
			if err := DB.AutoMigrate(m); err != nil {
				panic(fmt.Sprintf("Failed to auto-migrate %T: %v", m, err))
			}
			applogger.L.Info("Created table", "model", fmt.Sprintf("%T", m))
			ensureAutoIncrement(m)
		}
	}

	ensureSearchConfig()
	ensureDBVersion()

	// Drop sessions.status column (removed from model in Agent Runtime step2)
	dropSessionsStatusColumn()

	// Migrate enum columns from TEXT to INTEGER across all tables
	migrateEnumColumnsToInt()

	applogger.L.Info("Database migration completed")
}

// addMissingColumns inspects the model struct and adds any columns that
// don't exist in the database table. Uses ALTER TABLE ADD COLUMN which
// SQLite supports without table recreation.
func addMissingColumns(m interface{}) {
	stmt := &gorm.Statement{DB: DB}
	if err := stmt.Parse(m); err != nil {
		return
	}

	for _, field := range stmt.Schema.Fields {
		colName := field.DBName
		if !DB.Migrator().HasColumn(m, colName) {
			applogger.L.Info("Adding missing column", "table", stmt.Table, "column", colName)
			if err := DB.Migrator().AddColumn(m, colName); err != nil {
				panic(fmt.Sprintf("Failed to add column %s.%s: %v", stmt.Table, colName, err))
			}
		}
	}
}

// ensureAutoIncrement ensures a SQLite table uses AUTOINCREMENT for its primary key.
//
// GORM's autoIncrement tag generates "INTEGER PRIMARY KEY" which uses the max(id)+1
// algorithm, allowing ID reuse after row deletion. The AUTOINCREMENT keyword enforces
// strict monotonic IDs that are never reused, matching MySQL's AUTO_INCREMENT behavior.
//
// For new tables (empty): drops and recreates directly.
// For existing tables (with data): uses the standard SQLite table rebuild procedure:
//  1. Create a temporary table with AUTOINCREMENT
//  2. Copy all data from the original table
//  3. Drop the original table
//  4. Rename the temporary table to the original name
//  5. Recreate indexes
func ensureAutoIncrement(m interface{}) {
	stmt := &gorm.Statement{DB: DB}
	if err := stmt.Parse(m); err != nil {
		return
	}
	tableName := stmt.Table

	// Find the primary key column and check if it has autoIncrement
	var pkCol string
	hasAutoIncrement := false
	for _, field := range stmt.Schema.Fields {
		if field.PrimaryKey {
			pkCol = field.DBName
			hasAutoIncrement = field.AutoIncrement
			break
		}
	}
	if pkCol == "" || !hasAutoIncrement {
		return
	}

	// Get current CREATE TABLE DDL from sqlite_master
	var currentDDL string
	DB.Raw("SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?", tableName).Scan(&currentDDL)
	if currentDDL == "" {
		return
	}

	// Skip if AUTOINCREMENT is already present
	if containsAutoIncrement(currentDDL) {
		return
	}

	// Build the new DDL with AUTOINCREMENT
	newDDL := addAutoIncrementToDDL(currentDDL, pkCol)
	if newDDL == currentDDL {
		return
	}

	// Check if the table has data
	var rowCount int64
	DB.Raw("SELECT COUNT(*) FROM " + tableName).Scan(&rowCount)

	if rowCount == 0 {
		// Empty table: safe to drop and recreate directly
		rebuildEmptyTable(tableName, newDDL)
	} else {
		// Table with data: use migration rebuild with data preservation
		rebuildTableWithData(tableName, newDDL, currentDDL, pkCol)
	}
}

// rebuildEmptyTable drops and recreates an empty table with AUTOINCREMENT.
func rebuildEmptyTable(tableName, newDDL string) {
	applogger.L.Info("Rebuilding empty table with AUTOINCREMENT", "table", tableName)

	// Save index definitions before dropping
	indexes := getTableIndexes(tableName)

	DB.Exec("DROP TABLE " + tableName)
	if err := DB.Exec(newDDL).Error; err != nil {
		panic(fmt.Sprintf("Failed to rebuild table %s with AUTOINCREMENT: %v", tableName, err))
	}

	recreateIndexes(indexes)
}

// rebuildTableWithData rebuilds a table with data using the standard SQLite migration procedure.
func rebuildTableWithData(tableName, newDDL, _ string, pkCol string) {
	applogger.L.Info("Migrating table with AUTOINCREMENT (data preservation)", "table", tableName)

	// Save index definitions before any changes
	indexes := getTableIndexes(tableName)

	// Get column list for data copy
	columns := getTableColumns(tableName)

	tempTable := tableName + "_autoincrement_tmp"

	// Step 1: Create temporary table with AUTOINCREMENT
	tmpDDL := strings.Replace(newDDL, "CREATE TABLE "+tableName, "CREATE TABLE "+tempTable, 1)
	// Handle quoted table names
	tmpDDL = strings.Replace(tmpDDL, "CREATE TABLE \""+tableName+"\"", "CREATE TABLE \""+tempTable+"\"", 1)
	if err := DB.Exec(tmpDDL).Error; err != nil {
		panic(fmt.Sprintf("Failed to create temp table for %s: %v", tableName, err))
	}

	// Step 2: Copy data from original to temp
	colList := strings.Join(columns, ", ")
	copySQL := fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s", tempTable, colList, colList, tableName)
	if err := DB.Exec(copySQL).Error; err != nil {
		DB.Exec("DROP TABLE " + tempTable)
		panic(fmt.Sprintf("Failed to copy data for %s: %v", tableName, err))
	}

	// Verify row count matches
	var newRowCount int64
	DB.Raw("SELECT COUNT(*) FROM " + tempTable).Scan(&newRowCount)
	var oldRowCount int64
	DB.Raw("SELECT COUNT(*) FROM " + tableName).Scan(&oldRowCount)
	if newRowCount != oldRowCount {
		DB.Exec("DROP TABLE " + tempTable)
		panic(fmt.Sprintf("Row count mismatch during %s migration: %d != %d", tableName, newRowCount, oldRowCount))
	}

	// Step 3: Drop original table
	DB.Exec("DROP TABLE " + tableName)

	// Step 4: Rename temp table to original name
	if err := DB.Exec("ALTER TABLE " + tempTable + " RENAME TO " + tableName).Error; err != nil {
		panic(fmt.Sprintf("Failed to rename temp table for %s: %v", tableName, err))
	}

	// Step 5: Recreate indexes
	recreateIndexes(indexes)

	applogger.L.Info("Table migration completed", "table", tableName, "rows", newRowCount)
	_ = pkCol // pkCol used for DDL generation, not needed here
}

// indexDef holds an index name and its CREATE statement.
type indexDef struct {
	Name string
	SQL  string
}

// getTableIndexes returns index definitions for a table from sqlite_master.
func getTableIndexes(tableName string) []indexDef {
	var indexes []indexDef
	DB.Raw("SELECT name, sql FROM sqlite_master WHERE type = 'index' AND tbl_name = ? AND sql IS NOT NULL", tableName).Scan(&indexes)
	return indexes
}

// getTableColumns returns the column names of a table in definition order.
func getTableColumns(tableName string) []string {
	type ColumnInfo struct {
		CID  int
		Name string
	}
	var columns []ColumnInfo
	DB.Raw("PRAGMA table_info(" + tableName + ")").Scan(&columns)
	result := make([]string, 0, len(columns))
	for _, col := range columns {
		result = append(result, col.Name)
	}
	return result
}

// recreateIndexes recreates indexes from saved definitions.
func recreateIndexes(indexes []indexDef) {
	for _, idx := range indexes {
		if idx.SQL != "" {
			if err := DB.Exec(idx.SQL).Error; err != nil {
				applogger.L.Warn("Failed to recreate index", "index", idx.Name, "error", err)
			}
		}
	}
}

// containsAutoIncrement checks if a CREATE TABLE DDL already contains AUTOINCREMENT.
func containsAutoIncrement(ddl string) bool {
	return strings.Contains(strings.ToUpper(ddl), "AUTOINCREMENT")
}

// dropSessionsStatusColumn removes the `status` column from the sessions table.
// This column was removed from the model in the Agent Runtime (step2) refactor,
// as session status is now managed in-memory by AgentRuntime.
//
// SQLite does not support ALTER TABLE DROP COLUMN before version 3.35.0,
// so we use the standard table rebuild procedure:
//  1. Create new table without the column
//  2. Copy data
//  3. Drop old table
//  4. Rename new table
func dropSessionsStatusColumn() {
	if !DB.Migrator().HasTable(&model.Session{}) {
		return
	}
	if !DB.Migrator().HasColumn(&model.Session{}, "status") {
		return
	}

	applogger.L.Info("Dropping sessions.status column (removed from model)")

	// Rebuild sessions table without the status column
	DB.Exec(`
		CREATE TABLE sessions_new (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			title      VARCHAR(255) NOT NULL DEFAULT '',
			agent_id   INTEGER NOT NULL,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		)
	`)
	DB.Exec(`INSERT INTO sessions_new (id, title, agent_id, created_at, updated_at)
		SELECT id, title, agent_id, created_at, updated_at FROM sessions`)
	DB.Exec(`DROP TABLE sessions`)
	DB.Exec(`ALTER TABLE sessions_new RENAME TO sessions`)

	// Recreate the index on agent_id
	DB.Exec(`CREATE INDEX idx_sessions_agent_id ON sessions(agent_id)`)

	applogger.L.Info("Successfully dropped sessions.status column")
}

// migrateEnumColumnsToInt migrates all enum columns from TEXT to INTEGER
// across all affected tables. This enforces the project convention that
// all enum types must use int, not string.
//
// Affected tables and columns:
//   - participant_sessions: participant_type, role, status
//   - messages: role
//   - documents: status
//   - knowledge_bases: index_type
//
// SQLite does not support ALTER COLUMN, so each table is rebuilt using
// the standard procedure: create new → copy data → drop old → rename.
// String values are mapped to their int equivalents during copy.
func migrateEnumColumnsToInt() {
	migrateParticipantSessionsEnumToInt()
	migrateMessagesRoleToInt()
	migrateDocumentsStatusToInt()
	migrateKnowledgeBasesIndexTypeToInt()
}

// isIntegerType checks if a SQLite column type is an integer type.
func isIntegerType(typeName string) bool {
	t := strings.ToUpper(typeName)
	return t == "INTEGER" || t == "INT" || t == "BIGINT" || t == "SMALLINT" || t == "TINYINT"
}

// getColumnType returns the SQLite column type for a given table and column.
func getColumnType(tableName, colName string) string {
	type ColInfo struct {
		Type string
	}
	var info ColInfo
	DB.Raw("SELECT type FROM pragma_table_info(?) WHERE name = ?", tableName, colName).Scan(&info)
	return info.Type
}

// needEnumMigration checks if any of the specified columns in a table
// are not INTEGER type (i.e., still stored as TEXT/VARCHAR).
func needEnumMigration(tableName string, columns ...string) bool {
	for _, col := range columns {
		if !isIntegerType(getColumnType(tableName, col)) {
			return true
		}
	}
	return false
}

// migrateParticipantSessionsEnumToInt rebuilds participant_sessions with
// participant_type, role, status as INTEGER columns.
func migrateParticipantSessionsEnumToInt() {
	if !DB.Migrator().HasTable(&model.ParticipantSession{}) {
		return
	}
	if !needEnumMigration("participant_sessions", "participant_type", "role", "status") {
		return
	}

	applogger.L.Info("Migrating participant_sessions: enum TEXT → INTEGER")

	DB.Exec(`
		CREATE TABLE participant_sessions_new (
			id                   INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id           INTEGER NOT NULL,
			participant_type     INTEGER NOT NULL DEFAULT 1,
			participant_id       INTEGER NOT NULL DEFAULT 0,
			role                 INTEGER NOT NULL DEFAULT 2,
			status               INTEGER NOT NULL DEFAULT 0,
			last_read_message_id INTEGER NOT NULL DEFAULT 0,
			created_at           DATETIME NOT NULL,
			updated_at           DATETIME NOT NULL
		)
	`)

	// Map old string values to int constants
	DB.Exec(`INSERT INTO participant_sessions_new
		(id, session_id, participant_type, participant_id, role, status, last_read_message_id, created_at, updated_at)
		SELECT id, session_id,
			CASE participant_type WHEN 'agent' THEN 2 ELSE 1 END,
			participant_id,
			CASE role WHEN 'owner' THEN 1 WHEN 'watcher' THEN 3 ELSE 2 END,
			CASE status WHEN 'working' THEN 1 ELSE 0 END,
			last_read_message_id, created_at, updated_at
		FROM participant_sessions`)

	DB.Exec(`DROP TABLE participant_sessions`)
	DB.Exec(`ALTER TABLE participant_sessions_new RENAME TO participant_sessions`)
	DB.Exec(`CREATE INDEX idx_participant_sessions_session_id ON participant_sessions(session_id)`)

	applogger.L.Info("Successfully migrated participant_sessions enum columns to INTEGER")
}

// migrateMessagesRoleToInt rebuilds messages with role as INTEGER column.
func migrateMessagesRoleToInt() {
	if !DB.Migrator().HasTable(&model.Message{}) {
		return
	}
	if !needEnumMigration("messages", "role") {
		return
	}

	applogger.L.Info("Migrating messages: role TEXT → INTEGER")

	DB.Exec(`
		CREATE TABLE messages_new (
			id               INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id       INTEGER NOT NULL,
			role             INTEGER NOT NULL DEFAULT 1,
			content          TEXT NOT NULL,
			status           INTEGER NOT NULL DEFAULT 0,
			has_interactions INTEGER NOT NULL DEFAULT 0,
			draft_id         INTEGER,
			created_at       DATETIME NOT NULL,
			updated_at       DATETIME NOT NULL
		)
	`)

	DB.Exec(`INSERT INTO messages_new
		(id, session_id, role, content, status, has_interactions, draft_id, created_at, updated_at)
		SELECT id, session_id,
			CASE role WHEN 'assistant' THEN 2 ELSE 1 END,
			content, status, has_interactions, draft_id, created_at, updated_at
		FROM messages`)

	DB.Exec(`DROP TABLE messages`)
	DB.Exec(`ALTER TABLE messages_new RENAME TO messages`)
	DB.Exec(`CREATE INDEX idx_messages_session_id ON messages(session_id)`)

	applogger.L.Info("Successfully migrated messages.role to INTEGER")
}

// migrateDocumentsStatusToInt rebuilds documents with status as INTEGER column.
func migrateDocumentsStatusToInt() {
	if !DB.Migrator().HasTable(&model.Document{}) {
		return
	}
	if !needEnumMigration("documents", "status") {
		return
	}

	applogger.L.Info("Migrating documents: status TEXT → INTEGER")

	DB.Exec(`
		CREATE TABLE documents_new (
			id               INTEGER PRIMARY KEY AUTOINCREMENT,
			knowledge_base_id INTEGER NOT NULL,
			title            VARCHAR(500) NOT NULL,
			source           VARCHAR(500) NOT NULL DEFAULT '',
			file_path        VARCHAR(500) NOT NULL DEFAULT '',
			file_size        INTEGER NOT NULL DEFAULT 0,
			file_type        VARCHAR(20) NOT NULL DEFAULT '',
			chunk_count      INTEGER NOT NULL DEFAULT 0,
			status           INTEGER NOT NULL DEFAULT 0,
			error_message    TEXT NOT NULL DEFAULT '',
			created_at       DATETIME NOT NULL,
			updated_at       DATETIME NOT NULL
		)
	`)

	DB.Exec(`INSERT INTO documents_new
		(id, knowledge_base_id, title, source, file_path, file_size, file_type, chunk_count, status, error_message, created_at, updated_at)
		SELECT id, knowledge_base_id, title, source, file_path, file_size, file_type, chunk_count,
			CASE status WHEN 'processing' THEN 1 WHEN 'ready' THEN 2 WHEN 'failed' THEN 3 WHEN 'deleted' THEN 4 ELSE 0 END,
			error_message, created_at, updated_at
		FROM documents`)

	DB.Exec(`DROP TABLE documents`)
	DB.Exec(`ALTER TABLE documents_new RENAME TO documents`)

	applogger.L.Info("Successfully migrated documents.status to INTEGER")
}

// migrateKnowledgeBasesIndexTypeToInt rebuilds knowledge_bases with index_type as INTEGER column.
func migrateKnowledgeBasesIndexTypeToInt() {
	if !DB.Migrator().HasTable(&model.KnowledgeBase{}) {
		return
	}
	if !needEnumMigration("knowledge_bases", "index_type") {
		return
	}

	applogger.L.Info("Migrating knowledge_bases: index_type TEXT → INTEGER")

	DB.Exec(`
		CREATE TABLE knowledge_bases_new (
			id                  INTEGER PRIMARY KEY AUTOINCREMENT,
			name                VARCHAR(255) NOT NULL,
			description         TEXT NOT NULL DEFAULT '',
			embedding_config_id INTEGER NOT NULL,
			index_type          INTEGER NOT NULL DEFAULT 0,
			index_file_path     VARCHAR(500) NOT NULL DEFAULT '',
			document_count      INTEGER NOT NULL DEFAULT 0,
			vector_count        INTEGER NOT NULL DEFAULT 0,
			deleted_count       INTEGER NOT NULL DEFAULT 0,
			created_at          DATETIME NOT NULL,
			updated_at          DATETIME NOT NULL
		)
	`)

	DB.Exec(`INSERT INTO knowledge_bases_new
		(id, name, description, embedding_config_id, index_type, index_file_path, document_count, vector_count, deleted_count, created_at, updated_at)
		SELECT id, name, description, embedding_config_id,
			CASE index_type WHEN 'switching' THEN 1 WHEN 'hnsw' THEN 2 ELSE 0 END,
			index_file_path, document_count, vector_count, deleted_count, created_at, updated_at
		FROM knowledge_bases`)

	DB.Exec(`DROP TABLE knowledge_bases`)
	DB.Exec(`ALTER TABLE knowledge_bases_new RENAME TO knowledge_bases`)

	applogger.L.Info("Successfully migrated knowledge_bases.index_type to INTEGER")
}

// migrateInteractionsTable migrates the interactions table from the old schema
// (user_msg_id + agent_msg_id) to the new schema (draft_id).
// This aligns with the draft-based architecture where interactions are grouped
// by draft_id instead of message pairs.
func migrateInteractionsTable() {
	if !DB.Migrator().HasTable(&model.Interaction{}) {
		return
	}
	// If the new draft_id column already exists, migration is done
	if DB.Migrator().HasColumn(&model.Interaction{}, "draft_id") {
		return
	}
	// If the old user_msg_id column doesn't exist, nothing to migrate
	if !DB.Migrator().HasColumn(&model.Interaction{}, "user_msg_id") {
		return
	}

	applogger.L.Info("Migrating interactions table: user_msg_id+agent_msg_id → draft_id")

	// Rebuild the interactions table with the new schema
	DB.Exec(`
		CREATE TABLE interactions_new (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id INTEGER NOT NULL,
			draft_id   INTEGER NOT NULL,
			iteration  INTEGER NOT NULL,
			type       INTEGER NOT NULL,
			data       TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		)
	`)
	DB.Exec(`INSERT INTO interactions_new (id, session_id, draft_id, iteration, type, data, created_at, updated_at)
		SELECT id, session_id, 0, iteration, type, data, created_at, updated_at FROM interactions`)
	DB.Exec(`DROP TABLE interactions`)
	DB.Exec(`ALTER TABLE interactions_new RENAME TO interactions`)
	DB.Exec(`CREATE INDEX idx_interactions_session_id ON interactions(session_id)`)
	DB.Exec(`CREATE INDEX idx_interactions_draft_id ON interactions(draft_id)`)

	applogger.L.Info("Successfully migrated interactions table to draft_id schema")
}

// migrateHistoricalSummariesTable adds the agent_id column to historical_summaries.
// Summaries are now scoped by (session_id, agent_id) so that different agents
// in the same session maintain independent summaries and narratives.
//
// Since agent_id is NOT NULL and SQLite cannot add NOT NULL columns without
// defaults via ALTER TABLE, we use the standard table rebuild procedure:
//  1. Create new table with agent_id column
//  2. Copy data, backfilling agent_id from sessions.agent_id
//  3. Drop old table
//  4. Rename new table
func migrateHistoricalSummariesTable() {
	if !DB.Migrator().HasTable(&model.HistoricalSummary{}) {
		return
	}
	// If agent_id column already exists, migration is done
	if DB.Migrator().HasColumn(&model.HistoricalSummary{}, "agent_id") {
		return
	}

	applogger.L.Info("Migrating historical_summaries table: adding agent_id column")

	// Rebuild with agent_id, backfilling from sessions.agent_id
	DB.Exec(`
		CREATE TABLE historical_summaries_new (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id INTEGER NOT NULL,
			agent_id   INTEGER NOT NULL,
			version    INTEGER NOT NULL,
			content    TEXT NOT NULL,
			narrative  TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		)
	`)
	DB.Exec(`INSERT INTO historical_summaries_new (id, session_id, agent_id, version, content, narrative, created_at, updated_at)
		SELECT hs.id, hs.session_id, COALESCE(s.agent_id, 0), hs.version, hs.content, hs.narrative, hs.created_at, hs.created_at
		FROM historical_summaries hs
		LEFT JOIN sessions s ON hs.session_id = s.id`)
	DB.Exec(`DROP TABLE historical_summaries`)
	DB.Exec(`ALTER TABLE historical_summaries_new RENAME TO historical_summaries`)
	DB.Exec(`CREATE INDEX idx_historical_summaries_session_id ON historical_summaries(session_id)`)
	DB.Exec(`CREATE INDEX idx_historical_summaries_agent_id ON historical_summaries(agent_id)`)

	applogger.L.Info("Successfully migrated historical_summaries table with agent_id column")
}

// addAutoIncrementToDDL inserts AUTOINCREMENT after "INTEGER PRIMARY KEY" in the DDL.
func addAutoIncrementToDDL(ddl string, pkCol string) string {
	// Pattern: "pkCol" INTEGER PRIMARY KEY → "pkCol" INTEGER PRIMARY KEY AUTOINCREMENT
	quoted := `"` + pkCol + `"`
	target := quoted + " INTEGER PRIMARY KEY"
	replacement := quoted + " INTEGER PRIMARY KEY AUTOINCREMENT"

	// Case-insensitive search
	upperDDL := strings.ToUpper(ddl)
	upperTarget := strings.ToUpper(target)
	targetIdx := strings.Index(upperDDL, upperTarget)
	if targetIdx == -1 {
		return ddl
	}

	return ddl[:targetIdx] + replacement + ddl[targetIdx+len(target):]
}

// ensureSearchConfig creates the default search config record if it doesn't exist.
func ensureSearchConfig() {
	var count int64
	DB.Model(&model.SearchConfig{}).Where("id = ?", 1).Count(&count)
	if count == 0 {
		DB.Create(&model.SearchConfig{
			Provider:    "tavily",
			APIKey:      "",
			Description: "",
			IsActive:    false,
		})
	}
}

// ensureDBVersion creates the initial DB version record if the table is empty.
func ensureDBVersion() {
	var count int64
	DB.Model(&model.DBVersion{}).Count(&count)
	if count == 0 {
		DB.Create(&model.DBVersion{
			Version:     "0.0.8",
			Description: "Initial SQLite schema after MySQL migration",
		})
	}
}
