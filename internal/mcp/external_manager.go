package mcp

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/config"

	"github.com/google/uuid"

	"go.uber.org/zap"
)

// ExternalMCPManager ExternalMCPManager
type ExternalMCPManager struct {
	clients      map[string]ExternalMCPClient
	configs      map[string]config.ExternalMCPServerConfig
	logger       *zap.Logger
	storage      MonitorStorage            // Optional persistent storage
	executions   map[string]*ToolExecution // Execution record
	stats        map[string]*ToolStats     // Tool statistics
	errors       map[string]string         // Error message
	toolCounts   map[string]int            // Tool quantity cache
	toolCountsMu sync.RWMutex              // Tool quantity cache lock
	toolCache    map[string][]Tool         // Tool list cache: MCP name -> Tool list
	toolCacheMu  sync.RWMutex              // Tool list cache lock
	stopRefresh  chan struct{}             // Signal to stop background refresh
	refreshWg    sync.WaitGroup            // Wait for the background refresh goroutine to complete
	mu           sync.RWMutex
}

// NewExternalMCPManager creates an external MCP manager
func NewExternalMCPManager(logger *zap.Logger) *ExternalMCPManager {
	return NewExternalMCPManagerWithStorage(logger, nil)
}

// NewExternalMCPManagerWithStorage creates an external MCP manager (with persistent storage)
func NewExternalMCPManagerWithStorage(logger *zap.Logger, storage MonitorStorage) *ExternalMCPManager {
	manager := &ExternalMCPManager{
		clients:     make(map[string]ExternalMCPClient),
		configs:     make(map[string]config.ExternalMCPServerConfig),
		logger:      logger,
		storage:     storage,
		executions:  make(map[string]*ToolExecution),
		stats:       make(map[string]*ToolStats),
		errors:      make(map[string]string),
		toolCounts:  make(map[string]int),
		toolCache:   make(map[string][]Tool),
		stopRefresh: make(chan struct{}),
	}
	// Start the number of goroutines for background refresh tools
	manager.startToolCountRefresh()
	return manager
}

// LoadConfigs loads configurations
func (m *ExternalMCPManager) LoadConfigs(cfg *config.ExternalMCPConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if cfg == nil || cfg.Servers == nil {
		return
	}

	m.configs = make(map[string]config.ExternalMCPServerConfig)
	for name, serverCfg := range cfg.Servers {
		m.configs[name] = serverCfg
	}
}

// GetConfigs Get all configurations
func (m *ExternalMCPManager) GetConfigs() map[string]config.ExternalMCPServerConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]config.ExternalMCPServerConfig)
	for k, v := range m.configs {
		result[k] = v
	}
	return result
}

// AddOrUpdateConfig adds or updates configuration
func (m *ExternalMCPManager) AddOrUpdateConfig(name string, serverCfg config.ExternalMCPServerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// If the client already exists, close it first
	if client, exists := m.clients[name]; exists {
		client.Close()
		delete(m.clients, name)
	}

	m.configs[name] = serverCfg

	// If enabled, connect automatically
	if m.isEnabled(serverCfg) {
		go m.connectClient(name, serverCfg)
	}

	return nil
}

// RemoveConfig Remove configuration
func (m *ExternalMCPManager) RemoveConfig(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Close client
	if client, exists := m.clients[name]; exists {
		client.Close()
		delete(m.clients, name)
	}

	delete(m.configs, name)

	// Clean tool quantity cache
	m.toolCountsMu.Lock()
	delete(m.toolCounts, name)
	m.toolCountsMu.Unlock()

	// Clean tool list cache
	m.toolCacheMu.Lock()
	delete(m.toolCache, name)
	m.toolCacheMu.Unlock()

	return nil
}

// StartClient starts the client
func (m *ExternalMCPManager) StartClient(name string) error {
	m.mu.Lock()
	serverCfg, exists := m.configs[name]
	m.mu.Unlock()

	if !exists {
		return fmt.Errorf("Configuration does not exist: %s", name)
	}

	// Check if there is already a connected client
	m.mu.RLock()
	existingClient, hasClient := m.clients[name]
	m.mu.RUnlock()

	if hasClient {
		// Check if the client is connected
		if existingClient.IsConnected() {
			// The client is connected and returns success directly (the target status has been achieved)
			// Update the configuration to enable (make sure the configuration is consistent)
			m.mu.Lock()
			serverCfg.ExternalMCPEnable = true
			m.configs[name] = serverCfg
			m.mu.Unlock()
			return nil
		}
		// If there is a client but not connected, close it first
		existingClient.Close()
		m.mu.Lock()
		delete(m.clients, name)
		m.mu.Unlock()
	}

	// Update configuration is enabled
	m.mu.Lock()
	serverCfg.ExternalMCPEnable = true
	m.configs[name] = serverCfg
	// Clear previous error messages (on reboot)
	delete(m.errors, name)
	m.mu.Unlock()

	// Create the client immediately and set it to "connecting" status so the front end can see the status immediately
	client := m.createClient(serverCfg)
	if client == nil {
		return fmt.Errorf("Unable to create client: Unsupported transport mode")
	}

	// Set status to connecting
	m.setClientStatus(client, "connecting")

	// Save the client immediately so that the "connecting" status can be seen when the front-end queries
	m.mu.Lock()
	m.clients[name] = client
	m.mu.Unlock()

	// Do the actual connection asynchronously in the background
	go func() {
		if err := m.doConnect(name, serverCfg, client); err != nil {
			m.logger.Error("Failed to connect to external MCP client",
				zap.String("name", name),
				zap.Error(err),
			)
			// The connection fails, set the status to error and save the error information
			m.setClientStatus(client, "error")
			m.mu.Lock()
			m.errors[name] = err.Error()
			m.mu.Unlock()
			// Trigger tool number refresh (connection failed, tool number should be 0)
			m.triggerToolCountRefresh()
		} else {
			// Connection successful, clear error message
			m.mu.Lock()
			delete(m.errors, name)
			m.mu.Unlock()
			// Immediately refresh the tool count and tool list cache
			m.triggerToolCountRefresh()
			m.refreshToolCache(name, client)
			// Refresh again after 2 seconds, covering SSE/Streamable and other remote ends that need to wait for preparation.
			go func() {
				time.Sleep(2 * time.Second)
				m.triggerToolCountRefresh()
				m.refreshToolCache(name, client)
			}()
		}
	}()

	return nil
}

// StopClient Stop the client
func (m *ExternalMCPManager) StopClient(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	serverCfg, exists := m.configs[name]
	if !exists {
		return fmt.Errorf("Configuration does not exist: %s", name)
	}

	// Close client
	if client, exists := m.clients[name]; exists {
		client.Close()
		delete(m.clients, name)
	}

	// Clear error message
	delete(m.errors, name)

	// Update the tool quantity cache (the tool quantity is 0 after stopping)
	m.toolCountsMu.Lock()
	m.toolCounts[name] = 0
	m.toolCountsMu.Unlock()

	// Update configuration is disabled
	serverCfg.ExternalMCPEnable = false
	m.configs[name] = serverCfg

	return nil
}

// GetClient gets the client
func (m *ExternalMCPManager) GetClient(name string) (ExternalMCPClient, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	client, exists := m.clients[name]
	return client, exists
}

// GetError Get error information
func (m *ExternalMCPManager) GetError(name string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.errors[name]
}

// GetAllTools Gets all external MCP tools
// Get it from the connected client first, and return the cached tool list if the connection is disconnected
// Strategy:
// - error status: Do not use cache, skip directly (configuration error or service unavailable)
// - disconnected/connecting state: using cache (temporarily disconnected)
// - Connected status: normal acquisition, downgrade to cache if failed
func (m *ExternalMCPManager) GetAllTools(ctx context.Context) ([]Tool, error) {
	m.mu.RLock()
	clients := make(map[string]ExternalMCPClient)
	for k, v := range m.clients {
		clients[k] = v
	}
	m.mu.RUnlock()

	var allTools []Tool
	var hasError bool
	var lastError error

	// Use a short timeout for quick checks (3 seconds) to avoid blocking
	quickCtx, quickCancel := context.WithTimeout(ctx, 3*time.Second)
	defer quickCancel()

	for name, client := range clients {
		tools, err := m.getToolsForClient(name, client, quickCtx)
		if err != nil {
			// Log the error but continue processing other clients
			hasError = true
			if lastError == nil {
				lastError = err
			}
			continue
		}

		// Add prefixes to tools to avoid conflicts
		for _, tool := range tools {
			tool.Name = fmt.Sprintf("%s::%s", name, tool.Name)
			allTools = append(allTools, tool)
		}
	}

	// If there is an error but at least some tools are returned, no error is returned (partial success)
	if hasError && len(allTools) == 0 {
		return nil, fmt.Errorf("Failed to obtain external MCP tool: %w", lastError)
	}

	return allTools, nil
}

// GetToolsForClient Gets the tool list of the specified client
// Returns a list of tools and errors (if completely unobtainable)
func (m *ExternalMCPManager) getToolsForClient(name string, client ExternalMCPClient, ctx context.Context) ([]Tool, error) {
	status := client.GetStatus()

	// Error status: Do not use cache, return error directly
	if status == "error" {
		m.logger.Debug("Skip external MCP on connection failure (does not use caching)",
			zap.String("name", name),
			zap.String("status", status),
		)
		return nil, fmt.Errorf("External MCP connection failed: %s", name)
	}

	// Connected: Try to get the latest tool list
	if client.IsConnected() {
		tools, err := client.ListTools(ctx)
		if err != nil {
			// Failed to retrieve, trying to use cache
			return m.getCachedTools(name, "The connection is normal but the acquisition failed", err)
		}

		// Get successful, update cache
		m.updateToolCache(name, tools)
		return tools, nil
	}

	// Not connected: Determine whether to use cache based on status
	if status == "disconnected" || status == "connecting" {
		return m.getCachedTools(name, fmt.Sprintf("Client temporarily disconnected (status: %s)", status), nil)
	}

	// Other unknown states, no cache is used
	m.logger.Debug("Skip external MCP (unknown status)",
		zap.String("name", name),
		zap.String("status", status),
	)
	return nil, fmt.Errorf("External MCP status unknown: %s (status: %s)", name, status)
}

// GetCachedTools Gets the cached tool list
func (m *ExternalMCPManager) getCachedTools(name, reason string, originalErr error) ([]Tool, error) {
	m.toolCacheMu.RLock()
	cachedTools, hasCache := m.toolCache[name]
	m.toolCacheMu.RUnlock()

	if hasCache && len(cachedTools) > 0 {
		m.logger.Debug("Use cached tool list",
			zap.String("name", name),
			zap.String("reason", reason),
			zap.Int("count", len(cachedTools)),
			zap.Error(originalErr),
		)
		return cachedTools, nil
	}

	// No cache, return error
	if originalErr != nil {
		return nil, fmt.Errorf("Failed to obtain external MCP tool without cache: %w", originalErr)
	}
	return nil, fmt.Errorf("External MCP no cache tool: %s", name)
}

// UpdateToolCache update tool list cache
func (m *ExternalMCPManager) updateToolCache(name string, tools []Tool) {
	m.toolCacheMu.Lock()
	m.toolCache[name] = tools
	m.toolCacheMu.Unlock()

	// Log a warning if an empty list is returned
	if len(tools) == 0 {
		m.logger.Warn("External MCP returns empty tool list",
			zap.String("name", name),
			zap.String("hint", "The service may be temporarily unavailable and the tool list is empty"),
		)
	} else {
		m.logger.Debug("Tool list cache updated",
			zap.String("name", name),
			zap.Int("count", len(tools)),
		)
	}
}

// CallTool calls an external MCP tool (returns execution ID)
func (m *ExternalMCPManager) CallTool(ctx context.Context, toolName string, args map[string]interface{}) (*ToolResult, string, error) {
	// Parsing tool name: name::toolName
	var mcpName, actualToolName string
	if idx := findSubstring(toolName, "::"); idx > 0 {
		mcpName = toolName[:idx]
		actualToolName = toolName[idx+2:]
	} else {
		return nil, "", fmt.Errorf("Invalid tool name format: %s", toolName)
	}

	client, exists := m.GetClient(mcpName)
	if !exists {
		return nil, "", fmt.Errorf("External MCP client does not exist: %s", mcpName)
	}

	// Check the connection status. If it is not connected or the status is error, the call is not allowed.
	if !client.IsConnected() {
		status := client.GetStatus()
		if status == "error" {
			// Get the error message (if any)
			errorMsg := m.GetError(mcpName)
			if errorMsg != "" {
				return nil, "", fmt.Errorf("External MCP connection failed: %s (Error: %s)", mcpName, errorMsg)
			}
			return nil, "", fmt.Errorf("External MCP connection failed: %s", mcpName)
		}
		return nil, "", fmt.Errorf("External MCP client not connected: %s (status: %s)", mcpName, status)
	}

	// Create execution record
	executionID := uuid.New().String()
	execution := &ToolExecution{
		ID:        executionID,
		ToolName:  toolName, // Use full tool name (including MCP name)
		Arguments: args,
		Status:    "running",
		StartTime: time.Now(),
	}

	m.mu.Lock()
	m.executions[executionID] = execution
	// If the execution records in memory exceed the limit, clean up the oldest records
	m.cleanupOldExecutions()
	m.mu.Unlock()

	if m.storage != nil {
		if err := m.storage.SaveToolExecution(execution); err != nil {
			m.logger.Warn("Failed to save execution records to database", zap.Error(err))
		}
	}

	// Call tool
	result, err := client.CallTool(ctx, actualToolName, args)

	// Update execution record
	m.mu.Lock()
	now := time.Now()
	execution.EndTime = &now
	execution.Duration = now.Sub(execution.StartTime)

	if err != nil {
		execution.Status = "failed"
		execution.Error = err.Error()
	} else if result != nil && result.IsError {
		execution.Status = "failed"
		if len(result.Content) > 0 {
			execution.Error = result.Content[0].Text
		} else {
			execution.Error = "Tool execution returns incorrect results"
		}
		execution.Result = result
	} else {
		execution.Status = "completed"
		if result == nil {
			result = &ToolResult{
				Content: []Content{
					{Type: "text", Text: "Tool execution completed but no results returned"},
				},
			}
		}
		execution.Result = result
	}
	m.mu.Unlock()

	if m.storage != nil {
		if err := m.storage.SaveToolExecution(execution); err != nil {
			m.logger.Warn("Failed to save execution records to database", zap.Error(err))
		}
	}

	// Update statistics
	failed := err != nil || (result != nil && result.IsError)
	m.updateStats(toolName, failed)

	// If using storage, remove from memory (persisted)
	if m.storage != nil {
		m.mu.Lock()
		delete(m.executions, executionID)
		m.mu.Unlock()
	}

	if err != nil {
		return nil, executionID, err
	}

	return result, executionID, nil
}

// CleanupOldExecutions cleans old execution records (keeps the number of records in memory within limits)
func (m *ExternalMCPManager) cleanupOldExecutions() {
	const maxExecutionsInMemory = 1000
	if len(m.executions) <= maxExecutionsInMemory {
		return
	}

	// Sort by start time, delete oldest record
	type execTime struct {
		id        string
		startTime time.Time
	}
	var execs []execTime
	for id, exec := range m.executions {
		execs = append(execs, execTime{id: id, startTime: exec.StartTime})
	}

	// Sort by time
	for i := 0; i < len(execs)-1; i++ {
		for j := i + 1; j < len(execs); j++ {
			if execs[i].startTime.After(execs[j].startTime) {
				execs[i], execs[j] = execs[j], execs[i]
			}
		}
	}

	// Delete the oldest record
	toDelete := len(m.executions) - maxExecutionsInMemory
	for i := 0; i < toDelete && i < len(execs); i++ {
		delete(m.executions, execs[i].id)
	}
}

// GetExecution gets execution records (first search from memory, then search from database)
func (m *ExternalMCPManager) GetExecution(id string) (*ToolExecution, bool) {
	m.mu.RLock()
	exec, exists := m.executions[id]
	m.mu.RUnlock()

	if exists {
		return exec, true
	}

	if m.storage != nil {
		exec, err := m.storage.GetToolExecution(id)
		if err == nil {
			return exec, true
		}
	}

	return nil, false
}

// UpdateStats updates statistics
func (m *ExternalMCPManager) updateStats(toolName string, failed bool) {
	now := time.Now()
	if m.storage != nil {
		totalCalls := 1
		successCalls := 0
		failedCalls := 0
		if failed {
			failedCalls = 1
		} else {
			successCalls = 1
		}
		if err := m.storage.UpdateToolStats(toolName, totalCalls, successCalls, failedCalls, &now); err != nil {
			m.logger.Warn("Failed to save statistics to database", zap.Error(err))
		}
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.stats[toolName] == nil {
		m.stats[toolName] = &ToolStats{
			ToolName: toolName,
		}
	}

	stats := m.stats[toolName]
	stats.TotalCalls++
	stats.LastCallTime = &now

	if failed {
		stats.FailedCalls++
	} else {
		stats.SuccessCalls++
	}
}

// GetStats Gets MCP server statistics
func (m *ExternalMCPManager) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := len(m.configs)
	enabled := 0
	disabled := 0
	connected := 0

	for name, cfg := range m.configs {
		if m.isEnabled(cfg) {
			enabled++
			if client, exists := m.clients[name]; exists && client.IsConnected() {
				connected++
			}
		} else {
			disabled++
		}
	}

	return map[string]interface{}{
		"total":     total,
		"enabled":   enabled,
		"disabled":  disabled,
		"connected": connected,
	}
}

// GetToolStats Get tool statistics (combined memory and database)
// Only returns statistics for external MCP tools (tool names containing "::")
func (m *ExternalMCPManager) GetToolStats() map[string]*ToolStats {
	result := make(map[string]*ToolStats)

	// Load statistics from database (if using database storage)
	if m.storage != nil {
		dbStats, err := m.storage.LoadToolStats()
		if err == nil {
			// Only keep statistics for external MCP tools (tool names containing "::")
			for k, v := range dbStats {
				if findSubstring(k, "::") > 0 {
					result[k] = v
				}
			}
		} else {
			m.logger.Warn("Loading statistics from database failed", zap.Error(err))
		}
	}

	// Merge in-memory statistics
	m.mu.RLock()
	for k, v := range m.stats {
		// If statistics for this tool already exist in the database, merge them
		if existing, exists := result[k]; exists {
			// Create new statistics objects and avoid modifying shared objects
			merged := &ToolStats{
				ToolName:     k,
				TotalCalls:   existing.TotalCalls + v.TotalCalls,
				SuccessCalls: existing.SuccessCalls + v.SuccessCalls,
				FailedCalls:  existing.FailedCalls + v.FailedCalls,
			}
			// Use the latest call time
			if v.LastCallTime != nil && (existing.LastCallTime == nil || v.LastCallTime.After(*existing.LastCallTime)) {
				merged.LastCallTime = v.LastCallTime
			} else if existing.LastCallTime != nil {
				timeCopy := *existing.LastCallTime
				merged.LastCallTime = &timeCopy
			}
			result[k] = merged
		} else {
			// If it is not in the database, use the statistics in memory directly.
			statCopy := *v
			result[k] = &statCopy
		}
	}
	m.mu.RUnlock()

	return result
}

// GetToolCount Gets the number of tools in the specified external MCP (read from cache, does not block)
func (m *ExternalMCPManager) GetToolCount(name string) (int, error) {
	// Read from cache first
	m.toolCountsMu.RLock()
	if count, exists := m.toolCounts[name]; exists {
		m.toolCountsMu.RUnlock()
		return count, nil
	}
	m.toolCountsMu.RUnlock()

	// If not in cache, check client status
	client, exists := m.GetClient(name)
	if !exists {
		return 0, fmt.Errorf("Client does not exist: %s", name)
	}

	if !client.IsConnected() {
		// Not connected, cache is 0
		m.toolCountsMu.Lock()
		m.toolCounts[name] = 0
		m.toolCountsMu.Unlock()
		return 0, nil
	}

	// If connected but not in the cache, trigger an asynchronous refresh and return 0 (to avoid blocking)
	m.triggerToolCountRefresh()
	return 0, nil
}

// GetToolCounts Gets the number of tools for all external MCPs (read from cache, does not block)
func (m *ExternalMCPManager) GetToolCounts() map[string]int {
	m.toolCountsMu.RLock()
	defer m.toolCountsMu.RUnlock()

	// Return a cached copy to avoid external modifications
	result := make(map[string]int)
	for k, v := range m.toolCounts {
		result[k] = v
	}
	return result
}

// RefreshToolCounts refresh tool number cache (background asynchronous execution)
func (m *ExternalMCPManager) refreshToolCounts() {
	m.mu.RLock()
	clients := make(map[string]ExternalMCPClient)
	for k, v := range m.clients {
		clients[k] = v
	}
	m.mu.RUnlock()

	newCounts := make(map[string]int)

	// Use goroutine to concurrently obtain the number of tools for each client to avoid serial blocking
	type countResult struct {
		name  string
		count int
	}
	resultChan := make(chan countResult, len(clients))

	for name, client := range clients {
		go func(n string, c ExternalMCPClient) {
			if !c.IsConnected() {
				resultChan <- countResult{name: n, count: 0}
				return
			}

			// Use a reasonable timeout (15 seconds) to cope with network delays without blocking for too long
			// Since this is a background asynchronous refresh, the timeout will not affect the front-end response
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			tools, err := c.ListTools(ctx)
			cancel()

			if err != nil {
				errStr := err.Error()
				// SSE connection EOF: The remote end may have closed the stream or did not push a response on the stream according to the specification. It only prompts with Warn for the first time.
				if strings.Contains(errStr, "EOF") || strings.Contains(errStr, "client is closing") {
					m.logger.Warn("Failed to obtain the number of external MCP tools (the SSE stream has been closed or the server did not return a tools/list response on the stream)",
						zap.String("name", n),
						zap.String("hint", "If it is an SSE connection, please confirm that the server keeps the GET stream open and pushes the JSON-RPC response with event: message according to the MCP specification."),
						zap.Error(err),
					)
				} else {
					m.logger.Warn("Failed to obtain the number of external MCP tools, please check the connection or server tools/list",
						zap.String("name", n),
						zap.Error(err),
					)
				}
				resultChan <- countResult{name: n, count: -1} // -1 means use the old value
				return
			}

			resultChan <- countResult{name: n, count: len(tools)}
		}(name, client)
	}

	// Collect results
	m.toolCountsMu.RLock()
	oldCounts := make(map[string]int)
	for k, v := range m.toolCounts {
		oldCounts[k] = v
	}
	m.toolCountsMu.RUnlock()

	for i := 0; i < len(clients); i++ {
		result := <-resultChan
		if result.count >= 0 {
			newCounts[result.name] = result.count
		} else {
			// Retrieval failed, old value retained
			if oldCount, exists := oldCounts[result.name]; exists {
				newCounts[result.name] = oldCount
			} else {
				newCounts[result.name] = 0
			}
		}
	}

	// Update cache
	m.toolCountsMu.Lock()
	// Update all obtained values
	for name, count := range newCounts {
		m.toolCounts[name] = count
	}
	// For unconnected clients, set to 0
	for name, client := range clients {
		if !client.IsConnected() {
			m.toolCounts[name] = 0
		}
	}
	m.toolCountsMu.Unlock()
}

// RefreshToolCache refreshes the tool list cache of the specified MCP
func (m *ExternalMCPManager) refreshToolCache(name string, client ExternalMCPClient) {
	if !client.IsConnected() {
		return
	}

	// Check the status. If it is an error status, the cache will not be updated.
	status := client.GetStatus()
	if status == "error" {
		m.logger.Debug("Skip refreshing tool list cache (connection failed)",
			zap.String("name", name),
			zap.String("status", status),
		)
		return
	}

	// Use a shorter timeout (5 seconds)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tools, err := client.ListTools(ctx)
	if err != nil {
		m.logger.Debug("Failed to refresh tool list cache",
			zap.String("name", name),
			zap.Error(err),
		)
		// Do not update cache when refresh fails, retain old cache (if any)
		return
	}

	// Use a unified cache update method
	m.updateToolCache(name, tools)
}

// StartToolCountRefresh starts the goroutine of the number of background refresh tools
func (m *ExternalMCPManager) startToolCountRefresh() {
	m.refreshWg.Add(1)
	go func() {
		defer m.refreshWg.Done()
		ticker := time.NewTicker(10 * time.Second) // Refresh every 10 seconds
		defer ticker.Stop()

		// Perform a refresh immediately
		m.refreshToolCounts()

		for {
			select {
			case <-ticker.C:
				m.refreshToolCounts()
			case <-m.stopRefresh:
				return
			}
		}
	}()
}

// TriggerToolCountRefresh triggers the number of tools to be refreshed immediately (asynchronous)
func (m *ExternalMCPManager) triggerToolCountRefresh() {
	go m.refreshToolCounts()
}

// CreateClient creates a client (does not connect). The lazy client of the official MCP Go SDK is uniformly used, and the connection is completed during Initialize.
func (m *ExternalMCPManager) createClient(serverCfg config.ExternalMCPServerConfig) ExternalMCPClient {
	transport := serverCfg.Transport
	if transport == "" {
		if serverCfg.Command != "" {
			transport = "stdio"
		} else if serverCfg.URL != "" {
			transport = "http"
		} else {
			return nil
		}
	}

	switch transport {
	case "http":
		if serverCfg.URL == "" {
			return nil
		}
		return newLazySDKClient(serverCfg, m.logger)
	case "simple_http":
		// Simple HTTP (one POST, one response), used for self-built MCP, etc.
		if serverCfg.URL == "" {
			return nil
		}
		return newLazySDKClient(serverCfg, m.logger)
	case "stdio":
		if serverCfg.Command == "" {
			return nil
		}
		return newLazySDKClient(serverCfg, m.logger)
	case "sse":
		if serverCfg.URL == "" {
			return nil
		}
		return newLazySDKClient(serverCfg, m.logger)
	default:
		return nil
	}
}

// DoConnect performs the actual connection
func (m *ExternalMCPManager) doConnect(name string, serverCfg config.ExternalMCPServerConfig, client ExternalMCPClient) error {
	timeout := time.Duration(serverCfg.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	// Initialize connection
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := client.Initialize(ctx); err != nil {
		return err
	}

	m.logger.Info("External MCP client connected",
		zap.String("name", name),
	)

	return nil
}

// SetClientStatus sets client status (via type assertion)
func (m *ExternalMCPManager) setClientStatus(client ExternalMCPClient, status string) {
	if c, ok := client.(*lazySDKClient); ok {
		c.setStatus(status)
	}
}

// ConnectClient connect client (asynchronous) - reserved for backward compatibility
func (m *ExternalMCPManager) connectClient(name string, serverCfg config.ExternalMCPServerConfig) error {
	client := m.createClient(serverCfg)
	if client == nil {
		return fmt.Errorf("Unable to create client: Unsupported transport mode")
	}

	// Set status to connecting
	m.setClientStatus(client, "connecting")

	// Initialize connection
	timeout := time.Duration(serverCfg.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := client.Initialize(ctx); err != nil {
		m.logger.Error("Failed to initialize external MCP client",
			zap.String("name", name),
			zap.Error(err),
		)
		return err
	}

	// Save client
	m.mu.Lock()
	m.clients[name] = client
	m.mu.Unlock()

	m.logger.Info("External MCP client connected",
		zap.String("name", name),
	)

	// The connection is successful, triggering tool number refresh and tool list cache refresh
	m.triggerToolCountRefresh()
	m.mu.RLock()
	if client, exists := m.clients[name]; exists {
		m.refreshToolCache(name, client)
	}
	m.mu.RUnlock()

	return nil
}

// IsEnabled checks whether it is enabled
func (m *ExternalMCPManager) isEnabled(cfg config.ExternalMCPServerConfig) bool {
	// Prefer using ExternalMCPEnable field
	// If not set, check the old enabled/disabled fields (backward compatibility)
	if cfg.ExternalMCPEnable {
		return true
	}
	// Backward compatibility: check for old fields
	if cfg.Disabled {
		return false
	}
	if cfg.Enabled {
		return true
	}
	// None are set, the default is enabled
	return true
}

// FindSubstring finds substrings (simple implementation)
func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// StartAllEnabled starts all enabled clients
func (m *ExternalMCPManager) StartAllEnabled() {
	m.mu.RLock()
	configs := make(map[string]config.ExternalMCPServerConfig)
	for k, v := range m.configs {
		configs[k] = v
	}
	m.mu.RUnlock()

	for name, cfg := range configs {
		if m.isEnabled(cfg) {
			go func(n string, c config.ExternalMCPServerConfig) {
				if err := m.connectClient(n, c); err != nil {
					// Check if it is a connection refused error (the service may not be started yet)
					errStr := strings.ToLower(err.Error())
					isConnectionRefused := strings.Contains(errStr, "connection refused") ||
						strings.Contains(errStr, "dial tcp") ||
						strings.Contains(errStr, "connect: connection refused")

					if isConnectionRefused {
						// The connection is refused, indicating that the target service may not have been started yet. This is normal.
						// Use the Warn level to remind the user that this is normal and they can start it manually or wait for the service to start and connect automatically.
						fields := []zap.Field{
							zap.String("name", n),
							zap.String("message", "The target service may not have started yet, this is normal. After the service is started, you can connect manually through the interface, or wait for automatic retry."),
							zap.Error(err),
						}

						// Add corresponding information according to the transmission mode
						transport := c.Transport
						if transport == "" {
							if c.Command != "" {
								transport = "stdio"
							} else if c.URL != "" {
								transport = "http"
							}
						}

						if transport == "http" && c.URL != "" {
							fields = append(fields, zap.String("url", c.URL))
						} else if transport == "stdio" && c.Command != "" {
							fields = append(fields, zap.String("command", c.Command))
						}

						m.logger.Warn("The external MCP service is not ready yet", fields...)
					} else {
						// For other errors, use the Error level
						m.logger.Error("Failed to start external MCP client",
							zap.String("name", n),
							zap.Error(err),
						)
					}
				}
			}(name, cfg)
		}
	}
}

// StopAll Stop all clients
func (m *ExternalMCPManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, client := range m.clients {
		client.Close()
		delete(m.clients, name)
	}

	// Clear all tool quantity caches
	m.toolCountsMu.Lock()
	m.toolCounts = make(map[string]int)
	m.toolCountsMu.Unlock()

	// Clear all tool list cache
	m.toolCacheMu.Lock()
	m.toolCache = make(map[string][]Tool)
	m.toolCacheMu.Unlock()

	// Stop background refresh (use select to avoid closing the channel repeatedly)
	select {
	case <-m.stopRefresh:
		// Already closed, no need to close again
	default:
		close(m.stopRefresh)
		m.refreshWg.Wait()
	}
}
