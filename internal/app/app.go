package app

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/handler"
	"cyberstrike-ai/internal/knowledge"
	"cyberstrike-ai/internal/robot"
	"cyberstrike-ai/internal/logger"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/mcp/builtin"
	"cyberstrike-ai/internal/openai"
	"cyberstrike-ai/internal/security"
	"cyberstrike-ai/internal/skills"
	"cyberstrike-ai/internal/storage"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// App application
type App struct {
	config             *config.Config
	logger             *logger.Logger
	router             *gin.Engine
	mcpServer          *mcp.Server
	externalMCPMgr     *mcp.ExternalMCPManager
	agent              *agent.Agent
	executor           *security.Executor
	db                 *database.DB
	knowledgeDB        *database.DB // Knowledge base database connection (if using a standalone database)
	auth               *security.AuthManager
	knowledgeManager   *knowledge.Manager        // Knowledge Base Manager (for dynamic initialization)
	knowledgeRetriever *knowledge.Retriever      // Knowledge base retriever (for dynamic initialization)
	knowledgeIndexer   *knowledge.Indexer        // Knowledge base indexer (for dynamic initialization)
	knowledgeHandler   *handler.KnowledgeHandler // Knowledge base processor (for dynamic initialization)
	agentHandler       *handler.AgentHandler     // Agent processor (used to update the knowledge base manager)
	robotHandler       *handler.RobotHandler     // Robot processor (DingTalk/Feishu/Enterprise WeChat)
	robotMu            sync.Mutex                 // Protect DingTalk/Feishu long connection cancel
	dingCancel         context.CancelFunc        // DingTalk Stream cancellation function, used to restart when configuration changes
	larkCancel         context.CancelFunc        // Feishu long connection cancellation function, used to restart when configuration changes
}

// New Create a new application
func New(cfg *config.Config, log *logger.Logger) (*App, error) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()

	// CORS middleware
	router.Use(corsMiddleware())

	// Authentication Manager
	authManager, err := security.NewAuthManager(cfg.Auth.Password, cfg.Auth.SessionDurationHours)
	if err != nil {
		return nil, fmt.Errorf("Initial authentication failed: %w", err)
	}

	// Initialize database
	dbPath := cfg.Database.Path
	if dbPath == "" {
		dbPath = "data/conversations.db"
	}

	// Make sure the directory exists
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("Failed to create database directory: %w", err)
	}

	db, err := database.NewDB(dbPath, log.Logger)
	if err != nil {
		return nil, fmt.Errorf("Failed to initialize database: %w", err)
	}

	// Create MCP server (with database persistence)
	mcpServer := mcp.NewServerWithStorage(log.Logger, db)

	// Create a security tool executor
	executor := security.NewExecutor(&cfg.Security, mcpServer, log.Logger)

	// Registration tool
	executor.RegisterTools(mcpServer)

	// Register vulnerability logging tool
	registerVulnerabilityTool(mcpServer, db, log.Logger)

	if cfg.Auth.GeneratedPassword != "" {
		config.PrintGeneratedPasswordWarning(cfg.Auth.GeneratedPassword, cfg.Auth.GeneratedPasswordPersisted, cfg.Auth.GeneratedPasswordPersistErr)
		cfg.Auth.GeneratedPassword = ""
		cfg.Auth.GeneratedPasswordPersisted = false
		cfg.Auth.GeneratedPasswordPersistErr = ""
	}

	// Create an external MCP manager (using the same storage as the internal MCP server)
	externalMCPMgr := mcp.NewExternalMCPManagerWithStorage(log.Logger, db)
	if cfg.ExternalMCP.Servers != nil {
		externalMCPMgr.LoadConfigs(&cfg.ExternalMCP)
		// Start all enabled external MCP clients
		externalMCPMgr.StartAllEnabled()
	}

	// Initialize result storage
	resultStorageDir := "tmp"
	if cfg.Agent.ResultStorageDir != "" {
		resultStorageDir = cfg.Agent.ResultStorageDir
	}

	// Make sure the storage directory exists
	if err := os.MkdirAll(resultStorageDir, 0755); err != nil {
		return nil, fmt.Errorf("Failed to create results storage directory: %w", err)
	}

	// Create a result storage instance
	resultStorage, err := storage.NewFileResultStorage(resultStorageDir, log.Logger)
	if err != nil {
		return nil, fmt.Errorf("Initialization result storage failed: %w", err)
	}

	// CreateAgent
	maxIterations := cfg.Agent.MaxIterations
	if maxIterations <= 0 {
		maxIterations = 30 // Default value
	}
	agent := agent.NewAgent(&cfg.OpenAI, &cfg.Agent, mcpServer, externalMCPMgr, log.Logger, maxIterations)

	// Set the results to be stored in Agent
	agent.SetResultStorage(resultStorage)

	// Set the results to be stored in the Executor (for query tools)
	executor.SetResultStorage(resultStorage)

	// Initialize the knowledge base module (if enabled)
	var knowledgeManager *knowledge.Manager
	var knowledgeRetriever *knowledge.Retriever
	var knowledgeIndexer *knowledge.Indexer
	var knowledgeHandler *handler.KnowledgeHandler

	var knowledgeDBConn *database.DB
	log.Logger.Info("Check knowledge base configuration", zap.Bool("enabled", cfg.Knowledge.Enabled))
	if cfg.Knowledge.Enabled {
		// Determine the knowledge base database path
		knowledgeDBPath := cfg.Database.KnowledgeDBPath
		var knowledgeDB *sql.DB

		if knowledgeDBPath != "" {
			// Use a separate knowledge base database
			// Make sure the directory exists
			if err := os.MkdirAll(filepath.Dir(knowledgeDBPath), 0755); err != nil {
				return nil, fmt.Errorf("Failed to create knowledge base database directory: %w", err)
			}

			var err error
			knowledgeDBConn, err = database.NewKnowledgeDB(knowledgeDBPath, log.Logger)
			if err != nil {
				return nil, fmt.Errorf("Failed to initialize knowledge base database: %w", err)
			}
			knowledgeDB = knowledgeDBConn.DB
			log.Logger.Info("Use a separate knowledge base database", zap.String("path", knowledgeDBPath))
		} else {
			// Backward compatibility: using session database
			knowledgeDB = db.DB
			log.Logger.Info("Use session database to store knowledge base data (it is recommended to configure knowledge_db_path to separate data)")
		}

		// Create a knowledge base manager
		knowledgeManager = knowledge.NewManager(knowledgeDB, cfg.Knowledge.BasePath, log.Logger)

		// Create embedder
		// Use the API Key configured by OpenAI (if not specified in the knowledge base configuration)
		if cfg.Knowledge.Embedding.APIKey == "" {
			cfg.Knowledge.Embedding.APIKey = cfg.OpenAI.APIKey
		}
		if cfg.Knowledge.Embedding.BaseURL == "" {
			cfg.Knowledge.Embedding.BaseURL = cfg.OpenAI.BaseURL
		}

		httpClient := &http.Client{
			Timeout: 30 * time.Minute,
		}
		openAIClient := openai.NewClient(&cfg.OpenAI, httpClient, log.Logger)
		embedder := knowledge.NewEmbedder(&cfg.Knowledge, &cfg.OpenAI, openAIClient, log.Logger)

		// Create a retriever
		retrievalConfig := &knowledge.RetrievalConfig{
			TopK:                cfg.Knowledge.Retrieval.TopK,
			SimilarityThreshold: cfg.Knowledge.Retrieval.SimilarityThreshold,
			HybridWeight:        cfg.Knowledge.Retrieval.HybridWeight,
		}
		knowledgeRetriever = knowledge.NewRetriever(knowledgeDB, embedder, retrievalConfig, log.Logger)

		// Create indexer
		knowledgeIndexer = knowledge.NewIndexer(knowledgeDB, embedder, log.Logger)

		// Register the knowledge retrieval tool to the MCP server
		knowledge.RegisterKnowledgeTool(mcpServer, knowledgeRetriever, knowledgeManager, log.Logger)

		// Create a knowledge base API handler
		knowledgeHandler = handler.NewKnowledgeHandler(knowledgeManager, knowledgeRetriever, knowledgeIndexer, db, log.Logger)
		log.Logger.Info("Knowledge base module initialization completed", zap.Bool("handler_created", knowledgeHandler != nil))

		// Scan and index the knowledge base (asynchronously)
		go func() {
			itemsToIndex, err := knowledgeManager.ScanKnowledgeBase()
			if err != nil {
				log.Logger.Warn("Scanning knowledge base failed", zap.Error(err))
				return
			}

			// Check if there is an index
			hasIndex, err := knowledgeIndexer.HasIndex()
			if err != nil {
				log.Logger.Warn("Checking index status failed", zap.Error(err))
				return
			}

			if hasIndex {
				// If an index already exists, only newly added or updated items are indexed
				if len(itemsToIndex) > 0 {
					log.Logger.Info("An existing knowledge base index is detected and incremental indexing starts.", zap.Int("count", len(itemsToIndex)))
					ctx := context.Background()
					consecutiveFailures := 0
					var firstFailureItemID string
					var firstFailureError error
					failedCount := 0

					for _, itemID := range itemsToIndex {
						if err := knowledgeIndexer.IndexItem(ctx, itemID); err != nil {
							failedCount++
							consecutiveFailures++

							if consecutiveFailures == 1 {
								firstFailureItemID = itemID
								firstFailureError = err
								log.Logger.Warn("Indexing knowledge items failed", zap.String("itemId", itemID), zap.Error(err))
							}

							// If it fails 2 times in a row, stop incremental indexing immediately
							if consecutiveFailures >= 2 {
								log.Logger.Error("There are too many consecutive indexing failures. Stop incremental indexing immediately.",
									zap.Int("consecutiveFailures", consecutiveFailures),
									zap.Int("totalItems", len(itemsToIndex)),
									zap.String("firstFailureItemId", firstFailureItemID),
									zap.Error(firstFailureError),
								)
								break
							}
							continue
						}

						// Reset consecutive failure count on success
						if consecutiveFailures > 0 {
							consecutiveFailures = 0
							firstFailureItemID = ""
							firstFailureError = nil
						}
					}
					log.Logger.Info("Incremental indexing completed", zap.Int("totalItems", len(itemsToIndex)), zap.Int("failedCount", failedCount))
				} else {
					log.Logger.Info("Existing knowledge base index detected, no new or updated items need to be indexed")
				}
				return
			}

			// Automatically rebuild only if there are no indexes
			log.Logger.Info("No knowledge base index detected, automatic index building started")
			ctx := context.Background()
			if err := knowledgeIndexer.RebuildIndex(ctx); err != nil {
				log.Logger.Warn("Rebuilding the knowledge base index failed", zap.Error(err))
			}
		}()
	}

	// Get configuration file path
	configPath := "config.yaml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	// Initialize Skills Manager
	skillsDir := cfg.SkillsDir
	if skillsDir == "" {
		skillsDir = "skills" // Default directory
	}
	// If it is a relative path, it is relative to the directory where the configuration file is located.
	configDir := filepath.Dir(configPath)
	if !filepath.IsAbs(skillsDir) {
		skillsDir = filepath.Join(configDir, skillsDir)
	}
	skillsManager := skills.NewManager(skillsDir, log.Logger)
	log.Logger.Info("Skills manager initialized", zap.String("skillsDir", skillsDir))

	// Register Skills tools to the MCP server (allowing AI to be called on demand, with database storage to support statistics)
	// Create an adapter to adapt database.DB to the SkillStatsStorage interface
	var skillStatsStorage skills.SkillStatsStorage
	if db != nil {
		skillStatsStorage = &skillStatsDBAdapter{db: db}
	}
	skills.RegisterSkillsToolWithStorage(mcpServer, skillsManager, skillStatsStorage, log.Logger)

	// Create handler
	agentHandler := handler.NewAgentHandler(agent, db, cfg, log.Logger)
	agentHandler.SetSkillsManager(skillsManager) // Set up Skills Manager
	// If the knowledge base is enabled, set the knowledge base manager to AgentHandler to log retrievals
	if knowledgeManager != nil {
		agentHandler.SetKnowledgeManager(knowledgeManager)
	}
	monitorHandler := handler.NewMonitorHandler(mcpServer, executor, db, log.Logger)
	monitorHandler.SetExternalMCPManager(externalMCPMgr) // Set up an external MCP manager to obtain external MCP execution records
	groupHandler := handler.NewGroupHandler(db, log.Logger)
	authHandler := handler.NewAuthHandler(authManager, cfg, configPath, log.Logger)
	attackChainHandler := handler.NewAttackChainHandler(db, &cfg.OpenAI, log.Logger)
	vulnerabilityHandler := handler.NewVulnerabilityHandler(db, log.Logger)
	configHandler := handler.NewConfigHandler(configPath, cfg, mcpServer, executor, agent, attackChainHandler, externalMCPMgr, log.Logger)
	externalMCPHandler := handler.NewExternalMCPHandler(externalMCPMgr, cfg, configPath, log.Logger)
	roleHandler := handler.NewRoleHandler(cfg, configPath, log.Logger)
	roleHandler.SetSkillsManager(skillsManager) // Set Skills Manager to RoleHandler
	skillsHandler := handler.NewSkillsHandler(skillsManager, cfg, configPath, log.Logger)
	fofaHandler := handler.NewFofaHandler(cfg, log.Logger)
	terminalHandler := handler.NewTerminalHandler(log.Logger)
	if db != nil {
		skillsHandler.SetDB(db) // Set up a database connection to obtain call statistics
	}

	// Create OpenAPI handler
	conversationHandler := handler.NewConversationHandler(db, log.Logger)
	robotHandler := handler.NewRobotHandler(cfg, db, agentHandler, log.Logger)
	openAPIHandler := handler.NewOpenAPIHandler(db, log.Logger, resultStorage, conversationHandler, agentHandler)

	// Create an App instance (some fields will be filled in later)
	app := &App{
		config:             cfg,
		logger:             log,
		router:             router,
		mcpServer:          mcpServer,
		externalMCPMgr:     externalMCPMgr,
		agent:              agent,
		executor:           executor,
		db:                 db,
		knowledgeDB:        knowledgeDBConn,
		auth:               authManager,
		knowledgeManager:   knowledgeManager,
		knowledgeRetriever: knowledgeRetriever,
		knowledgeIndexer:   knowledgeIndexer,
		knowledgeHandler:   knowledgeHandler,
		agentHandler:       agentHandler,
		robotHandler:       robotHandler,
	}
	// Feishu/DingTalk long connection (no public network required), starts in the background when enabled; will be restarted through RestartRobotConnections during subsequent front-end application configuration
	app.startRobotConnections()

	// Set the vulnerability tool register (built-in tool, must be set)
	vulnerabilityRegistrar := func() error {
		registerVulnerabilityTool(mcpServer, db, log.Logger)
		return nil
	}
	configHandler.SetVulnerabilityToolRegistrar(vulnerabilityRegistrar)

	// Set the Skills tool register (built-in tool, must be set)
	skillsRegistrar := func() error {
		// Create an adapter to adapt database.DB to the SkillStatsStorage interface
		var skillStatsStorage skills.SkillStatsStorage
		if db != nil {
			skillStatsStorage = &skillStatsDBAdapter{db: db}
		}
		skills.RegisterSkillsToolWithStorage(mcpServer, skillsManager, skillStatsStorage, log.Logger)
		return nil
	}
	configHandler.SetSkillsToolRegistrar(skillsRegistrar)

	// Set the knowledge base initializer (for dynamic initialization, needs to be set after the app is created)
	configHandler.SetKnowledgeInitializer(func() (*handler.KnowledgeHandler, error) {
		knowledgeHandler, err := initializeKnowledge(cfg, db, knowledgeDBConn, mcpServer, agentHandler, app, log.Logger)
		if err != nil {
			return nil, err
		}

		// After dynamic initialization, set up the knowledge base tool register and retriever updater
		// In this way, the tool can be re-registered during subsequent ApplyConfig.
		if app.knowledgeRetriever != nil && app.knowledgeManager != nil {
			// Create a closure and capture references to knowledgeRetriever and knowledgeManager
			registrar := func() error {
				knowledge.RegisterKnowledgeTool(mcpServer, app.knowledgeRetriever, app.knowledgeManager, log.Logger)
				return nil
			}
			configHandler.SetKnowledgeToolRegistrar(registrar)
			// Set the retriever updater to update the retriever configuration when ApplyConfig
			configHandler.SetRetrieverUpdater(app.knowledgeRetriever)
			log.Logger.Info("The knowledge base tool registrar and retriever updater have been set after dynamic initialization")
		}

		return knowledgeHandler, nil
	})

	// If the knowledge base is enabled, set the knowledge base tool registrar and retriever updater
	if cfg.Knowledge.Enabled && knowledgeRetriever != nil && knowledgeManager != nil {
		// Create a closure and capture references to knowledgeRetriever and knowledgeManager
		registrar := func() error {
			knowledge.RegisterKnowledgeTool(mcpServer, knowledgeRetriever, knowledgeManager, log.Logger)
			return nil
		}
		configHandler.SetKnowledgeToolRegistrar(registrar)
		// Set the retriever updater to update the retriever configuration when ApplyConfig
		configHandler.SetRetrieverUpdater(knowledgeRetriever)
	}

	// Set up the robot connection restarter. After the front-end application is configured, the new DingTalk/Feishu configuration can take effect without restarting the service.
	configHandler.SetRobotRestarter(app)

	// Set up routing (use App instance to get handler dynamically)
	setupRoutes(
		router,
		authHandler,
		agentHandler,
		monitorHandler,
		conversationHandler,
		robotHandler,
		groupHandler,
		configHandler,
		externalMCPHandler,
		attackChainHandler,
		app, // Pass the App instance to dynamically obtain the knowledgeHandler
		vulnerabilityHandler,
		roleHandler,
		skillsHandler,
		fofaHandler,
		terminalHandler,
		mcpServer,
		authManager,
		openAPIHandler,
	)

	return app, nil

}

// Run to start the application
func (a *App) Run() error {
	// Start MCP server (if enabled)
	if a.config.MCP.Enabled {
		go func() {
			mcpAddr := fmt.Sprintf("%s:%d", a.config.MCP.Host, a.config.MCP.Port)
			a.logger.Info("Start MCP server", zap.String("address", mcpAddr))

			mux := http.NewServeMux()
			mux.HandleFunc("/mcp", a.mcpServer.HandleHTTP)

			if err := http.ListenAndServe(mcpAddr, mux); err != nil {
				a.logger.Error("MCP server failed to start", zap.Error(err))
			}
		}()
	}

	// Start the main server
	addr := fmt.Sprintf("%s:%d", a.config.Server.Host, a.config.Server.Port)
	a.logger.Info("Start HTTP server", zap.String("address", addr))

	return a.router.Run(addr)
}

// Shutdown close the application
func (a *App) Shutdown() {
	// Stop DingTalk/Feishu long connection
	a.robotMu.Lock()
	if a.dingCancel != nil {
		a.dingCancel()
		a.dingCancel = nil
	}
	if a.larkCancel != nil {
		a.larkCancel()
		a.larkCancel = nil
	}
	a.robotMu.Unlock()

	// Stop all external MCP clients
	if a.externalMCPMgr != nil {
		a.externalMCPMgr.StopAll()
	}

	// Close the knowledge base database connection (if using a standalone database)
	if a.knowledgeDB != nil {
		if err := a.knowledgeDB.Close(); err != nil {
			a.logger.Logger.Warn("Failed to close knowledge base database connection", zap.Error(err))
		}
	}
}

// StartRobotConnections starts DingTalk/Feishu long connections based on the current configuration (does not close existing connections first, only for first startup)
func (a *App) startRobotConnections() {
	a.robotMu.Lock()
	defer a.robotMu.Unlock()
	cfg := a.config
	if cfg.Robots.Lark.Enabled && cfg.Robots.Lark.AppID != "" && cfg.Robots.Lark.AppSecret != "" {
		ctx, cancel := context.WithCancel(context.Background())
		a.larkCancel = cancel
		go robot.StartLark(ctx, cfg.Robots.Lark, a.robotHandler, a.logger.Logger)
	}
	if cfg.Robots.Dingtalk.Enabled && cfg.Robots.Dingtalk.ClientID != "" && cfg.Robots.Dingtalk.ClientSecret != "" {
		ctx, cancel := context.WithCancel(context.Background())
		a.dingCancel = cancel
		go robot.StartDing(ctx, cfg.Robots.Dingtalk, a.robotHandler, a.logger.Logger)
	}
}

// RestartRobotConnections restarts DingTalk/Feishu long connections so that the front-end application configuration takes effect immediately (implementing handler.RobotRestarter)
func (a *App) RestartRobotConnections() {
	a.robotMu.Lock()
	if a.dingCancel != nil {
		a.dingCancel()
		a.dingCancel = nil
	}
	if a.larkCancel != nil {
		a.larkCancel()
		a.larkCancel = nil
	}
	a.robotMu.Unlock()
	// Give the old goroutine some time to exit
	time.Sleep(200 * time.Millisecond)
	a.startRobotConnections()
}

// SetupRoutes setup routes
func setupRoutes(
	router *gin.Engine,
	authHandler *handler.AuthHandler,
	agentHandler *handler.AgentHandler,
	monitorHandler *handler.MonitorHandler,
	conversationHandler *handler.ConversationHandler,
	robotHandler *handler.RobotHandler,
	groupHandler *handler.GroupHandler,
	configHandler *handler.ConfigHandler,
	externalMCPHandler *handler.ExternalMCPHandler,
	attackChainHandler *handler.AttackChainHandler,
	app *App, // Pass the App instance to dynamically obtain the knowledgeHandler
	vulnerabilityHandler *handler.VulnerabilityHandler,
	roleHandler *handler.RoleHandler,
	skillsHandler *handler.SkillsHandler,
	fofaHandler *handler.FofaHandler,
	terminalHandler *handler.TerminalHandler,
	mcpServer *mcp.Server,
	authManager *security.AuthManager,
	openAPIHandler *handler.OpenAPIHandler,
) {
	// API routing
	api := router.Group("/api")

	// Authentication related routes
	authRoutes := api.Group("/auth")
	{
		authRoutes.POST("/login", authHandler.Login)
		authRoutes.POST("/logout", security.AuthMiddleware(authManager), authHandler.Logout)
		authRoutes.POST("/change-password", security.AuthMiddleware(authManager), authHandler.ChangePassword)
		authRoutes.GET("/validate", security.AuthMiddleware(authManager), authHandler.Validate)
	}

	// Robot callback (no login required, can be called by the enterprise WeChat/DingTalk/Feishu server)
	api.GET("/robot/wecom", robotHandler.HandleWecomGET)
	api.POST("/robot/wecom", robotHandler.HandleWecomPOST)
	api.POST("/robot/dingtalk", robotHandler.HandleDingtalkPOST)
	api.POST("/robot/lark", robotHandler.HandleLarkPOST)

	protected := api.Group("")
	protected.Use(security.AuthMiddleware(authManager))
	{
		// Robot test (login required): POST /api/robot/test, body: {"platform":"dingtalk","user_id":"test","text":"help"}, used to verify the robot logic
		protected.POST("/robot/test", robotHandler.HandleRobotTest)

		// Agent Loop
		protected.POST("/agent-loop", agentHandler.AgentLoop)
		// Agent Loop streaming output
		protected.POST("/agent-loop/stream", agentHandler.AgentLoopStream)
		// Agent Loop Cancellation and Task List
		protected.POST("/agent-loop/cancel", agentHandler.CancelAgentLoop)
		protected.GET("/agent-loop/tasks", agentHandler.ListAgentTasks)
		protected.GET("/agent-loop/tasks/completed", agentHandler.ListCompletedTasks)

		// Information Collection - FOFA Query (Backend Proxy)
		protected.POST("/fofa/search", fofaHandler.Search)
		// Information collection - parsing natural language into FOFA grammar (manual confirmation is required before querying)
		protected.POST("/fofa/parse", fofaHandler.ParseNaturalLanguage)

		// Batch task management
		protected.POST("/batch-tasks", agentHandler.CreateBatchQueue)
		protected.GET("/batch-tasks", agentHandler.ListBatchQueues)
		protected.GET("/batch-tasks/:queueId", agentHandler.GetBatchQueue)
		protected.POST("/batch-tasks/:queueId/start", agentHandler.StartBatchQueue)
		protected.POST("/batch-tasks/:queueId/pause", agentHandler.PauseBatchQueue)
		protected.DELETE("/batch-tasks/:queueId", agentHandler.DeleteBatchQueue)
		protected.PUT("/batch-tasks/:queueId/tasks/:taskId", agentHandler.UpdateBatchTask)
		protected.POST("/batch-tasks/:queueId/tasks", agentHandler.AddBatchTask)
		protected.DELETE("/batch-tasks/:queueId/tasks/:taskId", agentHandler.DeleteBatchTask)

		// Conversation history
		protected.POST("/conversations", conversationHandler.CreateConversation)
		protected.GET("/conversations", conversationHandler.ListConversations)
		protected.GET("/conversations/:id", conversationHandler.GetConversation)
		protected.PUT("/conversations/:id", conversationHandler.UpdateConversation)
		protected.DELETE("/conversations/:id", conversationHandler.DeleteConversation)
		protected.PUT("/conversations/:id/pinned", groupHandler.UpdateConversationPinned)

		// Conversation grouping
		protected.POST("/groups", groupHandler.CreateGroup)
		protected.GET("/groups", groupHandler.ListGroups)
		protected.GET("/groups/:id", groupHandler.GetGroup)
		protected.PUT("/groups/:id", groupHandler.UpdateGroup)
		protected.DELETE("/groups/:id", groupHandler.DeleteGroup)
		protected.PUT("/groups/:id/pinned", groupHandler.UpdateGroupPinned)
		protected.GET("/groups/:id/conversations", groupHandler.GetGroupConversations)
		protected.POST("/groups/conversations", groupHandler.AddConversationToGroup)
		protected.DELETE("/groups/:id/conversations/:conversationId", groupHandler.RemoveConversationFromGroup)
		protected.PUT("/groups/:id/conversations/:conversationId/pinned", groupHandler.UpdateConversationPinnedInGroup)

		// Monitor
		protected.GET("/monitor", monitorHandler.Monitor)
		protected.GET("/monitor/execution/:id", monitorHandler.GetExecution)
		protected.DELETE("/monitor/execution/:id", monitorHandler.DeleteExecution)
		protected.DELETE("/monitor/executions", monitorHandler.DeleteExecutions)
		protected.GET("/monitor/stats", monitorHandler.GetStats)

		// Configuration management
		protected.GET("/config", configHandler.GetConfig)
		protected.GET("/config/tools", configHandler.GetTools)
		protected.PUT("/config", configHandler.UpdateConfig)
		protected.POST("/config/apply", configHandler.ApplyConfig)

		// System Settings - Terminal (execute commands to improve operation and maintenance efficiency)
		protected.POST("/terminal/run", terminalHandler.RunCommand)
		protected.POST("/terminal/run/stream", terminalHandler.RunCommandStream)
		protected.GET("/terminal/ws", terminalHandler.RunCommandWS)

		// External MCP management
		protected.GET("/external-mcp", externalMCPHandler.GetExternalMCPs)
		protected.GET("/external-mcp/stats", externalMCPHandler.GetExternalMCPStats)
		protected.GET("/external-mcp/:name", externalMCPHandler.GetExternalMCP)
		protected.PUT("/external-mcp/:name", externalMCPHandler.AddOrUpdateExternalMCP)
		protected.DELETE("/external-mcp/:name", externalMCPHandler.DeleteExternalMCP)
		protected.POST("/external-mcp/:name/start", externalMCPHandler.StartExternalMCP)
		protected.POST("/external-mcp/:name/stop", externalMCPHandler.StopExternalMCP)

		// Attack chain visualization
		protected.GET("/attack-chain/:conversationId", attackChainHandler.GetAttackChain)
		protected.POST("/attack-chain/:conversationId/regenerate", attackChainHandler.RegenerateAttackChain)

		// Knowledge base management (always register routes, dynamically obtain handlers through App instances)
		knowledgeRoutes := protected.Group("/knowledge")
		{
			knowledgeRoutes.GET("/categories", func(c *gin.Context) {
				if app.knowledgeHandler == nil {
					c.JSON(http.StatusOK, gin.H{
						"categories": []string{},
						"enabled":    false,
						"message":    "The knowledge base function is not enabled. Please go to the system settings to enable the knowledge retrieval function.",
					})
					return
				}
				app.knowledgeHandler.GetCategories(c)
			})
			knowledgeRoutes.GET("/items", func(c *gin.Context) {
				if app.knowledgeHandler == nil {
					c.JSON(http.StatusOK, gin.H{
						"items":   []interface{}{},
						"enabled": false,
						"message": "The knowledge base function is not enabled. Please go to the system settings to enable the knowledge retrieval function.",
					})
					return
				}
				app.knowledgeHandler.GetItems(c)
			})
			knowledgeRoutes.GET("/items/:id", func(c *gin.Context) {
				if app.knowledgeHandler == nil {
					c.JSON(http.StatusOK, gin.H{
						"enabled": false,
						"message": "The knowledge base function is not enabled. Please go to the system settings to enable the knowledge retrieval function.",
					})
					return
				}
				app.knowledgeHandler.GetItem(c)
			})
			knowledgeRoutes.POST("/items", func(c *gin.Context) {
				if app.knowledgeHandler == nil {
					c.JSON(http.StatusOK, gin.H{
						"enabled": false,
						"error":   "The knowledge base function is not enabled. Please go to the system settings to enable the knowledge retrieval function.",
					})
					return
				}
				app.knowledgeHandler.CreateItem(c)
			})
			knowledgeRoutes.PUT("/items/:id", func(c *gin.Context) {
				if app.knowledgeHandler == nil {
					c.JSON(http.StatusOK, gin.H{
						"enabled": false,
						"error":   "The knowledge base function is not enabled. Please go to the system settings to enable the knowledge retrieval function.",
					})
					return
				}
				app.knowledgeHandler.UpdateItem(c)
			})
			knowledgeRoutes.DELETE("/items/:id", func(c *gin.Context) {
				if app.knowledgeHandler == nil {
					c.JSON(http.StatusOK, gin.H{
						"enabled": false,
						"error":   "The knowledge base function is not enabled. Please go to the system settings to enable the knowledge retrieval function.",
					})
					return
				}
				app.knowledgeHandler.DeleteItem(c)
			})
			knowledgeRoutes.GET("/index-status", func(c *gin.Context) {
				if app.knowledgeHandler == nil {
					c.JSON(http.StatusOK, gin.H{
						"enabled":          false,
						"total_items":      0,
						"indexed_items":    0,
						"progress_percent": 0,
						"is_complete":      false,
						"message":          "The knowledge base function is not enabled. Please go to the system settings to enable the knowledge retrieval function.",
					})
					return
				}
				app.knowledgeHandler.GetIndexStatus(c)
			})
			knowledgeRoutes.POST("/index", func(c *gin.Context) {
				if app.knowledgeHandler == nil {
					c.JSON(http.StatusOK, gin.H{
						"enabled": false,
						"error":   "The knowledge base function is not enabled. Please go to the system settings to enable the knowledge retrieval function.",
					})
					return
				}
				app.knowledgeHandler.RebuildIndex(c)
			})
			knowledgeRoutes.POST("/scan", func(c *gin.Context) {
				if app.knowledgeHandler == nil {
					c.JSON(http.StatusOK, gin.H{
						"enabled": false,
						"error":   "The knowledge base function is not enabled. Please go to the system settings to enable the knowledge retrieval function.",
					})
					return
				}
				app.knowledgeHandler.ScanKnowledgeBase(c)
			})
			knowledgeRoutes.GET("/retrieval-logs", func(c *gin.Context) {
				if app.knowledgeHandler == nil {
					c.JSON(http.StatusOK, gin.H{
						"logs":    []interface{}{},
						"enabled": false,
						"message": "The knowledge base function is not enabled. Please go to the system settings to enable the knowledge retrieval function.",
					})
					return
				}
				app.knowledgeHandler.GetRetrievalLogs(c)
			})
			knowledgeRoutes.DELETE("/retrieval-logs/:id", func(c *gin.Context) {
				if app.knowledgeHandler == nil {
					c.JSON(http.StatusOK, gin.H{
						"enabled": false,
						"error":   "The knowledge base function is not enabled. Please go to the system settings to enable the knowledge retrieval function.",
					})
					return
				}
				app.knowledgeHandler.DeleteRetrievalLog(c)
			})
			knowledgeRoutes.POST("/search", func(c *gin.Context) {
				if app.knowledgeHandler == nil {
					c.JSON(http.StatusOK, gin.H{
						"results": []interface{}{},
						"enabled": false,
						"message": "The knowledge base function is not enabled. Please go to the system settings to enable the knowledge retrieval function.",
					})
					return
				}
				app.knowledgeHandler.Search(c)
			})
			knowledgeRoutes.GET("/stats", func(c *gin.Context) {
				if app.knowledgeHandler == nil {
					c.JSON(http.StatusOK, gin.H{
						"enabled":          false,
						"total_categories": 0,
						"total_items":      0,
						"message":          "The knowledge base function is not enabled. Please go to the system settings to enable the knowledge retrieval function.",
					})
					return
				}
				app.knowledgeHandler.GetStats(c)
			})
		}

		// Vulnerability management
		protected.GET("/vulnerabilities", vulnerabilityHandler.ListVulnerabilities)
		protected.GET("/vulnerabilities/stats", vulnerabilityHandler.GetVulnerabilityStats)
		protected.GET("/vulnerabilities/:id", vulnerabilityHandler.GetVulnerability)
		protected.POST("/vulnerabilities", vulnerabilityHandler.CreateVulnerability)
		protected.PUT("/vulnerabilities/:id", vulnerabilityHandler.UpdateVulnerability)
		protected.DELETE("/vulnerabilities/:id", vulnerabilityHandler.DeleteVulnerability)

		// Role management
		protected.GET("/roles", roleHandler.GetRoles)
		protected.GET("/roles/:name", roleHandler.GetRole)
		protected.GET("/roles/skills/list", roleHandler.GetSkills)
		protected.POST("/roles", roleHandler.CreateRole)
		protected.PUT("/roles/:name", roleHandler.UpdateRole)
		protected.DELETE("/roles/:name", roleHandler.DeleteRole)

		// Skills management
		protected.GET("/skills", skillsHandler.GetSkills)
		protected.GET("/skills/stats", skillsHandler.GetSkillStats)
		protected.DELETE("/skills/stats", skillsHandler.ClearSkillStats)
		protected.GET("/skills/:name", skillsHandler.GetSkill)
		protected.GET("/skills/:name/bound-roles", skillsHandler.GetSkillBoundRoles)
		protected.POST("/skills", skillsHandler.CreateSkill)
		protected.PUT("/skills/:name", skillsHandler.UpdateSkill)
		protected.DELETE("/skills/:name", skillsHandler.DeleteSkill)
		protected.DELETE("/skills/:name/stats", skillsHandler.ClearSkillStatsByName)

		// MCP endpoint
		protected.POST("/mcp", func(c *gin.Context) {
			mcpServer.HandleHTTP(c.Writer, c.Request)
		})

		// OpenAPI results aggregation endpoint (optional, used to get the complete results of the conversation)
		protected.GET("/conversations/:id/results", openAPIHandler.GetConversationResults)
	}

	// OpenAPI specification (requires authentication to avoid exposing API structure information)
	protected.GET("/openapi/spec", openAPIHandler.GetOpenAPISpec)

	// API documentation page (publicly accessible, but you need to log in to use the API)
	router.GET("/api-docs", func(c *gin.Context) {
		c.HTML(http.StatusOK, "api-docs.html", nil)
	})

	// Static files
	router.Static("/static", "./web/static")
	router.LoadHTMLGlob("web/templates/*")

	// Front-end page
	router.GET("/", func(c *gin.Context) {
		version := app.config.Version
		if version == "" {
			version = "v1.0.0"
		}
		c.HTML(http.StatusOK, "index.html", gin.H{"Version": version})
	})
}

// RegisterVulnerabilityTool registers the vulnerability recording tool to the MCP server
func registerVulnerabilityTool(mcpServer *mcp.Server, db *database.DB, logger *zap.Logger) {
	tool := mcp.Tool{
		Name:             builtin.ToolRecordVulnerability,
		Description:      "Record the details of discovered vulnerabilities to the vulnerability management system. When a valid vulnerability is discovered, use this tool to log vulnerability information, including title, description, severity, type, target, justification, impact, and recommendations.",
		ShortDescription: "Record discovered vulnerability details into vulnerability management system",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"title": map[string]interface{}{
					"type":        "string",
					"description": "Vulnerability title (required)",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Detailed description of the vulnerability",
				},
				"severity": map[string]interface{}{
					"type":        "string",
					"description": "Vulnerability severity: critical, high, medium, low, info",
					"enum":        []string{"critical", "high", "medium", "low", "info"},
				},
				"vulnerability_type": map[string]interface{}{
					"type":        "string",
					"description": "Vulnerability types, such as: SQL injection, XSS, CSRF, command injection, etc.",
				},
				"target": map[string]interface{}{
					"type":        "string",
					"description": "Affected targets (URLs, IP addresses, services, etc.)",
				},
				"proof": map[string]interface{}{
					"type":        "string",
					"description": "Proof of vulnerability (POC, screenshots, request/response, etc.)",
				},
				"impact": map[string]interface{}{
					"type":        "string",
					"description": "Vulnerability impact statement",
				},
				"recommendation": map[string]interface{}{
					"type":        "string",
					"description": "Repair suggestions",
				},
			},
			"required": []string{"title", "severity"},
		},
	}

	handler := func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		// Get conversation_id from parameters (automatically added by Agent)
		conversationID, _ := args["conversation_id"].(string)
		if conversationID == "" {
			return &mcp.ToolResult{
				Content: []mcp.Content{
					{
						Type: "text",
						Text: "Error: conversation_id is not set. This is a system error, please try again.",
					},
				},
				IsError: true,
			}, nil
		}

		title, ok := args["title"].(string)
		if !ok || title == "" {
			return &mcp.ToolResult{
				Content: []mcp.Content{
					{
						Type: "text",
						Text: "Error: title parameter is required and cannot be empty",
					},
				},
				IsError: true,
			}, nil
		}

		severity, ok := args["severity"].(string)
		if !ok || severity == "" {
			return &mcp.ToolResult{
				Content: []mcp.Content{
					{
						Type: "text",
						Text: "Error: severity parameter is required and cannot be empty",
					},
				},
				IsError: true,
			}, nil
		}

		// Verify severity
		validSeverities := map[string]bool{
			"critical": true,
			"high":     true,
			"medium":   true,
			"low":      true,
			"info":     true,
		}
		if !validSeverities[severity] {
			return &mcp.ToolResult{
				Content: []mcp.Content{
					{
						Type: "text",
						Text: fmt.Sprintf("Error: severity must be one of critical, high, medium, low or info, current value: %s", severity),
					},
				},
				IsError: true,
			}, nil
		}

		// Get optional parameters
		description := ""
		if d, ok := args["description"].(string); ok {
			description = d
		}

		vulnType := ""
		if t, ok := args["vulnerability_type"].(string); ok {
			vulnType = t
		}

		target := ""
		if t, ok := args["target"].(string); ok {
			target = t
		}

		proof := ""
		if p, ok := args["proof"].(string); ok {
			proof = p
		}

		impact := ""
		if i, ok := args["impact"].(string); ok {
			impact = i
		}

		recommendation := ""
		if r, ok := args["recommendation"].(string); ok {
			recommendation = r
		}

		// Create a vulnerability record
		vuln := &database.Vulnerability{
			ConversationID: conversationID,
			Title:          title,
			Description:    description,
			Severity:       severity,
			Status:         "open",
			Type:           vulnType,
			Target:         target,
			Proof:          proof,
			Impact:         impact,
			Recommendation: recommendation,
		}

		created, err := db.CreateVulnerability(vuln)
		if err != nil {
			logger.Error("Failure to log vulnerability", zap.Error(err))
			return &mcp.ToolResult{
				Content: []mcp.Content{
					{
						Type: "text",
						Text: fmt.Sprintf("Failed to log vulnerability: %v", err),
					},
				},
				IsError: true,
			}, nil
		}

		logger.Info("Vulnerability recorded successfully",
			zap.String("id", created.ID),
			zap.String("title", created.Title),
			zap.String("severity", created.Severity),
			zap.String("conversation_id", conversationID),
		)

		return &mcp.ToolResult{
			Content: []mcp.Content{
				{
					Type: "text",
					Text: fmt.Sprintf("The vulnerability has been successfully logged! \n\nVulnerability ID: %s\nTitle: %s\nSeverity: %s\nStatus: %s\n\nYou can view and manage this vulnerability on the vulnerability management page.", created.ID, created.Title, created.Severity, created.Status),
				},
			},
			IsError: false,
		}, nil
	}

	mcpServer.RegisterTool(tool, handler)
	logger.Info("Vulnerability recording tool registration successful")
}

// InitializeKnowledge initializes the knowledge base component (for dynamic initialization)
func initializeKnowledge(
	cfg *config.Config,
	db *database.DB,
	knowledgeDBConn *database.DB,
	mcpServer *mcp.Server,
	agentHandler *handler.AgentHandler,
	app *App, // Pass the App reference to update the knowledge base component
	logger *zap.Logger,
) (*handler.KnowledgeHandler, error) {
	// Determine the knowledge base database path
	knowledgeDBPath := cfg.Database.KnowledgeDBPath
	var knowledgeDB *sql.DB

	if knowledgeDBPath != "" {
		// Use a separate knowledge base database
		// Make sure the directory exists
		if err := os.MkdirAll(filepath.Dir(knowledgeDBPath), 0755); err != nil {
			return nil, fmt.Errorf("Failed to create knowledge base database directory: %w", err)
		}

		var err error
		knowledgeDBConn, err = database.NewKnowledgeDB(knowledgeDBPath, logger)
		if err != nil {
			return nil, fmt.Errorf("Failed to initialize knowledge base database: %w", err)
		}
		knowledgeDB = knowledgeDBConn.DB
		logger.Info("Use a separate knowledge base database", zap.String("path", knowledgeDBPath))
	} else {
		// Backward compatibility: using session database
		knowledgeDB = db.DB
		logger.Info("Use session database to store knowledge base data (it is recommended to configure knowledge_db_path to separate data)")
	}

	// Create a knowledge base manager
	knowledgeManager := knowledge.NewManager(knowledgeDB, cfg.Knowledge.BasePath, logger)

	// Create embedder
	// Use the API Key configured by OpenAI (if not specified in the knowledge base configuration)
	if cfg.Knowledge.Embedding.APIKey == "" {
		cfg.Knowledge.Embedding.APIKey = cfg.OpenAI.APIKey
	}
	if cfg.Knowledge.Embedding.BaseURL == "" {
		cfg.Knowledge.Embedding.BaseURL = cfg.OpenAI.BaseURL
	}

	httpClient := &http.Client{
		Timeout: 30 * time.Minute,
	}
	openAIClient := openai.NewClient(&cfg.OpenAI, httpClient, logger)
	embedder := knowledge.NewEmbedder(&cfg.Knowledge, &cfg.OpenAI, openAIClient, logger)

	// Create a retriever
	retrievalConfig := &knowledge.RetrievalConfig{
		TopK:                cfg.Knowledge.Retrieval.TopK,
		SimilarityThreshold: cfg.Knowledge.Retrieval.SimilarityThreshold,
		HybridWeight:        cfg.Knowledge.Retrieval.HybridWeight,
	}
	knowledgeRetriever := knowledge.NewRetriever(knowledgeDB, embedder, retrievalConfig, logger)

	// Create indexer
	knowledgeIndexer := knowledge.NewIndexer(knowledgeDB, embedder, logger)

	// Register the knowledge retrieval tool to the MCP server
	knowledge.RegisterKnowledgeTool(mcpServer, knowledgeRetriever, knowledgeManager, logger)

	// Create a knowledge base API handler
	knowledgeHandler := handler.NewKnowledgeHandler(knowledgeManager, knowledgeRetriever, knowledgeIndexer, db, logger)
	logger.Info("Knowledge base module initialization completed", zap.Bool("handler_created", knowledgeHandler != nil))

	// Set the knowledge base manager to AgentHandler to record retrieval logs
	agentHandler.SetKnowledgeManager(knowledgeManager)

	// Update the knowledge base component in App (if App is not nil, it means dynamic initialization)
	if app != nil {
		app.knowledgeManager = knowledgeManager
		app.knowledgeRetriever = knowledgeRetriever
		app.knowledgeIndexer = knowledgeIndexer
		app.knowledgeHandler = knowledgeHandler
		// If using a standalone database, update knowledgeDB
		if knowledgeDBPath != "" {
			app.knowledgeDB = knowledgeDBConn
		}
		logger.Info("The knowledge base component in the app has been updated")
	}

	// Scan and index the knowledge base (asynchronously)
	go func() {
		itemsToIndex, err := knowledgeManager.ScanKnowledgeBase()
		if err != nil {
			logger.Warn("Scanning knowledge base failed", zap.Error(err))
			return
		}

		// Check if there is an index
		hasIndex, err := knowledgeIndexer.HasIndex()
		if err != nil {
			logger.Warn("Checking index status failed", zap.Error(err))
			return
		}

		if hasIndex {
			// If an index already exists, only newly added or updated items are indexed
			if len(itemsToIndex) > 0 {
				logger.Info("An existing knowledge base index is detected and incremental indexing starts.", zap.Int("count", len(itemsToIndex)))
				ctx := context.Background()
				consecutiveFailures := 0
				var firstFailureItemID string
				var firstFailureError error
				failedCount := 0

				for _, itemID := range itemsToIndex {
					if err := knowledgeIndexer.IndexItem(ctx, itemID); err != nil {
						failedCount++
						consecutiveFailures++

						if consecutiveFailures == 1 {
							firstFailureItemID = itemID
							firstFailureError = err
							logger.Warn("Indexing knowledge items failed", zap.String("itemId", itemID), zap.Error(err))
						}

						// If it fails 2 times in a row, stop incremental indexing immediately
						if consecutiveFailures >= 2 {
							logger.Error("There are too many consecutive indexing failures. Stop incremental indexing immediately.",
								zap.Int("consecutiveFailures", consecutiveFailures),
								zap.Int("totalItems", len(itemsToIndex)),
								zap.String("firstFailureItemId", firstFailureItemID),
								zap.Error(firstFailureError),
							)
							break
						}
						continue
					}

					// Reset consecutive failure count on success
					if consecutiveFailures > 0 {
						consecutiveFailures = 0
						firstFailureItemID = ""
						firstFailureError = nil
					}
				}
				logger.Info("Incremental indexing completed", zap.Int("totalItems", len(itemsToIndex)), zap.Int("failedCount", failedCount))
			} else {
				logger.Info("Existing knowledge base index detected, no new or updated items need to be indexed")
			}
			return
		}

		// Automatically rebuild only if there are no indexes
		logger.Info("No knowledge base index detected, automatic index building started")
		ctx := context.Background()
		if err := knowledgeIndexer.RebuildIndex(ctx); err != nil {
			logger.Warn("Rebuilding the knowledge base index failed", zap.Error(err))
		}
	}()

	return knowledgeHandler, nil
}

// CorsMiddleware CORS middleware
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}
