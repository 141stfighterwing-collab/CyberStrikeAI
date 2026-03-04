package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// MonitorStorage monitoring data storage interface
type MonitorStorage interface {
	SaveToolExecution(exec *ToolExecution) error
	LoadToolExecutions() ([]*ToolExecution, error)
	GetToolExecution(id string) (*ToolExecution, error)
	SaveToolStats(toolName string, stats *ToolStats) error
	LoadToolStats() (map[string]*ToolStats, error)
	UpdateToolStats(toolName string, totalCalls, successCalls, failedCalls int, lastCallTime *time.Time) error
}

// Server MCP server
type Server struct {
	tools                 map[string]ToolHandler
	toolDefs              map[string]Tool // Tool definition
	executions            map[string]*ToolExecution
	stats                 map[string]*ToolStats
	prompts               map[string]*Prompt   // Prompt word template
	resources             map[string]*Resource // Resource
	storage               MonitorStorage       // Optional persistent storage
	mu                    sync.RWMutex
	logger                *zap.Logger
	maxExecutionsInMemory int // Maximum number of execution records in memory
	sseClients            map[string]*sseClient
}

type sseClient struct {
	id   string
	send chan []byte
}

// ToolHandler tool processing function
type ToolHandler func(ctx context.Context, args map[string]interface{}) (*ToolResult, error)

// NewServer creates a new MCP server
func NewServer(logger *zap.Logger) *Server {
	return NewServerWithStorage(logger, nil)
}

// NewServerWithStorage creates a new MCP server (with persistent storage)
func NewServerWithStorage(logger *zap.Logger, storage MonitorStorage) *Server {
	s := &Server{
		tools:                 make(map[string]ToolHandler),
		toolDefs:              make(map[string]Tool),
		executions:            make(map[string]*ToolExecution),
		stats:                 make(map[string]*ToolStats),
		prompts:               make(map[string]*Prompt),
		resources:             make(map[string]*Resource),
		storage:               storage,
		logger:                logger,
		maxExecutionsInMemory: 1000, // By default, up to 1,000 execution records are kept in memory.
		sseClients:            make(map[string]*sseClient),
	}

	// Initialize default prompt words and resources
	s.initDefaultPrompts()
	s.initDefaultResources()

	return s
}

// RegisterTool Registration Tool
func (s *Server) RegisterTool(tool Tool, handler ToolHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[tool.Name] = handler
	s.toolDefs[tool.Name] = tool

	// Automatically create resource documents for tools
	resourceURI := fmt.Sprintf("tool://%s", tool.Name)
	s.resources[resourceURI] = &Resource{
		URI:         resourceURI,
		Name:        fmt.Sprintf("%sTool Documentation", tool.Name),
		Description: tool.Description,
		MimeType:    "text/plain",
	}
}

// ClearTools clears all tools (used to reload configuration)
func (s *Server) ClearTools() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Clear tools and tool definitions
	s.tools = make(map[string]ToolHandler)
	s.toolDefs = make(map[string]Tool)

	// Clear tool-related resources (retain other resources)
	newResources := make(map[string]*Resource)
	for uri, resource := range s.resources {
		// Preserve non-tool resources
		if !strings.HasPrefix(uri, "tool://") {
			newResources[uri] = resource
		}
	}
	s.resources = newResources
}

// HandleHTTP handles HTTP requests
func (s *Server) HandleHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		s.handleSSE(w, r)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Official MCP SSE specification: POST with sessionid indicates that the message is sent to this SSE session and the response is returned through the SSE stream
	if sessionID := r.URL.Query().Get("sessionid"); sessionID != "" {
		s.serveSSESessionMessage(w, r, sessionID)
		return
	}

	// Simple POST: the request body is JSON-RPC and the response is returned in the body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.sendError(w, nil, -32700, "Parse error", err.Error())
		return
	}

	var msg Message
	if err := json.Unmarshal(body, &msg); err != nil {
		s.sendError(w, nil, -32700, "Parse error", err.Error())
		return
	}

	response := s.handleMessage(&msg)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// ServeSSESessionMessage handles POSTs to an SSE session: reads the JSON-RPC request, processes the response and pushes the response through the session's SSE stream
func (s *Server) serveSSESessionMessage(w http.ResponseWriter, r *http.Request, sessionID string) {
	s.mu.RLock()
	client, exists := s.sseClients[sessionID]
	s.mu.RUnlock()
	if !exists || client == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	var msg Message
	if err := json.Unmarshal(body, &msg); err != nil {
		http.Error(w, "failed to parse body", http.StatusBadRequest)
		return
	}

	response := s.handleMessage(&msg)
	if response == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	respBytes, err := json.Marshal(response)
	if err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}

	select {
	case client.send <- respBytes:
		w.WriteHeader(http.StatusAccepted)
	default:
		http.Error(w, "session send buffer full", http.StatusServiceUnavailable)
	}
}

// HandleSSE handles SSE connections, compatible with the official MCP 2024-11-05 SSE specification:
// 1. The first event must be event: endpoint, and data is the URL of the client POST message (including sessionid)
// 2. The subsequent event is event: message, and data is the JSON-RPC response.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	sessionID := uuid.New().String()
	client := &sseClient{
		id:   sessionID,
		send: make(chan []byte, 32),
	}

	s.addSSEClient(client)
	defer s.removeSSEClient(client.id)

	// Official specifications: The first event is the endpoint, and data is the message endpoint URL (the client will POST the request to this URL)
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if r.URL.Scheme != "" {
		scheme = r.URL.Scheme
	}
	endpointURL := fmt.Sprintf("%s://%s%s?sessionid=%s", scheme, r.Host, r.URL.Path, sessionID)
	fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", endpointURL)
	flusher.Flush()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-client.send:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

// AddSSEClient register SSE client
func (s *Server) addSSEClient(client *sseClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sseClients[client.id] = client
}

// RemoveSSEClient removes the SSE client
func (s *Server) removeSSEClient(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if client, exists := s.sseClients[id]; exists {
		close(client.send)
		delete(s.sseClients, id)
	}
}

// HandleMessage handles MCP messages
func (s *Server) handleMessage(msg *Message) *Message {
	// Check if it is a notification - notifications have no id field and do not require a response
	isNotification := msg.ID.Value() == nil || msg.ID.String() == ""

	// If it is not a notification and the ID is empty, generate a new UUID
	if !isNotification && msg.ID.String() == "" {
		msg.ID = MessageID{value: uuid.New().String()}
	}

	switch msg.Method {
	case "initialize":
		return s.handleInitialize(msg)
	case "tools/list":
		return s.handleListTools(msg)
	case "tools/call":
		return s.handleCallTool(msg)
	case "prompts/list":
		return s.handleListPrompts(msg)
	case "prompts/get":
		return s.handleGetPrompt(msg)
	case "resources/list":
		return s.handleListResources(msg)
	case "resources/read":
		return s.handleReadResource(msg)
	case "sampling/request":
		return s.handleSamplingRequest(msg)
	case "notifications/initialized":
		// Notification type, no response required
		s.logger.Debug("Receive initialized notification")
		return nil
	case "":
		// Empty method name, may be a notification, does not return an error
		if isNotification {
			s.logger.Debug("Received notification message without method name")
			return nil
		}
		fallthrough
	default:
		// If it is a notification, no error response is returned
		if isNotification {
			s.logger.Debug("Received unknown notification", zap.String("method", msg.Method))
			return nil
		}
		// For requests, return method not found error
		return &Message{
			ID:      msg.ID,
			Type:    MessageTypeError,
			Version: "2.0",
			Error:   &Error{Code: -32601, Message: "Method not found"},
		}
	}
}

// HandleInitialize handles the initialization request
func (s *Server) handleInitialize(msg *Message) *Message {
	var req InitializeRequest
	if err := json.Unmarshal(msg.Params, &req); err != nil {
		return &Message{
			ID:      msg.ID,
			Type:    MessageTypeError,
			Version: "2.0",
			Error:   &Error{Code: -32602, Message: "Invalid params"},
		}
	}

	response := InitializeResponse{
		ProtocolVersion: ProtocolVersion,
		Capabilities: ServerCapabilities{
			Tools: map[string]interface{}{
				"listChanged": true,
			},
			Prompts: map[string]interface{}{
				"listChanged": true,
			},
			Resources: map[string]interface{}{
				"subscribe":   true,
				"listChanged": true,
			},
			Sampling: map[string]interface{}{},
		},
		ServerInfo: ServerInfo{
			Name:    "CyberStrikeAI",
			Version: "1.0.0",
		},
	}

	result, _ := json.Marshal(response)
	return &Message{
		ID:      msg.ID,
		Type:    MessageTypeResponse,
		Version: "2.0",
		Result:  result,
	}
}

// HandleListTools handles list tool requests
func (s *Server) handleListTools(msg *Message) *Message {
	s.mu.RLock()
	tools := make([]Tool, 0, len(s.toolDefs))
	for _, tool := range s.toolDefs {
		tools = append(tools, tool)
	}
	s.mu.RUnlock()
	s.logger.Debug("Tools/list request", zap.Int("Return the number of tools", len(tools)))

	response := ListToolsResponse{Tools: tools}
	result, _ := json.Marshal(response)
	return &Message{
		ID:      msg.ID,
		Type:    MessageTypeResponse,
		Version: "2.0",
		Result:  result,
	}
}

// HandleCallTool handles tool call requests
func (s *Server) handleCallTool(msg *Message) *Message {
	var req CallToolRequest
	if err := json.Unmarshal(msg.Params, &req); err != nil {
		return &Message{
			ID:      msg.ID,
			Type:    MessageTypeError,
			Version: "2.0",
			Error:   &Error{Code: -32602, Message: "Invalid params"},
		}
	}

	executionID := uuid.New().String()
	execution := &ToolExecution{
		ID:        executionID,
		ToolName:  req.Name,
		Arguments: req.Arguments,
		Status:    "running",
		StartTime: time.Now(),
	}

	s.mu.Lock()
	s.executions[executionID] = execution
	// If the execution records in memory exceed the limit, clean up the oldest records
	s.cleanupOldExecutions()
	s.mu.Unlock()

	if s.storage != nil {
		if err := s.storage.SaveToolExecution(execution); err != nil {
			s.logger.Warn("Failed to save execution records to database", zap.Error(err))
		}
	}

	s.mu.RLock()
	handler, exists := s.tools[req.Name]
	s.mu.RUnlock()

	if !exists {
		execution.Status = "failed"
		execution.Error = "Tool not found"
		now := time.Now()
		execution.EndTime = &now
		execution.Duration = now.Sub(execution.StartTime)

		if s.storage != nil {
			if err := s.storage.SaveToolExecution(execution); err != nil {
				s.logger.Warn("Failed to save execution records to database", zap.Error(err))
			}
			s.mu.Lock()
			delete(s.executions, executionID)
			s.mu.Unlock()
		}

		s.updateStats(req.Name, true)

		return &Message{
			ID:      msg.ID,
			Type:    MessageTypeError,
			Version: "2.0",
			Error:   &Error{Code: -32601, Message: "Tool not found"},
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	s.logger.Info("Start executing tool",
		zap.String("toolName", req.Name),
		zap.Any("arguments", req.Arguments),
	)

	result, err := handler(ctx, req.Arguments)
	now := time.Now()
	var failed bool
	var finalResult *ToolResult

	s.mu.Lock()
	execution.EndTime = &now
	execution.Duration = now.Sub(execution.StartTime)

	if err != nil {
		execution.Status = "failed"
		execution.Error = err.Error()
		failed = true
	} else if result != nil && result.IsError {
		execution.Status = "failed"
		if len(result.Content) > 0 {
			execution.Error = result.Content[0].Text
		} else {
			execution.Error = "Tool execution returns incorrect results"
		}
		execution.Result = result
		failed = true
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
		failed = false
	}

	finalResult = execution.Result
	s.mu.Unlock()

	if s.storage != nil {
		if err := s.storage.SaveToolExecution(execution); err != nil {
			s.logger.Warn("Failed to save execution records to database", zap.Error(err))
		}
	}

	s.updateStats(req.Name, failed)

	if s.storage != nil {
		s.mu.Lock()
		delete(s.executions, executionID)
		s.mu.Unlock()
	}

	if err != nil {
		s.logger.Error("Tool execution failed",
			zap.String("toolName", req.Name),
			zap.Error(err),
		)

		errorResult, _ := json.Marshal(CallToolResponse{
			Content: []Content{
				{Type: "text", Text: fmt.Sprintf("Tool execution failed: %v", err)},
			},
			IsError: true,
		})
		return &Message{
			ID:      msg.ID,
			Type:    MessageTypeResponse,
			Version: "2.0",
			Result:  errorResult,
		}
	}

	if finalResult != nil && finalResult.IsError {
		s.logger.Warn("Tool execution returns incorrect results",
			zap.String("toolName", req.Name),
		)

		errorResult, _ := json.Marshal(CallToolResponse{
			Content: finalResult.Content,
			IsError: true,
		})
		return &Message{
			ID:      msg.ID,
			Type:    MessageTypeResponse,
			Version: "2.0",
			Result:  errorResult,
		}
	}

	if finalResult == nil {
		finalResult = &ToolResult{
			Content: []Content{
				{Type: "text", Text: "Tool execution completed but no results returned"},
			},
		}
	}

	resultJSON, _ := json.Marshal(CallToolResponse{
		Content: finalResult.Content,
		IsError: false,
	})

	s.logger.Info("Tool execution completed",
		zap.String("toolName", req.Name),
		zap.Bool("isError", finalResult.IsError),
	)

	return &Message{
		ID:      msg.ID,
		Type:    MessageTypeResponse,
		Version: "2.0",
		Result:  resultJSON,
	}
}

// UpdateStats updates statistics
func (s *Server) updateStats(toolName string, failed bool) {
	now := time.Now()
	if s.storage != nil {
		totalCalls := 1
		successCalls := 0
		failedCalls := 0
		if failed {
			failedCalls = 1
		} else {
			successCalls = 1
		}
		if err := s.storage.UpdateToolStats(toolName, totalCalls, successCalls, failedCalls, &now); err != nil {
			s.logger.Warn("Failed to save statistics to database", zap.Error(err))
		}
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stats[toolName] == nil {
		s.stats[toolName] = &ToolStats{
			ToolName: toolName,
		}
	}

	stats := s.stats[toolName]
	stats.TotalCalls++
	stats.LastCallTime = &now

	if failed {
		stats.FailedCalls++
	} else {
		stats.SuccessCalls++
	}
}

// GetExecution gets execution records (first search from memory, then search from database)
func (s *Server) GetExecution(id string) (*ToolExecution, bool) {
	s.mu.RLock()
	exec, exists := s.executions[id]
	s.mu.RUnlock()

	if exists {
		return exec, true
	}

	if s.storage != nil {
		exec, err := s.storage.GetToolExecution(id)
		if err == nil {
			return exec, true
		}
	}

	return nil, false
}

// LoadHistoricalData loads historical data from the database
func (s *Server) loadHistoricalData() {
	if s.storage == nil {
		return
	}

	// Load historical execution records (last 1000 records)
	executions, err := s.storage.LoadToolExecutions()
	if err != nil {
		s.logger.Warn("Failed to load historical execution records", zap.Error(err))
	} else {
		s.mu.Lock()
		for _, exec := range executions {
			// Only load the most recent maxExecutionsInMemory entries to avoid excessive memory usage
			if len(s.executions) < s.maxExecutionsInMemory {
				s.executions[exec.ID] = exec
			} else {
				break
			}
		}
		s.mu.Unlock()
		s.logger.Info("Load historical execution records", zap.Int("count", len(executions)))
	}

	// Load historical statistics
	stats, err := s.storage.LoadToolStats()
	if err != nil {
		s.logger.Warn("Failed to load historical statistics", zap.Error(err))
	} else {
		s.mu.Lock()
		for k, v := range stats {
			s.stats[k] = v
		}
		s.mu.Unlock()
		s.logger.Info("Load historical statistics", zap.Int("count", len(stats)))
	}
}

// GetAllExecutions gets all execution records (merging memory and database)
func (s *Server) GetAllExecutions() []*ToolExecution {
	if s.storage != nil {
		dbExecutions, err := s.storage.LoadToolExecutions()
		if err == nil {
			execMap := make(map[string]*ToolExecution)
			for _, exec := range dbExecutions {
				if _, exists := execMap[exec.ID]; !exists {
					execMap[exec.ID] = exec
				}
			}

			s.mu.RLock()
			for id, exec := range s.executions {
				if _, exists := execMap[id]; !exists {
					execMap[id] = exec
				}
			}
			s.mu.RUnlock()

			result := make([]*ToolExecution, 0, len(execMap))
			for _, exec := range execMap {
				result = append(result, exec)
			}
			return result
		} else {
			s.logger.Warn("Failed to load execution records from database", zap.Error(err))
		}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	memExecutions := make([]*ToolExecution, 0, len(s.executions))
	for _, exec := range s.executions {
		memExecutions = append(memExecutions, exec)
	}
	return memExecutions
}

// GetStats Get statistics (combined memory and database)
func (s *Server) GetStats() map[string]*ToolStats {
	if s.storage != nil {
		dbStats, err := s.storage.LoadToolStats()
		if err == nil {
			return dbStats
		}
		s.logger.Warn("Loading statistics from database failed", zap.Error(err))
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	memStats := make(map[string]*ToolStats)
	for k, v := range s.stats {
		statCopy := *v
		memStats[k] = &statCopy
	}

	return memStats
}

// GetAllTools gets all registered tools (used by Agent to dynamically obtain the tool list)
func (s *Server) GetAllTools() []Tool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tools := make([]Tool, 0, len(s.toolDefs))
	for _, tool := range s.toolDefs {
		tools = append(tools, tool)
	}
	return tools
}

// CallTool calls the tool directly (for internal calls)
func (s *Server) CallTool(ctx context.Context, toolName string, args map[string]interface{}) (*ToolResult, string, error) {
	s.mu.RLock()
	handler, exists := s.tools[toolName]
	s.mu.RUnlock()

	if !exists {
		return nil, "", fmt.Errorf("Tool %s not found", toolName)
	}

	// Create execution record
	executionID := uuid.New().String()
	execution := &ToolExecution{
		ID:        executionID,
		ToolName:  toolName,
		Arguments: args,
		Status:    "running",
		StartTime: time.Now(),
	}

	s.mu.Lock()
	s.executions[executionID] = execution
	// If the execution records in memory exceed the limit, clean up the oldest records
	s.cleanupOldExecutions()
	s.mu.Unlock()

	if s.storage != nil {
		if err := s.storage.SaveToolExecution(execution); err != nil {
			s.logger.Warn("Failed to save execution records to database", zap.Error(err))
		}
	}

	result, err := handler(ctx, args)

	s.mu.Lock()
	now := time.Now()
	execution.EndTime = &now
	execution.Duration = now.Sub(execution.StartTime)
	var failed bool
	var finalResult *ToolResult

	if err != nil {
		execution.Status = "failed"
		execution.Error = err.Error()
		failed = true
	} else if result != nil && result.IsError {
		execution.Status = "failed"
		if len(result.Content) > 0 {
			execution.Error = result.Content[0].Text
		} else {
			execution.Error = "Tool execution returns incorrect results"
		}
		execution.Result = result
		failed = true
		finalResult = result
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
		finalResult = result
		failed = false
	}

	if finalResult == nil {
		finalResult = execution.Result
	}
	s.mu.Unlock()

	if s.storage != nil {
		if err := s.storage.SaveToolExecution(execution); err != nil {
			s.logger.Warn("Failed to save execution records to database", zap.Error(err))
		}
	}

	s.updateStats(toolName, failed)

	if s.storage != nil {
		s.mu.Lock()
		delete(s.executions, executionID)
		s.mu.Unlock()
	}

	if err != nil {
		return nil, executionID, err
	}

	return finalResult, executionID, nil
}

// CleanupOldExecutions cleans old execution records to prevent unlimited memory growth
func (s *Server) cleanupOldExecutions() {
	if len(s.executions) <= s.maxExecutionsInMemory {
		return
	}

	// Sort by start time to find the oldest record
	type execWithTime struct {
		id        string
		startTime time.Time
	}
	execs := make([]execWithTime, 0, len(s.executions))
	for id, exec := range s.executions {
		execs = append(execs, execWithTime{
			id:        id,
			startTime: exec.StartTime,
		})
	}

	// Use the sort package for efficient sorting (oldest first)
	sort.Slice(execs, func(i, j int) bool {
		return execs[i].startTime.Before(execs[j].startTime)
	})

	// Delete the oldest records, keeping maxExecutionsInMemory records
	toDelete := len(s.executions) - s.maxExecutionsInMemory
	for i := 0; i < toDelete; i++ {
		delete(s.executions, execs[i].id)
	}

	s.logger.Debug("Clean up old execution records",
		zap.Int("before", len(execs)),
		zap.Int("after", len(s.executions)),
		zap.Int("deleted", toDelete),
	)
}

// InitDefaultPrompts initializes the default prompt word template
func (s *Server) initDefaultPrompts() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Network security testing prompts
	s.prompts["security_scan"] = &Prompt{
		Name:        "security_scan",
		Description: "Generate prompt words for network security scanning tasks",
		Arguments: []PromptArgument{
			{Name: "target", Description: "Scan target (IP address or domain name)", Required: true},
			{Name: "scan_type", Description: "Scan type (port, vuln, web, etc.)", Required: false},
		},
	}

	// Penetration testing prompts
	s.prompts["penetration_test"] = &Prompt{
		Name:        "penetration_test",
		Description: "Generate prompt words for penetration testing tasks",
		Arguments: []PromptArgument{
			{Name: "target", Description: "Test target", Required: true},
			{Name: "scope", Description: "Test range", Required: false},
		},
	}
}

// InitDefaultResources initializes default resources
// Note: Tool resources are now automatically created when RegisterTool, this function is reserved for other non-tool resources
func (s *Server) initDefaultResources() {
	// Tool resources have been changed to be automatically created when RegisterTool, no need to hard code here
}

// HandleListPrompts handles requests to list prompt words
func (s *Server) handleListPrompts(msg *Message) *Message {
	s.mu.RLock()
	prompts := make([]Prompt, 0, len(s.prompts))
	for _, prompt := range s.prompts {
		prompts = append(prompts, *prompt)
	}
	s.mu.RUnlock()

	response := ListPromptsResponse{
		Prompts: prompts,
	}
	result, _ := json.Marshal(response)
	return &Message{
		ID:      msg.ID,
		Type:    MessageTypeResponse,
		Version: "2.0",
		Result:  result,
	}
}

// HandleGetPrompt handles the request to get the prompt word
func (s *Server) handleGetPrompt(msg *Message) *Message {
	var req GetPromptRequest
	if err := json.Unmarshal(msg.Params, &req); err != nil {
		return &Message{
			ID:      msg.ID,
			Type:    MessageTypeError,
			Version: "2.0",
			Error:   &Error{Code: -32602, Message: "Invalid params"},
		}
	}

	s.mu.RLock()
	prompt, exists := s.prompts[req.Name]
	s.mu.RUnlock()

	if !exists {
		return &Message{
			ID:      msg.ID,
			Type:    MessageTypeError,
			Version: "2.0",
			Error:   &Error{Code: -32601, Message: "Prompt not found"},
		}
	}

	// Generate message based on prompt word name
	messages := s.generatePromptMessages(prompt, req.Arguments)

	response := GetPromptResponse{
		Messages: messages,
	}
	result, _ := json.Marshal(response)
	return &Message{
		ID:      msg.ID,
		Type:    MessageTypeResponse,
		Version: "2.0",
		Result:  result,
	}
}

// GeneratePromptMessages generates prompt word messages
func (s *Server) generatePromptMessages(prompt *Prompt, args map[string]interface{}) []PromptMessage {
	messages := []PromptMessage{}

	switch prompt.Name {
	case "security_scan":
		target, _ := args["target"].(string)
		scanType, _ := args["scan_type"].(string)
		if scanType == "" {
			scanType = "comprehensive"
		}

Content := fmt.Sprintf(`Please perform %s security scan on target %s. Includes:
1. Port scanning and service identification
2. Vulnerability detection
3. Web application security testing
4. Generate detailed security reports`, target, scanType)

		messages = append(messages, PromptMessage{
			Role:    "user",
			Content: content,
		})

	case "penetration_test":
		target, _ := args["target"].(string)
		scope, _ := args["scope"].(string)

Content := fmt.Sprintf(`Please perform penetration testing on target %s.`, target)
		if scope != "" {
			content += fmt.Sprintf("Test range: %s", scope)
		}
		content += "\nPlease conduct comprehensive security testing according to OWASP Top 10."

		messages = append(messages, PromptMessage{
			Role:    "user",
			Content: content,
		})

	default:
		messages = append(messages, PromptMessage{
			Role:    "user",
			Content: "Please perform security testing tasks",
		})
	}

	return messages
}

// HandleListResources handles list resource requests
func (s *Server) handleListResources(msg *Message) *Message {
	s.mu.RLock()
	resources := make([]Resource, 0, len(s.resources))
	for _, resource := range s.resources {
		resources = append(resources, *resource)
	}
	s.mu.RUnlock()

	response := ListResourcesResponse{
		Resources: resources,
	}
	result, _ := json.Marshal(response)
	return &Message{
		ID:      msg.ID,
		Type:    MessageTypeResponse,
		Version: "2.0",
		Result:  result,
	}
}

// HandleReadResource handles read resource requests
func (s *Server) handleReadResource(msg *Message) *Message {
	var req ReadResourceRequest
	if err := json.Unmarshal(msg.Params, &req); err != nil {
		return &Message{
			ID:      msg.ID,
			Type:    MessageTypeError,
			Version: "2.0",
			Error:   &Error{Code: -32602, Message: "Invalid params"},
		}
	}

	s.mu.RLock()
	resource, exists := s.resources[req.URI]
	s.mu.RUnlock()

	if !exists {
		return &Message{
			ID:      msg.ID,
			Type:    MessageTypeError,
			Version: "2.0",
			Error:   &Error{Code: -32601, Message: "Resource not found"},
		}
	}

	// Generate resource content
	content := s.generateResourceContent(resource)

	response := ReadResourceResponse{
		Contents: []ResourceContent{content},
	}
	result, _ := json.Marshal(response)
	return &Message{
		ID:      msg.ID,
		Type:    MessageTypeResponse,
		Version: "2.0",
		Result:  result,
	}
}

// GenerateResourceContent generates resource content
func (s *Server) generateResourceContent(resource *Resource) ResourceContent {
	content := ResourceContent{
		URI:      resource.URI,
		MimeType: resource.MimeType,
	}

	// If it is a tool resource, generate detailed documentation
	if strings.HasPrefix(resource.URI, "tool://") {
		toolName := strings.TrimPrefix(resource.URI, "tool://")
		content.Text = s.generateToolDocumentation(toolName, resource)
	} else {
		// Other resources use descriptions or default content
		content.Text = resource.Description
	}

	return content
}

// GenerateToolDocumentation generates tool documentation
// NOTE: Hardcoded tool documentation has been removed, now only the information from the tool definition is used
func (s *Server) generateToolDocumentation(toolName string, resource *Resource) string {
	// Get the tool definition for more detailed information
	s.mu.RLock()
	tool, hasTool := s.toolDefs[toolName]
	s.mu.RUnlock()

	// Use the description information in the tool definition
	if hasTool {
		doc := fmt.Sprintf("%s\n\n", resource.Description)
		if tool.InputSchema != nil {
			if props, ok := tool.InputSchema["properties"].(map[string]interface{}); ok {
				doc += "Parameter description:\n"
				for paramName, paramInfo := range props {
					if paramMap, ok := paramInfo.(map[string]interface{}); ok {
						if desc, ok := paramMap["description"].(string); ok {
							doc += fmt.Sprintf("- %s: %s\n", paramName, desc)
						}
					}
				}
			}
		}
		return doc
	}
	return resource.Description
}

// HandleSamplingRequest handles sampling requests
func (s *Server) handleSamplingRequest(msg *Message) *Message {
	var req SamplingRequest
	if err := json.Unmarshal(msg.Params, &req); err != nil {
		return &Message{
			ID:      msg.ID,
			Type:    MessageTypeError,
			Version: "2.0",
			Error:   &Error{Code: -32602, Message: "Invalid params"},
		}
	}

	// NOTE: Sampling functions typically require connection to an actual LLM service
	// A placeholder response is returned here, the actual implementation needs to integrate the LLM API
	s.logger.Warn("Sampling request received but not fully implemented",
		zap.Any("request", req),
	)

	response := SamplingResponse{
		Content: []SamplingContent{
			{
				Type: "text",
				Text: "The sampling function requires configuring the LLM service. Please use the Agent Loop API for AI conversations.",
			},
		},
		StopReason: "length",
	}
	result, _ := json.Marshal(response)
	return &Message{
		ID:      msg.ID,
		Type:    MessageTypeResponse,
		Version: "2.0",
		Result:  result,
	}
}

// RegisterPrompt registration prompt word template
func (s *Server) RegisterPrompt(prompt *Prompt) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prompts[prompt.Name] = prompt
}

// RegisterResource Register resource
func (s *Server) RegisterResource(resource *Resource) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resources[resource.URI] = resource
}

// HandleStdio handles standard input and output (for stdio transfer mode)
// The MCP protocol uses newline-delimited JSON-RPC messages; the pipeline needs to be flushed after each write, otherwise the client will not be able to read the response.
func (s *Server) HandleStdio() error {
	decoder := json.NewDecoder(os.Stdin)
	stdout := bufio.NewWriter(os.Stdout)
	encoder := json.NewEncoder(stdout)
	// NOTE: Without setting indentation, MCP protocol expects compact JSON format

	for {
		var msg Message
		if err := decoder.Decode(&msg); err != nil {
			if err == io.EOF {
				break
			}
			// Log output to stderr to avoid interfering with stdout's JSON-RPC communication
			s.logger.Error("Failed to read message", zap.Error(err))
			// Send error response
			errorMsg := Message{
				ID:      msg.ID,
				Type:    MessageTypeError,
				Version: "2.0",
				Error:   &Error{Code: -32700, Message: "Parse error", Data: err.Error()},
			}
			if err := encoder.Encode(errorMsg); err != nil {
				return fmt.Errorf("Failed to send error response: %w", err)
			}
			if err := stdout.Flush(); err != nil {
				return fmt.Errorf("Failed to refresh stdout: %w", err)
			}
			continue
		}

		// Process messages
		response := s.handleMessage(&msg)

		// If it is a notification (response is nil), there is no need to send a response
		if response == nil {
			continue
		}

		// Send response
		if err := encoder.Encode(response); err != nil {
			return fmt.Errorf("Failed to send response: %w", err)
		}
		if err := stdout.Flush(); err != nil {
			return fmt.Errorf("Failed to refresh stdout: %w", err)
		}
	}

	return nil
}

// SendError sends an error response
func (s *Server) sendError(w http.ResponseWriter, id interface{}, code int, message, data string) {
	var msgID MessageID
	if id != nil {
		msgID = MessageID{value: id}
	}
	response := Message{
		ID:      msgID,
		Type:    MessageTypeError,
		Version: "2.0",
		Error:   &Error{Code: code, Message: message, Data: data},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
