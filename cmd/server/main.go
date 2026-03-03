package main

import (
	"cyberstrike-ai/internal/app"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/logger"
	"flag"
	"fmt"
)

func main() {
	var configPath = flag.String("config", "config.yaml", "Configuration file path")
	flag.Parse()

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Printf("Failed to load configuration: %v\n", err)
		return
	}

	// Initialization log
	log := logger.New(cfg.Log.Level, cfg.Log.Output)

	// Create app
	application, err := app.New(cfg, log)
	if err != nil {
		log.Fatal("Application initialization failed", "error", err)
	}

	// Start the server
	if err := application.Run(); err != nil {
		log.Fatal("Server startup failed", "error", err)
	}
}

