package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"private-buddy-server/internal/api"
	"private-buddy-server/internal/api/handler"
	"private-buddy-server/internal/config"
	"private-buddy-server/internal/database"
	"private-buddy-server/internal/logger"
	"private-buddy-server/internal/service/eventqueue"
	"private-buddy-server/internal/service/kb"
	"private-buddy-server/internal/service/runtime"
	"private-buddy-server/internal/service/task/tools"

	applogger "private-buddy-server/internal/logger"

	"github.com/joho/godotenv"
)

// safeMarshalSSE marshals data to JSON for SSE push, logging on failure.
// Returns the JSON string or an empty string if marshaling fails.
func safeMarshalSSE(data map[string]interface{}) string {
	bytes, err := json.Marshal(data)
	if err != nil {
		applogger.L.Error("Failed to marshal SSE event data", "error", err)
		return ""
	}
	return string(bytes)
}

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

	// Initialize the Agent Runtime system with SSE callbacks
	onStatusChange := func(agentID, sessionID int64, status int) {
		data := safeMarshalSSE(map[string]interface{}{
			"type":       "agent_status",
			"agent_id":   agentID,
			"session_id": sessionID,
			"status":     status,
		})
		if data != "" {
			handler.PushSSEToSession(sessionID, data)
		}
	}
	onPushMessage := func(sessionID, messageID int64, content string, hasInteractions int) {
		data := safeMarshalSSE(map[string]interface{}{
			"type":             "message",
			"message_id":       messageID,
			"content":          content,
			"has_interactions": hasInteractions,
		})
		if data != "" {
			handler.PushSSEToSession(sessionID, data)
		}
	}

	// Initialize the global event queue first, before runtimes subscribe to it
	eventqueue.Init()

	runtime.Start(onStatusChange, onPushMessage, handler.PushSSEToSession)

	kb.Init(1536, 0)
	kb.RecoverProcessingDocuments()

	r := api.SetupRouter()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}
	addr := fmt.Sprintf(":%s", port)

	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	// Start server in a goroutine so we can listen for shutdown signals
	go func() {
		applogger.L.Info("Server listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			applogger.L.Error("Server failed to start", "error", err)
			panic(err)
		}
	}()

	// Wait for interrupt signal for graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	applogger.L.Info("Received shutdown signal", "signal", sig.String())

	// Stop all agent runtimes and event queue before shutting down
	applogger.L.Info("Stopping agent runtimes...")
	runtime.StopAll()

	// Cancel all pending alarm goroutines
	applogger.L.Info("Cancelling pending alarms...")
	tools.CancelAlarms()

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		applogger.L.Error("Server forced to shutdown", "error", err)
	}

	applogger.L.Info("Server stopped gracefully")
}
