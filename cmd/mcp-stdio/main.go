package main

import (
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/logger"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/security"
	"flag"
	"fmt"
	"os"

	"go.uber.org/zap"
)

func main() {
	var configPath = flag.String("config", "config.yaml", "Configuration file path")
	flag.Parse()

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Initialization log (use stderr in stdio mode to output logs to avoid interfering with JSON-RPC communication)
	log := logger.New(cfg.Log.Level, "stderr")

	// Create MCP server
	mcpServer := mcp.NewServer(log.Logger)

	// Create a security tool executor
	executor := security.NewExecutor(&cfg.Security, mcpServer, log.Logger)

	// Registration tool
	executor.RegisterTools(mcpServer)

	log.Logger.Info("MCP server (stdio mode) started, waiting for messages...")

	// Run stdio loop
	if err := mcpServer.HandleStdio(); err != nil {
		log.Logger.Error("MCP server failed to run", zap.Error(err))
		os.Exit(1)
	}
}

