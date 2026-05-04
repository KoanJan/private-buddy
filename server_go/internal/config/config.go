package config

import (
	"os"
	"path/filepath"
	"strconv"
)

var globalSettings *Settings

type Settings struct {
	DataRoot               string
	SummaryWindowSize      int
	LogLevel               string
	TaskMaxIterations      int
	WorkspaceRoot          string
	ContextWindowIterations int
	NotesMaxChars          int
}

func Init() {
	dataRoot := getEnv("DATA_ROOT", filepath.Join("..", "data"))
	
	globalSettings = &Settings{
		DataRoot:               dataRoot,
		SummaryWindowSize:      getEnvInt("SUMMARY_WINDOW_SIZE", 5),
		LogLevel:               getEnv("LOG_LEVEL", "INFO"),
		TaskMaxIterations:      getEnvInt("TASK_MAX_ITERATIONS", 50),
		WorkspaceRoot:          getEnv("WORKSPACE_ROOT", ""),
		ContextWindowIterations: getEnvInt("CONTEXT_WINDOW_ITERATIONS", 10),
		NotesMaxChars:          getEnvInt("NOTES_MAX_CHARS", 5000),
	}
}

func Get() *Settings {
	if globalSettings == nil {
		Init()
	}
	return globalSettings
}

func (s *Settings) GetDataRoot() string {
	return s.DataRoot
}

func (s *Settings) DatabaseURL() string {
	return filepath.Join(s.DataRoot, "db", "private_buddy.db")
}

func (s *Settings) VectorDBFile() string {
	return filepath.Join(s.DataRoot, "db", "vectors_go.db")
}

func (s *Settings) GetWorkspaceRoot() string {
	if s.WorkspaceRoot != "" {
		return s.WorkspaceRoot
	}
	return filepath.Join(s.DataRoot, "workspace")
}

func (s *Settings) GetAvatarsDir() string {
	return filepath.Join(s.DataRoot, "avatars")
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if val := os.Getenv(key); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			return n
		}
	}
	return fallback
}
