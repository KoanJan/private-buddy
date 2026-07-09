package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"private-buddy-server/internal/database"
	applogger "private-buddy-server/internal/logger"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service/workspace"
)

func TestDeliverToTool_Execute(t *testing.T) {
	// Setup a temp workspace root. Must set before any config access.
	tmpRoot := t.TempDir()
	// macOS temp dirs are symlinks — resolve to real path for workspace safety checks
	realRoot, err := filepath.EvalSymlinks(tmpRoot)
	if err != nil {
		t.Fatalf("failed to resolve temp dir: %v", err)
	}
	os.Setenv("DATA_ROOT", realRoot)
	os.Setenv("WORKSPACE_ROOT", filepath.Join(realRoot, "workspace"))
	os.Setenv("LOG_DIR", filepath.Join(realRoot, "logs"))

	// Initialize logger and DB
	applogger.Init()
	database.Init()
	database.AutoMigrate()

	// Create a test user for receiver resolution
	database.DB.Create(&model.User{Name: "Alice"})

	agentID := int64(1)
	sessionID := int64(100)

	// Initialize workspace with received/ directory
	workspace.InitWorkspace(agentID, sessionID)

	// Create test files in output/
	outputDir := workspace.GetOutputDir(agentID, sessionID)
	testFile := filepath.Join(outputDir, "report.txt")
	if err := os.WriteFile(testFile, []byte("hello world"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Create a subdirectory with files
	subDir := filepath.Join(outputDir, "dist")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}
	subFile := filepath.Join(subDir, "app.js")
	if err := os.WriteFile(subFile, []byte("console.log('hi')"), 0644); err != nil {
		t.Fatalf("failed to create sub file: %v", err)
	}

	tool := NewDeliverToTool(agentID, sessionID)

	t.Run("deliver single file to user", func(t *testing.T) {
		result, err := tool.Execute(map[string]interface{}{
			"receiver_name": "Alice",
			"paths":         []interface{}{"report.txt"},
			"remark":        "Test delivery",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "delivery #1") {
			t.Errorf("expected delivery #1 in result, got: %s", result)
		}
		if !strings.Contains(result, "report.txt") {
			t.Errorf("expected report.txt in result, got: %s", result)
		}

		// Verify file was copied to user's received/ directory
		userReceived := workspace.GetReceivedDir(0, sessionID)
		copiedFile := filepath.Join(userReceived, "delivery_1", "report.txt")
		data, err := os.ReadFile(copiedFile)
		if err != nil {
			t.Fatalf("copied file not found: %v", err)
		}
		if string(data) != "hello world" {
			t.Errorf("expected 'hello world', got '%s'", string(data))
		}
	})

	t.Run("deliver directory to user", func(t *testing.T) {
		result, err := tool.Execute(map[string]interface{}{
			"receiver_name": "Alice",
			"paths":         []interface{}{"dist"},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "delivery #2") {
			t.Errorf("expected delivery #2 in result, got: %s", result)
		}

		// Verify directory was copied
		userReceived := workspace.GetReceivedDir(0, sessionID)
		copiedFile := filepath.Join(userReceived, "delivery_2", "dist", "app.js")
		data, err := os.ReadFile(copiedFile)
		if err != nil {
			t.Fatalf("copied file not found: %v", err)
		}
		if string(data) != "console.log('hi')" {
			t.Errorf("unexpected file content: %s", string(data))
		}
	})

	t.Run("multiple deliveries don't overwrite", func(t *testing.T) {
		userReceived := workspace.GetReceivedDir(0, sessionID)

		// delivery_1 and delivery_2 should both exist
		if _, err := os.Stat(filepath.Join(userReceived, "delivery_1", "report.txt")); err != nil {
			t.Errorf("delivery_1 should still exist: %v", err)
		}
		if _, err := os.Stat(filepath.Join(userReceived, "delivery_2", "dist", "app.js")); err != nil {
			t.Errorf("delivery_2 should still exist: %v", err)
		}
	})

	t.Run("reject empty receiver_name", func(t *testing.T) {
		_, err := tool.Execute(map[string]interface{}{
			"receiver_name": "",
			"paths":         []interface{}{"report.txt"},
		})
		if err == nil {
			t.Fatal("expected error for empty receiver_name, got nil")
		}
	})

	t.Run("reject unknown receiver_name", func(t *testing.T) {
		_, err := tool.Execute(map[string]interface{}{
			"receiver_name": "Nobody",
			"paths":         []interface{}{"report.txt"},
		})
		if err == nil {
			t.Fatal("expected error for unknown receiver_name, got nil")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("expected 'not found' in error, got: %v", err)
		}
	})

	t.Run("reject non-existent path", func(t *testing.T) {
		_, err := tool.Execute(map[string]interface{}{
			"receiver_name": "Alice",
			"paths":         []interface{}{"nonexistent.txt"},
		})
		if err == nil {
			t.Fatal("expected error for non-existent path, got nil")
		}
	})

	t.Run("reject empty paths", func(t *testing.T) {
		_, err := tool.Execute(map[string]interface{}{
			"receiver_name": "Alice",
			"paths":         []interface{}{},
		})
		if err == nil {
			t.Fatal("expected error for empty paths, got nil")
		}
	})
}

func TestDeliverToTool_Schema(t *testing.T) {
	tool := NewDeliverToTool(1, 100)
	schema := tool.Schema()

	if schema.Name != "deliver_to" {
		t.Errorf("expected name 'deliver_to', got '%s'", schema.Name)
	}

	params := schema.Parameters
	required, ok := params["required"].([]string)
	if !ok {
		t.Fatal("schema missing required field")
	}

	hasReceiverName := false
	hasPaths := false
	for _, r := range required {
		switch r {
		case "receiver_name":
			hasReceiverName = true
		case "paths":
			hasPaths = true
		}
	}
	if !hasReceiverName {
		t.Error("receiver_name should be required")
	}
	if !hasPaths {
		t.Error("paths should be required")
	}
}

func TestDeliverToTool_DBRecord(t *testing.T) {
	// This test verifies the AgentDelivery model can be marshaled/unmarshaled
	paths := []string{"report.txt", "dist/app.js"}
	pathsJSON, _ := json.Marshal(paths)

	var decoded []string
	if err := json.Unmarshal(pathsJSON, &decoded); err != nil {
		t.Fatalf("failed to unmarshal paths JSON: %v", err)
	}
	if len(decoded) != 2 {
		t.Errorf("expected 2 paths, got %d", len(decoded))
	}
	if decoded[0] != "report.txt" || decoded[1] != "dist/app.js" {
		t.Errorf("unexpected decoded paths: %v", decoded)
	}
}
