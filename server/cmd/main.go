package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"private-buddy-server/internal/api"
	"private-buddy-server/internal/api/handler"
	"private-buddy-server/internal/config"
	"private-buddy-server/internal/database"
	"private-buddy-server/internal/logger"
	"private-buddy-server/internal/service/kb"
	"private-buddy-server/internal/service/runtime"

	applogger "private-buddy-server/internal/logger"

	"github.com/joho/godotenv"
)

func main() {
	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)
	envFile := filepath.Join(exeDir, ".env")
	if err := godotenv.Load(envFile); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not load %s: %v\n", envFile, err)
	}
	if err := godotenv.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not load .env from cwd: %v\n", err)
	}

	config.Init()
	logger.Init()

	applogger.L.Info("Starting Private Buddy Server")

	database.Init()
	database.AutoMigrate()

	// Initialize Agent Runtime system with SSE status change callback
	onStatusChange := func(agentID, sessionID int64, status int) {
		data, _ := json.Marshal(map[string]interface{}{
			"type":       "agent_status",
			"agent_id":   agentID,
			"session_id": sessionID,
			"status":     status,
		})
		handler.PushSSEToSession(sessionID, string(data))
	}
	runtime.InitGlobalRuntimeManager(onStatusChange)

	// Connect runtime's pushMessageEvent to SSE
	runtime.SetPushMessageEvent(func(sessionID, messageID int64, content string, hasInteractions int) {
		data, _ := json.Marshal(map[string]interface{}{
			"type":             "message",
			"message_id":       messageID,
			"content":          content,
			"has_interactions": hasInteractions,
		})
		handler.PushSSEToSession(sessionID, string(data))
	})

	// Connect runtime's pushSSEEvent to SSE (for notifications and other raw events)
	runtime.SetPushSSEEvent(func(sessionID int64, data string) {
		handler.PushSSEToSession(sessionID, data)
	})

	kb.Init(1536, 0)
	kb.RecoverProcessingDocuments()

	r := api.SetupRouter()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}
	addr := fmt.Sprintf(":%s", port)
	applogger.L.Info("Server listening", "addr", addr)
	if err := r.Run(addr); err != nil {
		applogger.L.Error("Server failed to start", "error", err)
		panic(err)
	}
}
