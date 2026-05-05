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
	"time"

	"private-buddy-server/internal/config"
	applogger "private-buddy-server/internal/logger"
	"private-buddy-server/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// DB is the global database connection instance.
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

// AutoMigrate creates all database tables that don't already exist.
// Also seeds default data for search_config and db_version tables.
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
	}

	for _, m := range models {
		if !hasTable(m) {
			if err := DB.AutoMigrate(m); err != nil {
				panic(fmt.Sprintf("Failed to auto-migrate %T: %v", m, err))
			}
			applogger.L.Info("Created table", "model", fmt.Sprintf("%T", m))
		}
	}

	ensureSearchConfig()
	ensureDBVersion()
	applogger.L.Info("Database migration completed")
}

// hasTable checks if a database table already exists for the given model.
func hasTable(m interface{}) bool {
	stmt := &gorm.Statement{DB: DB}
	if err := stmt.Parse(m); err != nil {
		return false
	}
	return DB.Migrator().HasTable(stmt.Table)
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
