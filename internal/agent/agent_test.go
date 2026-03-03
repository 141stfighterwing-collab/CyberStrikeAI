package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/storage"

	"go.uber.org/zap"
)

// SetupTestAgent creates an Agent for testing
func setupTestAgent(t *testing.T) (*Agent, *storage.FileResultStorage) {
	logger := zap.NewNop()
	mcpServer := mcp.NewServer(logger)
	
	openAICfg := &config.OpenAIConfig{
		APIKey:  "test-key",
		BaseURL: "https://api.test.com/v1",
		Model:   "test-model",
	}
	
	agentCfg := &config.AgentConfig{
		MaxIterations:        10,
		LargeResultThreshold: 100, // Set smaller thresholds for easier testing
		ResultStorageDir:     "",
	}
	
	agent := NewAgent(openAICfg, agentCfg, mcpServer, nil, logger, 10)
	
	// Create test storage
	tmpDir := filepath.Join(os.TempDir(), "test_agent_storage_"+time.Now().Format("20060102_150405"))
	testStorage, err := storage.NewFileResultStorage(tmpDir, logger)
	if err != nil {
		t.Fatalf("Failed to create test store: %v", err)
	}
	
	agent.SetResultStorage(testStorage)
	
	return agent, testStorage
}

func TestAgent_FormatMinimalNotification(t *testing.T) {
	agent, testStorage := setupTestAgent(t)
	_ = testStorage // Avoid unused variable warnings
	
	executionID := "test_exec_001"
	toolName := "nmap_scan"
	size := 50000
	lineCount := 1000
	filePath := "tmp/test_exec_001.txt"
	
	notification := agent.formatMinimalNotification(executionID, toolName, size, lineCount, filePath)
	
	// Verification notification contains necessary information
	if !strings.Contains(notification, executionID) {
		t.Errorf("The notification should contain execution ID: %s", executionID)
	}
	
	if !strings.Contains(notification, toolName) {
		t.Errorf("The notification should contain the tool name: %s", toolName)
	}
	
	if !strings.Contains(notification, "50000") {
		t.Errorf("Notifications should contain size information")
	}
	
	if !strings.Contains(notification, "1000") {
		t.Errorf("The notification should contain row number information")
	}
	
	if !strings.Contains(notification, "query_execution_result") {
		t.Errorf("The notification should include instructions for using the query tool")
	}
}

func TestAgent_ExecuteToolViaMCP_LargeResult(t *testing.T) {
	agent, _ := setupTestAgent(t)
	
	// Create simulated MCP tool results (large results)
	largeResult := &mcp.ToolResult{
		Content: []mcp.Content{
			{
				Type: "text",
				Text: strings.Repeat("This is a test line with some content.\n", 1000), // About 50KB
			},
		},
		IsError: false,
	}
	
	// Simulate MCP server returning large results
	// Since we need to simulate the behavior of CallTool, we need to create a mock or use an actual MCP server.
	// In order to simplify the test, we directly test the result processing logic
	
	// Set threshold
	agent.mu.Lock()
	agent.largeResultThreshold = 1000 // Set a smaller threshold
	agent.mu.Unlock()
	
	// Create execution ID
	executionID := "test_exec_large_001"
	toolName := "test_tool"
	
	// Format results
	var resultText strings.Builder
	for _, content := range largeResult.Content {
		resultText.WriteString(content.Text)
		resultText.WriteString("\n")
	}
	
	resultStr := resultText.String()
	resultSize := len(resultStr)
	
	// Detect large results and save
	agent.mu.RLock()
	threshold := agent.largeResultThreshold
	storage := agent.resultStorage
	agent.mu.RUnlock()
	
	if resultSize > threshold && storage != nil {
		// Save big results
		err := storage.SaveResult(executionID, toolName, resultStr)
		if err != nil {
			t.Fatalf("Failed to save large result: %v", err)
		}
		
		// Generate notification
		lines := strings.Split(resultStr, "\n")
		filePath := storage.GetResultPath(executionID)
		notification := agent.formatMinimalNotification(executionID, toolName, resultSize, len(lines), filePath)
		
		// Verify notification format
		if !strings.Contains(notification, executionID) {
			t.Errorf("The notification should contain the execution ID")
		}
		
		// Verification results saved
		savedResult, err := storage.GetResult(executionID)
		if err != nil {
			t.Fatalf("Failed to retrieve saved results: %v", err)
		}
		
		if savedResult != resultStr {
			t.Errorf("The saved results do not match the original results")
		}
	} else {
		t.Fatal("Large results should be detected and saved")
	}
}

func TestAgent_ExecuteToolViaMCP_SmallResult(t *testing.T) {
	agent, _ := setupTestAgent(t)
	
	// Create small results
	smallResult := &mcp.ToolResult{
		Content: []mcp.Content{
			{
				Type: "text",
				Text: "Small result content",
			},
		},
		IsError: false,
	}
	
	// Set a larger threshold
	agent.mu.Lock()
	agent.largeResultThreshold = 100000 // 100KB
	agent.mu.Unlock()
	
	// Format results
	var resultText strings.Builder
	for _, content := range smallResult.Content {
		resultText.WriteString(content.Text)
		resultText.WriteString("\n")
	}
	
	resultStr := resultText.String()
	resultSize := len(resultStr)
	
	// Test big results
	agent.mu.RLock()
	threshold := agent.largeResultThreshold
	storage := agent.resultStorage
	agent.mu.RUnlock()
	
	if resultSize > threshold && storage != nil {
		t.Fatal("Small results should not be saved")
	}
	
	// Small results should be returned directly
	if resultSize <= threshold {
		// This is expected behavior
		if resultStr == "" {
			t.Fatal("Small results should be returned directly and should not be empty")
		}
	}
}

func TestAgent_SetResultStorage(t *testing.T) {
	agent, _ := setupTestAgent(t)
	
	// Create new storage
	tmpDir := filepath.Join(os.TempDir(), "test_new_storage_"+time.Now().Format("20060102_150405"))
	newStorage, err := storage.NewFileResultStorage(tmpDir, zap.NewNop())
	if err != nil {
		t.Fatalf("Failed to create new storage: %v", err)
	}
	
	// Set up new storage
	agent.SetResultStorage(newStorage)
	
	// Verify that the store has been updated
	agent.mu.RLock()
	currentStorage := agent.resultStorage
	agent.mu.RUnlock()
	
	if currentStorage != newStorage {
		t.Fatal("Storage not updated correctly")
	}
	
	// Clean up
	os.RemoveAll(tmpDir)
}

func TestAgent_NewAgent_DefaultValues(t *testing.T) {
	logger := zap.NewNop()
	mcpServer := mcp.NewServer(logger)
	
	openAICfg := &config.OpenAIConfig{
		APIKey:  "test-key",
		BaseURL: "https://api.test.com/v1",
		Model:   "test-model",
	}
	
	// Test default configuration
	agent := NewAgent(openAICfg, nil, mcpServer, nil, logger, 0)
	
	if agent.maxIterations != 30 {
		t.Errorf("The default number of iterations does not match. Expected: 30, Actual: %d", agent.maxIterations)
	}
	
	agent.mu.RLock()
	threshold := agent.largeResultThreshold
	agent.mu.RUnlock()
	
	if threshold != 50*1024 {
		t.Errorf("Default threshold does not match. Expected: %d, Actual: %d", 50*1024, threshold)
	}
}

func TestAgent_NewAgent_CustomConfig(t *testing.T) {
	logger := zap.NewNop()
	mcpServer := mcp.NewServer(logger)
	
	openAICfg := &config.OpenAIConfig{
		APIKey:  "test-key",
		BaseURL: "https://api.test.com/v1",
		Model:   "test-model",
	}
	
	agentCfg := &config.AgentConfig{
		MaxIterations:        20,
		LargeResultThreshold: 100 * 1024, // 100KB
		ResultStorageDir:     "custom_tmp",
	}
	
	agent := NewAgent(openAICfg, agentCfg, mcpServer, nil, logger, 15)
	
	if agent.maxIterations != 15 {
		t.Errorf("The number of iterations does not match. Expected: 15, Actual: %d", agent.maxIterations)
	}
	
	agent.mu.RLock()
	threshold := agent.largeResultThreshold
	agent.mu.RUnlock()
	
	if threshold != 100*1024 {
		t.Errorf("Threshold mismatch. Expected: %d, Actual: %d", 100*1024, threshold)
	}
}

