package security

import (
	"context"
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

// SetupTestExecutor creates an executor for testing
func setupTestExecutor(t *testing.T) (*Executor, *mcp.Server) {
	logger := zap.NewNop()
	mcpServer := mcp.NewServer(logger)
	
	cfg := &config.SecurityConfig{
		Tools: []config.ToolConfig{},
	}
	
	executor := NewExecutor(cfg, mcpServer, logger)
	return executor, mcpServer
}

// SetupTestStorage creates storage for testing
func setupTestStorage(t *testing.T) *storage.FileResultStorage {
	tmpDir := filepath.Join(os.TempDir(), "test_executor_storage_"+time.Now().Format("20060102_150405"))
	logger := zap.NewNop()
	
	storage, err := storage.NewFileResultStorage(tmpDir, logger)
	if err != nil {
		t.Fatalf("Failed to create test store: %v", err)
	}
	
	return storage
}

func TestExecutor_ExecuteInternalTool_QueryExecutionResult(t *testing.T) {
	executor, _ := setupTestExecutor(t)
	testStorage := setupTestStorage(t)
	executor.SetResultStorage(testStorage)
	
	// Prepare test data
	executionID := "test_exec_001"
	toolName := "nmap_scan"
	result := "Line 1: Port 22 open\nLine 2: Port 80 open\nLine 3: Port 443 open\nLine 4: error occurred"
	
	// Save test results
	err := testStorage.SaveResult(executionID, toolName, result)
	if err != nil {
		t.Fatalf("Failed to save test results: %v", err)
	}
	
	ctx := context.Background()
	
	// Test 1: Basic query (first page)
	args := map[string]interface{}{
		"execution_id": executionID,
		"page":         float64(1),
		"limit":        float64(2),
	}
	
	toolResult, err := executor.executeQueryExecutionResult(ctx, args)
	if err != nil {
		t.Fatalf("Failed to execute query: %v", err)
	}
	
	if toolResult.IsError {
		t.Fatalf("The query should succeed but returned error: %s", toolResult.Content[0].Text)
	}
	
	// Verification results contain expected content
	resultText := toolResult.Content[0].Text
	if !strings.Contains(resultText, executionID) {
		t.Errorf("The result should contain execution ID: %s", executionID)
	}
	
	if !strings.Contains(resultText, "No. 1/") {
		t.Errorf("Results should contain pagination information")
	}
	
	// Test 2: Search function
	args2 := map[string]interface{}{
		"execution_id": executionID,
		"search":       "error",
		"page":         float64(1),
		"limit":        float64(10),
	}
	
	toolResult2, err := executor.executeQueryExecutionResult(ctx, args2)
	if err != nil {
		t.Fatalf("Failed to perform search: %v", err)
	}
	
	if toolResult2.IsError {
		t.Fatalf("The search should have succeeded but returned error: %s", toolResult2.Content[0].Text)
	}
	
	resultText2 := toolResult2.Content[0].Text
	if !strings.Contains(resultText2, "error") {
		t.Errorf("The search results should contain the keyword: error")
	}
	
	// Test 3: Filter function
	args3 := map[string]interface{}{
		"execution_id": executionID,
		"filter":       "Port",
		"page":         float64(1),
		"limit":        float64(10),
	}
	
	toolResult3, err := executor.executeQueryExecutionResult(ctx, args3)
	if err != nil {
		t.Fatalf("Failed to perform filtering: %v", err)
	}
	
	if toolResult3.IsError {
		t.Fatalf("Filtering should succeed but returned error: %s", toolResult3.Content[0].Text)
	}
	
	resultText3 := toolResult3.Content[0].Text
	if !strings.Contains(resultText3, "Port") {
		t.Errorf("The filtered results should contain the keyword: Port")
	}
	
	// Test 4: Missing required parameters
	args4 := map[string]interface{}{
		"page": float64(1),
	}
	
	toolResult4, err := executor.executeQueryExecutionResult(ctx, args4)
	if err != nil {
		t.Fatalf("Failed to execute query: %v", err)
	}
	
	if !toolResult4.IsError {
		t.Fatal("Missing execution_id should return an error")
	}
	
	// Test 5: Non-existent execution ID
	args5 := map[string]interface{}{
		"execution_id": "nonexistent_id",
		"page":         float64(1),
	}
	
	toolResult5, err := executor.executeQueryExecutionResult(ctx, args5)
	if err != nil {
		t.Fatalf("Failed to execute query: %v", err)
	}
	
	if !toolResult5.IsError {
		t.Fatal("Non-existent execution ID should return an error")
	}
}

func TestExecutor_ExecuteInternalTool_UnknownTool(t *testing.T) {
	executor, _ := setupTestExecutor(t)
	
	ctx := context.Background()
	args := map[string]interface{}{
		"test": "value",
	}
	
	// Test unknown internal tool types
	toolResult, err := executor.executeInternalTool(ctx, "unknown_tool", "internal:unknown_tool", args)
	if err != nil {
		t.Fatalf("Failed to execute internal tool: %v", err)
	}
	
	if !toolResult.IsError {
		t.Fatal("Unknown tool type should return an error")
	}
	
	if !strings.Contains(toolResult.Content[0].Text, "Unknown internal tool type") {
		t.Errorf("The error message should contain 'Unknown internal tool type'")
	}
}

func TestExecutor_ExecuteInternalTool_NoStorage(t *testing.T) {
	executor, _ := setupTestExecutor(t)
	// Do not set up storage and test the uninitialized situation.
	
	ctx := context.Background()
	args := map[string]interface{}{
		"execution_id": "test_id",
	}
	
	toolResult, err := executor.executeQueryExecutionResult(ctx, args)
	if err != nil {
		t.Fatalf("Failed to execute query: %v", err)
	}
	
	if !toolResult.IsError {
		t.Fatal("Uninitialized storage should return an error")
	}
	
	if !strings.Contains(toolResult.Content[0].Text, "Result storage is not initialized") {
		t.Errorf("The error message should contain 'result storage not initialized'")
	}
}

func TestPaginateLines(t *testing.T) {
	lines := []string{"Line 1", "Line 2", "Line 3", "Line 4", "Line 5"}
	
	// Test the first page
	page := paginateLines(lines, 1, 2)
	if page.Page != 1 {
		t.Errorf("Page numbers don't match. Expected: 1, Actual: %d", page.Page)
	}
	if page.Limit != 2 {
		t.Errorf("The number of rows per page does not match. Expected: 2, Actual: %d", page.Limit)
	}
	if page.TotalLines != 5 {
		t.Errorf("The total number of rows does not match. Expected: 5, Actual: %d", page.TotalLines)
	}
	if page.TotalPages != 3 {
		t.Errorf("The total number of pages does not match. Expected: 3, Actual: %d", page.TotalPages)
	}
	if len(page.Lines) != 2 {
		t.Errorf("The number of rows on the first page does not match. Expected: 2, Actual: %d", len(page.Lines))
	}
	
	// Test second page
	page2 := paginateLines(lines, 2, 2)
	if len(page2.Lines) != 2 {
		t.Errorf("The number of rows on the second page does not match. Expected: 2, Actual: %d", len(page2.Lines))
	}
	if page2.Lines[0] != "Line 3" {
		t.Errorf("The first line of the second page does not match. Expected: Line 3, Actual: %s", page2.Lines[0])
	}
	
	// Test last page
	page3 := paginateLines(lines, 3, 2)
	if len(page3.Lines) != 1 {
		t.Errorf("The number of rows on the third page does not match. Expected: 1, Actual: %d", len(page3.Lines))
	}
	
	// Test for out-of-range page numbers (should return the last page)
	page4 := paginateLines(lines, 4, 2)
	if page4.Page != 3 {
		t.Errorf("Out-of-range page numbers should be corrected to the last page. Expected: 3, Actual: %d", page4.Page)
	}
	if len(page4.Lines) != 1 {
		t.Errorf("The last page should only have 1 row. Actual: %d rows", len(page4.Lines))
	}
	
	// Test invalid page number (less than 1)
	page0 := paginateLines(lines, 0, 2)
	if page0.Page != 1 {
		t.Errorf("Invalid page numbers should be corrected to 1. Actual: %d", page0.Page)
	}
	
	// Test empty list
	emptyPage := paginateLines([]string{}, 1, 10)
	if emptyPage.TotalLines != 0 {
		t.Errorf("The total number of rows for an empty list should be 0. Actual: %d", emptyPage.TotalLines)
	}
	if len(emptyPage.Lines) != 0 {
		t.Errorf("An empty list should return empty results. Actual: %d rows", len(emptyPage.Lines))
	}
}

