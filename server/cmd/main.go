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
	"private-buddy-server/internal/service"
	"private-buddy-server/internal/service/eventqueue"
	"private-buddy-server/internal/service/experience"
	"private-buddy-server/internal/service/kb"
	"private-buddy-server/internal/service/llm"
	"private-buddy-server/internal/service/memory"
	"private-buddy-server/internal/service/runtime"

	applogger "private-buddy-server/internal/logger"

	"github.com/joho/godotenv"
)

// safeMarshalSSE marshals data to JSON for SSE push, logging on failure.
// Returns the JSON string or an empty string if marshaling fails.
func safeMarshalSSE(data map[string]interface{}) string {
	bytes, err := json.Marshal(data)
	if err != nil {
		applogger.Error("Failed to marshal SSE event data", "error", err)
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
	applogger.Info("Private Buddy Server config initialized", "config", config.Get())

	applogger.Info("Starting Private Buddy Server")

	database.Init()
	database.AutoMigrate()
	llm.LoadCapabilityCache()

	// Initialize the embedding service once — shared by memory and experience systems.
	embSvc := getEmbeddingService()

	// Memory system: event vectorization + daily cron
	memory.Init(embSvc)
	memCtx, memCancel := context.WithCancel(context.Background())
	go memory.Start(memCtx)

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
	onPushMessage := func(sessionID, messageID int64, content string) {
		data := safeMarshalSSE(map[string]interface{}{
			"type":       "message",
			"message_id": messageID,
			"content":    content,
		})
		if data != "" {
			handler.PushSSEToSession(sessionID, data)
		}
	}

	// Experience system: semantic retrieval for tasks + heartbeat-triggered reflection.
	experience.Init(embSvc)

	// Initialize the global event queue first, before runtimes subscribe to it
	eventqueue.Init()

	// Start all agent runtimes and recover orphaned scheduled events.
	// recoverScheduledEvents() is called inside Start() after all runtimes
	// have subscribed to the event queue.
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
		applogger.Info("Server listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			applogger.Error("Server failed to start", "error", err)
			panic(err)
		}
	}()

	// Wait for interrupt signal for graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	applogger.Info("Received shutdown signal", "signal", sig.String())

	// Stop all agent runtimes and wait for graceful completion.
	applogger.Info("Stopping agent runtimes...")
	runtime.Shutdown(10 * time.Second)

	// Shut down the memory system (vectorization + daily cron)
	memCancel()

	// Close all SSE connections so HTTP shutdown doesn't wait for keep-alive
	handler.ShutdownSSE()

	// Graceful HTTP shutdown — returns immediately since SSE connections are closed
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		applogger.Warn("HTTP server shutdown", "error", err)
	}

	applogger.Info("Server stopped gracefully")
}

// getEmbeddingService creates an EmbeddingService from the global embedding config.
// Returns nil if no embedding config exists.
func getEmbeddingService() *llm.EmbeddingService {
	embConfig := service.GetEmbeddingConfig()
	if embConfig == nil {
		return nil
	}

	embSvc := llm.NewEmbeddingService(embConfig.BaseURL, embConfig.APIKey, embConfig.ModelID, 1536)
	applogger.Info("Embedding service created",
		"config_name", embConfig.Name,
		"model", embConfig.ModelID,
	)
	return embSvc
}
