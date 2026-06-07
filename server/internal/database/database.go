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
	}

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

// IndexDef holds an index name and its CREATE statement.
type IndexDef struct {
	Name string
	SQL  string
}

// getTableIndexes returns index definitions for a table from sqlite_master.
func getTableIndexes(tableName string) []IndexDef {
	var indexes []IndexDef
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
func recreateIndexes(indexes []IndexDef) {
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
