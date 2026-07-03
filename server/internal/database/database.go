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
		DefaultContextTimeout: 30 * time.Second,
		Logger:                gormlogger.Default.LogMode(gormlogger.Silent),
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
	applogger.Info("Database initialized", "path", dbPath)
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
		&model.Summary{},
		&model.AgentNarrative{},
		&model.SearchConfig{},
		&model.DBVersion{},
		&model.KnowledgeBase{},
		&model.Document{},
		&model.DocumentChunk{},
		&model.Work{},
		&model.MessageDraft{},
		&model.ParticipantSession{},
		&model.ScheduledEvent{},
		&model.Event{},
		&model.AgentObservation{},
		&model.EventVector{},
		&model.EntityProfile{},
		&model.User{},
		&model.ModelCapability{},
		&model.AgentExperience{},
		&model.AgentExperienceVector{},
		&model.PublicExperience{},
		&model.PublicExperienceVector{},
		&model.SystemLLMConfig{},
		&model.UploadedSkill{},
	}

	// Run structural migrations BEFORE addMissingColumns, because some
	// migrations rebuild tables (e.g., changing NOT NULL columns) which
	// cannot be done via ALTER TABLE ADD COLUMN.
	migrateInteractionsTable()

	for _, m := range models {
		if DB.Migrator().HasTable(m) {
			addMissingColumns(m)
		} else {
			if err := DB.AutoMigrate(m); err != nil {
				panic(fmt.Sprintf("Failed to auto-migrate %T: %v", m, err))
			}
			applogger.Info("Created table", "model", fmt.Sprintf("%T", m))
		}
	}

	ensureSearchConfig()
	ensureDBVersion()

	// Drop sessions.status column (removed from model in Agent Runtime step2)
	dropSessionsStatusColumn()

	// Drop agent_observations.survival_count column (removed — importance-based
	// staleness gating supersedes the binary survival_count gate)
	dropSurvivalCountColumn()

	// Drop experience content column (replaced by when_to_use/guidelines/pitfalls/procedure)
	dropExperienceContentColumn()

	// Migrate enum columns from TEXT to INTEGER across all tables
	migrateEnumColumnsToInt()

	// Rebuild agent_experiences table: replace source_fingerprint (string hash)
	// with source_id (int64 resource reference). Existing rows cannot be migrated
	// because a hash cannot be reverse-mapped to a session_id, so the table is
	// dropped and recreated empty. Decision: clear-and-rebuild is acceptable at
	// this early stage of the system.
	rebuildAgentExperiencesForSourceID()

	// Rebuild uploaded_skills table: remove status and fingerprint columns,
	// rename source_name → file_name, add title column.
	// UploadedSkill is now stateless — concurrency control is in-memory.
	rebuildUploadedSkillsStateless()

	applogger.Info("Database migration completed")
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
			applogger.Info("Adding missing column", "table", stmt.Table, "column", colName)
			if err := DB.Migrator().AddColumn(m, colName); err != nil {
				panic(fmt.Sprintf("Failed to add column %s.%s: %v", stmt.Table, colName, err))
			}
		}
	}
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

	applogger.Info("Dropping sessions.status column (removed from model)")

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

	applogger.Info("Successfully dropped sessions.status column")
}

// dropSurvivalCountColumn removes the `survival_count` column from the
// agent_observations table. This column is superseded by importance-based
// staleness gating: an observation with importance > 0.5 has been boosted
// by retrieval or propagation and is protected from cleanup.
//
// Uses the same table rebuild procedure as dropSessionsStatusColumn
// (SQLite does not support ALTER TABLE DROP COLUMN before 3.35.0).
func dropSurvivalCountColumn() {
	if !DB.Migrator().HasTable(&model.AgentObservation{}) {
		return
	}
	if !DB.Migrator().HasColumn(&model.AgentObservation{}, "survival_count") {
		return
	}

	applogger.Info("Dropping agent_observations.survival_count column (removed from model)")

	DB.Exec(`
		CREATE TABLE agent_observations_new (
			id               INTEGER PRIMARY KEY AUTOINCREMENT,
			agent_id         INTEGER NOT NULL,
			event_id         INTEGER NOT NULL,
			importance       REAL    NOT NULL DEFAULT 0.5,
			last_accessed_at DATETIME NOT NULL,
			last_scored_at   DATETIME NOT NULL,
			created_at       DATETIME NOT NULL,
			updated_at       DATETIME NOT NULL
		)
	`)
	DB.Exec(`INSERT INTO agent_observations_new (id, agent_id, event_id, importance, last_accessed_at, last_scored_at, created_at, updated_at)
		SELECT id, agent_id, event_id, importance, last_accessed_at, last_scored_at, created_at, updated_at FROM agent_observations`)
	DB.Exec(`DROP TABLE agent_observations`)
	DB.Exec(`ALTER TABLE agent_observations_new RENAME TO agent_observations`)

	// Recreate the unique index on (agent_id, event_id)
	DB.Exec(`CREATE UNIQUE INDEX idx_observations_agent_event ON agent_observations(agent_id, event_id)`)

	applogger.Info("Successfully dropped agent_observations.survival_count column")
}

// dropExperienceContentColumn removes the `content` column from the
// agent_experiences table. This column has been replaced by four
// dedicated fields: when_to_use, guidelines, pitfalls, procedure.
//
// Uses the standard SQLite table rebuild procedure:
//  1. Create new table without content
//  2. Copy data
//  3. Drop old table
//  4. Rename new table
func dropExperienceContentColumn() {
	if !DB.Migrator().HasTable(&model.AgentExperience{}) {
		return
	}
	if !DB.Migrator().HasColumn(&model.AgentExperience{}, "content") {
		return
	}

	applogger.Info("Dropping agent_experiences.content column (replaced by structured fields)")

	DB.Exec(`
		CREATE TABLE agent_experiences_new (
			id                 INTEGER PRIMARY KEY AUTOINCREMENT,
			agent_id           INTEGER NOT NULL,
			title              VARCHAR(500) NOT NULL,
			description        TEXT NOT NULL,
			when_to_use        TEXT NOT NULL DEFAULT '',
			guidelines         TEXT NOT NULL DEFAULT '',
			pitfalls           TEXT NOT NULL DEFAULT '',
			procedure         TEXT NOT NULL DEFAULT '',
			source             INTEGER NOT NULL DEFAULT 1,
			source_fingerprint VARCHAR(64) NOT NULL,
			created_at         DATETIME NOT NULL,
			updated_at         DATETIME NOT NULL
		)
	`)
	DB.Exec(`INSERT INTO agent_experiences_new (id, agent_id, title, description, when_to_use, guidelines, pitfalls, procedure, source, source_fingerprint, created_at, updated_at)
		SELECT id, agent_id, title, description, when_to_use, guidelines, pitfalls, procedure, source, source_fingerprint, created_at, updated_at FROM agent_experiences`)
	DB.Exec(`DROP TABLE agent_experiences`)
	DB.Exec(`ALTER TABLE agent_experiences_new RENAME TO agent_experiences`)

	DB.Exec(`CREATE INDEX idx_agent_experiences_agent_id ON agent_experiences(agent_id)`)
	DB.Exec(`CREATE INDEX idx_agent_experiences_source_fingerprint ON agent_experiences(source_fingerprint)`)

	applogger.Info("Successfully dropped agent_experiences.content column")
}

// rebuildAgentExperiencesForSourceID replaces the source_fingerprint column
// (a SHA-256 hash string that conflated "which resource" with "has it changed")
// with a source_id INTEGER column that purely references the origin resource:
//
//   - Source=1 (Reflection): source_id = session_id
//   - Source=2 (Learn):       source_id = public_experience_id
//
// Existing rows cannot be migrated because a hash cannot be reverse-mapped to
// a session_id. The table (and its vectors table) are dropped and recreated
// empty. This is acceptable at the current early stage of the system.
//
// Triggered when the old source_fingerprint column is detected. After the
// rebuild, the schema matches model.AgentExperience and no further action
// is needed on subsequent startups.
func rebuildAgentExperiencesForSourceID() {
	if !DB.Migrator().HasTable(&model.AgentExperience{}) {
		return
	}
	// Only rebuild when the deprecated source_fingerprint column is still present.
	if !DB.Migrator().HasColumn(&model.AgentExperience{}, "source_fingerprint") {
		return
	}

	applogger.Info("Rebuilding agent_experiences: replace source_fingerprint with source_id (clearing existing rows)")

	// Drop both the experience table and its vectors table — vectors are
	// meaningless without their parent rows and would orphan if kept.
	if DB.Migrator().HasTable(&model.AgentExperienceVector{}) {
		if err := DB.Migrator().DropTable(&model.AgentExperienceVector{}); err != nil {
			applogger.Error("Failed to drop agent_experience_vectors during rebuild", "error", err)
			return
		}
	}
	if err := DB.Migrator().DropTable(&model.AgentExperience{}); err != nil {
		applogger.Error("Failed to drop agent_experiences during rebuild", "error", err)
		return
	}

	// Recreate both tables from the current model definitions.
	if err := DB.AutoMigrate(&model.AgentExperience{}, &model.AgentExperienceVector{}); err != nil {
		applogger.Error("Failed to recreate agent_experiences tables", "error", err)
		return
	}

	applogger.Info("Successfully rebuilt agent_experiences with source_id column")
}

// rebuildUploadedSkillsStateless removes the status and fingerprint columns
// from the uploaded_skills table, renames source_name → file_name, and adds
// the title column. UploadedSkill is now a stateless record — concurrency
// control is handled in-memory (sync.Map), and deduplication via fingerprint
// unique index has been removed.
//
// Data (source_name → file_name, raw_content) is preserved during the rebuild.
// Triggered when any deprecated column (status, fingerprint, source_name) is
// detected. After rebuild, the schema matches model.UploadedSkill.
func rebuildUploadedSkillsStateless() {
	if !DB.Migrator().HasTable(&model.UploadedSkill{}) {
		return
	}
	// Check for any deprecated columns that indicate the table needs rebuilding.
	hasStatus := DB.Migrator().HasColumn(&model.UploadedSkill{}, "status")
	hasFingerprint := DB.Migrator().HasColumn(&model.UploadedSkill{}, "fingerprint")
	hasSourceName := DB.Migrator().HasColumn(&model.UploadedSkill{}, "source_name")
	if !hasStatus && !hasFingerprint && !hasSourceName {
		return
	}

	applogger.Info("Rebuilding uploaded_skills: rename source_name → file_name, add title, remove status/fingerprint")

	DB.Exec(`
		CREATE TABLE uploaded_skills_new (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			file_name   VARCHAR(500) NOT NULL DEFAULT '',
			title       VARCHAR(500) NOT NULL DEFAULT '',
			raw_content TEXT NOT NULL,
			created_at  DATETIME NOT NULL
		)
	`)
	// Copy data: source_name → file_name (if column exists), title defaults to ''.
	// Existing records' titles are left empty — their pubexps are already Active
	// with LLM-generated titles, so the placeholder title is not needed.
	if hasSourceName {
		DB.Exec(`INSERT INTO uploaded_skills_new (id, file_name, title, raw_content, created_at)
			SELECT id, source_name, '', raw_content, created_at FROM uploaded_skills`)
	} else {
		DB.Exec(`INSERT INTO uploaded_skills_new (id, file_name, title, raw_content, created_at)
			SELECT id, '', '', raw_content, created_at FROM uploaded_skills`)
	}
	DB.Exec(`DROP TABLE uploaded_skills`)
	DB.Exec(`ALTER TABLE uploaded_skills_new RENAME TO uploaded_skills`)

	applogger.Info("Successfully rebuilt uploaded_skills (file_name + title, stateless)")
}

// migrateEnumColumnsToInt converts all enum columns in the database from
// TEXT storage (deprecated) to INTEGER storage. After this migration,
// all enum comparisons use integer constants.
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

	applogger.Info("Migrating participant_sessions: enum TEXT → INTEGER")

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

	applogger.Info("Successfully migrated participant_sessions enum columns to INTEGER")
}

// migrateMessagesRoleToInt rebuilds messages with role as INTEGER column.
func migrateMessagesRoleToInt() {
	if !DB.Migrator().HasTable(&model.Message{}) {
		return
	}
	if !needEnumMigration("messages", "role") {
		return
	}

	applogger.Info("Migrating messages: role TEXT → INTEGER")

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

	applogger.Info("Successfully migrated messages.role to INTEGER")
}

// migrateDocumentsStatusToInt rebuilds documents with status as INTEGER column.
func migrateDocumentsStatusToInt() {
	if !DB.Migrator().HasTable(&model.Document{}) {
		return
	}
	if !needEnumMigration("documents", "status") {
		return
	}

	applogger.Info("Migrating documents: status TEXT → INTEGER")

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

	applogger.Info("Successfully migrated documents.status to INTEGER")
}

// migrateKnowledgeBasesIndexTypeToInt rebuilds knowledge_bases with index_type as INTEGER column.
func migrateKnowledgeBasesIndexTypeToInt() {
	if !DB.Migrator().HasTable(&model.KnowledgeBase{}) {
		return
	}
	if !needEnumMigration("knowledge_bases", "index_type") {
		return
	}

	applogger.Info("Migrating knowledge_bases: index_type TEXT → INTEGER")

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

	applogger.Info("Successfully migrated knowledge_bases.index_type to INTEGER")
}

// migrateInteractionsTable migrates the interactions table schema.
// Phase 1: user_msg_id + agent_msg_id → draft_id (legacy)
// Phase 2: draft_id → work_id (current)
//
// Interactions are now grouped by work_id, directly modeling the relationship
// between a Work and its execution trace, independent of the message stream.
func migrateInteractionsTable() {
	if !DB.Migrator().HasTable(&model.Interaction{}) {
		return
	}

	// Phase 2: draft_id → work_id
	// If work_id column already exists, migration is fully done
	if DB.Migrator().HasColumn(&model.Interaction{}, "work_id") {
		return
	}
	// If draft_id column exists, migrate from draft_id to work_id
	if DB.Migrator().HasColumn(&model.Interaction{}, "draft_id") {
		applogger.Info("Migrating interactions table: draft_id → work_id")

		DB.Exec(`
			CREATE TABLE interactions_new (
				id         INTEGER PRIMARY KEY AUTOINCREMENT,
				session_id INTEGER NOT NULL,
				work_id    INTEGER NOT NULL,
				iteration  INTEGER NOT NULL,
				type       INTEGER NOT NULL,
				data       TEXT NOT NULL,
				created_at DATETIME NOT NULL,
				updated_at DATETIME NOT NULL
			)
		`)
		// Map draft_id to work_id: look up works table by draft_id.
		// For records with no matching work, default work_id to 0.
		DB.Exec(`INSERT INTO interactions_new (id, session_id, work_id, iteration, type, data, created_at, updated_at)
			SELECT i.id, i.session_id,
				COALESCE((SELECT w.id FROM works w WHERE w.draft_id = i.draft_id LIMIT 1), 0),
				i.iteration, i.type, i.data, i.created_at, i.updated_at
			FROM interactions i`)
		DB.Exec(`DROP TABLE interactions`)
		DB.Exec(`ALTER TABLE interactions_new RENAME TO interactions`)
		DB.Exec(`CREATE INDEX idx_interactions_session_id ON interactions(session_id)`)
		DB.Exec(`CREATE INDEX idx_interactions_work_id ON interactions(work_id)`)

		applogger.Info("Successfully migrated interactions table to work_id schema")
		return
	}

	// Phase 1 (legacy): user_msg_id + agent_msg_id → work_id
	// If the old user_msg_id column doesn't exist, nothing to migrate
	if !DB.Migrator().HasColumn(&model.Interaction{}, "user_msg_id") {
		return
	}

	applogger.Info("Migrating interactions table: user_msg_id+agent_msg_id → work_id")

	DB.Exec(`
		CREATE TABLE interactions_new (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id INTEGER NOT NULL,
			work_id    INTEGER NOT NULL,
			iteration  INTEGER NOT NULL,
			type       INTEGER NOT NULL,
			data       TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		)
	`)
	DB.Exec(`INSERT INTO interactions_new (id, session_id, work_id, iteration, type, data, created_at, updated_at)
		SELECT id, session_id, 0, iteration, type, data, created_at, updated_at FROM interactions`)
	DB.Exec(`DROP TABLE interactions`)
	DB.Exec(`ALTER TABLE interactions_new RENAME TO interactions`)
	DB.Exec(`CREATE INDEX idx_interactions_session_id ON interactions(session_id)`)
	DB.Exec(`CREATE INDEX idx_interactions_work_id ON interactions(work_id)`)

	applogger.Info("Successfully migrated interactions table to work_id schema")
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
