package main

import (
	"fmt"
	"os"
	"path/filepath"

	"private-buddy-server/internal/api"
	"private-buddy-server/internal/config"
	"private-buddy-server/internal/database"
	"private-buddy-server/internal/logger"

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

	r := api.SetupRouter(database.DB)

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
