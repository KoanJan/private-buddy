// Package config provides application-wide configuration management.
//
// Configuration is loaded from environment variables with sensible defaults.
// The global Settings singleton is initialized on first access via Get().
//
// Two deployment modes:
//   - Electron packaging: PORT and DATA_ROOT are injected by the main process;
//     .env file is not included in the bundle.
//   - Standalone server: .env file is required; DATA_ROOT defaults to ../data (project root).
//
// Environment variables:
//   - DATA_ROOT: Root directory for all data storage (default: ../data, relative to executable)
//   - PORT: Server listen port (default: 8000)
//   - SUMMARY_WINDOW_SIZE: Number of messages before triggering summary generation (default: 5)
//   - LOG_LEVEL: Logging level (default: INFO)
//   - TASK_MAX_ITERATIONS: Maximum iterations for task loop (default: 50)
//   - WORKSPACE_ROOT: Root directory for task workspace files (default: DATA_ROOT/workspace)
//   - CONTEXT_WINDOW_ITERATIONS: Number of recent iterations visible to agent (default: 10)
//   - NOTES_MAX_CHARS: Maximum character limit for agent notes (default: 5000)
package config

import (
	"os"
	"path/filepath"
	"strconv"
)

// globalSettings is the singleton configuration instance.
var globalSettings *Settings

// Settings holds all application configuration values.
type Settings struct {
	DataRoot                string // Root directory for all data storage
	SummaryWindowSize       int    // Number of messages before triggering summary generation
	LogLevel                string // Logging level (DEBUG, INFO, WARN, ERROR)
	TaskMaxIterations       int    // Maximum iterations for task loop
	WorkspaceRoot           string // Root directory for task workspace files
	ContextWindowIterations int    // Number of recent iterations visible to agent
	NotesMaxChars           int    // Maximum character limit for agent notes
}

// Init loads configuration from environment variables with defaults.
func Init() {
	dataRoot := expandHome(getEnv("DATA_ROOT", filepath.Join("..", "data")))

	globalSettings = &Settings{
		DataRoot:                dataRoot,
		SummaryWindowSize:       getEnvInt("SUMMARY_WINDOW_SIZE", 5),
		LogLevel:                getEnv("LOG_LEVEL", "INFO"),
		TaskMaxIterations:       getEnvInt("TASK_MAX_ITERATIONS", 50),
		WorkspaceRoot:           expandHome(getEnv("WORKSPACE_ROOT", "")),
		ContextWindowIterations: getEnvInt("CONTEXT_WINDOW_ITERATIONS", 10),
		NotesMaxChars:           getEnvInt("NOTES_MAX_CHARS", 5000),
	}
}

// Get returns the global Settings instance, initializing it if necessary.
func Get() *Settings {
	if globalSettings == nil {
		Init()
	}
	return globalSettings
}

// GetDataRoot returns the data root directory path.
func (s *Settings) GetDataRoot() string {
	return s.DataRoot
}

// DatabaseURL returns the SQLite database file path.
func (s *Settings) DatabaseURL() string {
	return filepath.Join(s.DataRoot, "db", "private_buddy.db")
}

// VectorDBFile returns the vector database file path for RAG operations.
func (s *Settings) VectorDBFile() string {
	return filepath.Join(s.DataRoot, "db", "vectors_go.db")
}

// GetWorkspaceRoot returns the workspace root directory path.
// Falls back to DATA_ROOT/workspace if WORKSPACE_ROOT is not explicitly set.
func (s *Settings) GetWorkspaceRoot() string {
	if s.WorkspaceRoot != "" {
		return s.WorkspaceRoot
	}
	return filepath.Join(s.DataRoot, "workspace")
}

// GetAvatarsDir returns the directory path for agent avatar images.
func (s *Settings) GetAvatarsDir() string {
	return filepath.Join(s.DataRoot, "avatars")
}

// getEnv returns the environment variable value or the fallback if not set.
func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

// getEnvInt returns the environment variable value as an integer or the fallback if not set/invalid.
func getEnvInt(key string, fallback int) int {
	if val := os.Getenv(key); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			return n
		}
	}
	return fallback
}

// homeDir returns the current user's home directory.
func homeDir() string {
	if dir, err := os.UserHomeDir(); err == nil {
		return dir
	}
	return os.Getenv("HOME")
}

// expandHome replaces a leading ~/ in the path with the user's home directory.
// Returns the path unchanged if it does not start with ~/.
func expandHome(path string) string {
	if len(path) >= 2 && path[:2] == "~/" {
		return filepath.Join(homeDir(), path[2:])
	}
	return path
}
