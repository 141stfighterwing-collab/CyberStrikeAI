package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

// SetupTestStorage creates a storage instance for testing
func setupTestStorage(t *testing.T) (*FileResultStorage, string) {
	tmpDir := filepath.Join(os.TempDir(), "test_result_storage_"+time.Now().Format("20060102_150405"))
	logger := zap.NewNop()

	storage, err := NewFileResultStorage(tmpDir, logger)
	if err != nil {
		t.Fatalf("Failed to create test store: %v", err)
	}

	return storage, tmpDir
}

// CleanupTestStorage cleans test data
func cleanupTestStorage(t *testing.T, tmpDir string) {
	if err := os.RemoveAll(tmpDir); err != nil {
		t.Logf("Failed to clean test directory: %v", err)
	}
}

func TestNewFileResultStorage(t *testing.T) {
	tmpDir := filepath.Join(os.TempDir(), "test_new_storage_"+time.Now().Format("20060102_150405"))
	defer cleanupTestStorage(t, tmpDir)

	logger := zap.NewNop()
	storage, err := NewFileResultStorage(tmpDir, logger)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	if storage == nil {
		t.Fatal("Store instance is nil")
	}

	// Verify directory has been created
	if _, err := os.Stat(tmpDir); os.IsNotExist(err) {
		t.Fatal("Storage directory not created")
	}
}

func TestFileResultStorage_SaveResult(t *testing.T) {
	storage, tmpDir := setupTestStorage(t)
	defer cleanupTestStorage(t, tmpDir)

	executionID := "test_exec_001"
	toolName := "nmap_scan"
	result := "Line 1\nLine 2\nLine 3\nLine 4\nLine 5"

	err := storage.SaveResult(executionID, toolName, result)
	if err != nil {
		t.Fatalf("Failed to save results: %v", err)
	}

	// Verify that the result file exists
	resultPath := filepath.Join(tmpDir, executionID+".txt")
	if _, err := os.Stat(resultPath); os.IsNotExist(err) {
		t.Fatal("Result file not created")
	}

	// Verify metadata file exists
	metadataPath := filepath.Join(tmpDir, executionID+".meta.json")
	if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
		t.Fatal("Metadata file not created")
	}
}

func TestFileResultStorage_GetResult(t *testing.T) {
	storage, tmpDir := setupTestStorage(t)
	defer cleanupTestStorage(t, tmpDir)

	executionID := "test_exec_002"
	toolName := "test_tool"
	expectedResult := "Test result content\nLine 2\nLine 3"

	// Save the results first
	err := storage.SaveResult(executionID, toolName, expectedResult)
	if err != nil {
		t.Fatalf("Failed to save results: %v", err)
	}

	// Get results
	result, err := storage.GetResult(executionID)
	if err != nil {
		t.Fatalf("Failed to get results: %v", err)
	}

	if result != expectedResult {
		t.Errorf("The result does not match. Expected: %q, Actual: %q", expectedResult, result)
	}

	// Testing a non-existent execution ID
	_, err = storage.GetResult("nonexistent_id")
	if err == nil {
		t.Fatal("Should return an error")
	}
}

func TestFileResultStorage_GetResultMetadata(t *testing.T) {
	storage, tmpDir := setupTestStorage(t)
	defer cleanupTestStorage(t, tmpDir)

	executionID := "test_exec_003"
	toolName := "test_tool"
	result := "Line 1\nLine 2\nLine 3"

	// Save results
	err := storage.SaveResult(executionID, toolName, result)
	if err != nil {
		t.Fatalf("Failed to save results: %v", err)
	}

	// Get metadata
	metadata, err := storage.GetResultMetadata(executionID)
	if err != nil {
		t.Fatalf("Failed to get metadata: %v", err)
	}

	if metadata.ExecutionID != executionID {
		t.Errorf("Execution IDs do not match. Expected: %s, Actual: %s", executionID, metadata.ExecutionID)
	}

	if metadata.ToolName != toolName {
		t.Errorf("Tool names do not match. Expected: %s, Actual: %s", toolName, metadata.ToolName)
	}

	if metadata.TotalSize != len(result) {
		t.Errorf("Total size does not match. Expected: %d, Actual: %d", len(result), metadata.TotalSize)
	}

	expectedLines := len(strings.Split(result, "\n"))
	if metadata.TotalLines != expectedLines {
		t.Errorf("The total number of rows does not match. Expected: %d, Actual: %d", expectedLines, metadata.TotalLines)
	}

	// Verify that the creation time is within a reasonable range
	now := time.Now()
	if metadata.CreatedAt.After(now) || metadata.CreatedAt.Before(now.Add(-time.Second)) {
		t.Errorf("Creation time is not within reasonable range: %v", metadata.CreatedAt)
	}
}

func TestFileResultStorage_GetResultPage(t *testing.T) {
	storage, tmpDir := setupTestStorage(t)
	defer cleanupTestStorage(t, tmpDir)

	executionID := "test_exec_004"
	toolName := "test_tool"
	// Create a result containing 10 rows
	lines := make([]string, 10)
	for i := 0; i < 10; i++ {
		lines[i] = fmt.Sprintf("Line %d", i+1)
	}
	result := strings.Join(lines, "\n")

	// Save results
	err := storage.SaveResult(executionID, toolName, result)
	if err != nil {
		t.Fatalf("Failed to save results: %v", err)
	}

	// Test the first page (3 lines per page)
	page, err := storage.GetResultPage(executionID, 1, 3)
	if err != nil {
		t.Fatalf("Failed to get first page: %v", err)
	}

	if page.Page != 1 {
		t.Errorf("Page numbers don't match. Expected: 1, Actual: %d", page.Page)
	}

	if page.Limit != 3 {
		t.Errorf("The number of rows per page does not match. Expected: 3, Actual: %d", page.Limit)
	}

	if page.TotalLines != 10 {
		t.Errorf("The total number of rows does not match. Expected: 10, Actual: %d", page.TotalLines)
	}

	if page.TotalPages != 4 {
		t.Errorf("The total number of pages does not match. Expected: 4, Actual: %d", page.TotalPages)
	}

	if len(page.Lines) != 3 {
		t.Errorf("The number of rows on the first page does not match. Expected: 3, Actual: %d", len(page.Lines))
	}

	if page.Lines[0] != "Line 1" {
		t.Errorf("The contents of the first line do not match. Expected: Line 1, Actual: %s", page.Lines[0])
	}

	// Test second page
	page2, err := storage.GetResultPage(executionID, 2, 3)
	if err != nil {
		t.Fatalf("Failed to get second page: %v", err)
	}

	if len(page2.Lines) != 3 {
		t.Errorf("The number of rows on the second page does not match. Expected: 3, Actual: %d", len(page2.Lines))
	}

	if page2.Lines[0] != "Line 4" {
		t.Errorf("The content of the first line of the second page does not match. Expected: Line 4, Actual: %s", page2.Lines[0])
	}

	// Test the last page (may be less than one page)
	page4, err := storage.GetResultPage(executionID, 4, 3)
	if err != nil {
		t.Fatalf("Failed to get the fourth page: %v", err)
	}

	if len(page4.Lines) != 1 {
		t.Errorf("The number of rows on the fourth page does not match. Expected: 1, Actual: %d", len(page4.Lines))
	}

	// Test for out-of-range page numbers (should return the last page)
	page5, err := storage.GetResultPage(executionID, 5, 3)
	if err != nil {
		t.Fatalf("Failed to get the fifth page: %v", err)
	}

	// Page numbers outside the range will be corrected to the last page, so the content of the last page should be returned
	if page5.Page != 4 {
		t.Errorf("Out-of-range page numbers should be corrected to the last page. Expected: 4, Actual: %d", page5.Page)
	}

	// The last page should only have 1 row
	if len(page5.Lines) != 1 {
		t.Errorf("The last page should only have 1 row. Actual: %d rows", len(page5.Lines))
	}
}

func TestFileResultStorage_SearchResult(t *testing.T) {
	storage, tmpDir := setupTestStorage(t)
	defer cleanupTestStorage(t, tmpDir)

	executionID := "test_exec_005"
	toolName := "test_tool"
	result := "Line 1: error occurred\nLine 2: success\nLine 3: error again\nLine 4: ok"

	// Save results
	err := storage.SaveResult(executionID, toolName, result)
	if err != nil {
		t.Fatalf("Failed to save results: %v", err)
	}

	// Search for lines containing "error" (simple string matching)
	matchedLines, err := storage.SearchResult(executionID, "error", false)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(matchedLines) != 2 {
		t.Errorf("The number of search results does not match. Expected: 2, Actual: %d", len(matchedLines))
	}

	// Verify search result content
	for i, line := range matchedLines {
		if !strings.Contains(line, "error") {
			t.Errorf("The search result row %d does not contain the keyword: %s", i+1, line)
		}
	}

	// Test search for non-existent keywords
	noMatch, err := storage.SearchResult(executionID, "nonexistent", false)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(noMatch) != 0 {
		t.Errorf("Searching for keywords that do not exist should return empty results. Actual: %d rows", len(noMatch))
	}

	// Test regular expression search
	regexMatched, err := storage.SearchResult(executionID, "error.*again", true)
	if err != nil {
		t.Fatalf("Regular search failed: %v", err)
	}

	if len(regexMatched) != 1 {
		t.Errorf("The number of regular search results does not match. Expected: 1, Actual: %d", len(regexMatched))
	}
}

func TestFileResultStorage_FilterResult(t *testing.T) {
	storage, tmpDir := setupTestStorage(t)
	defer cleanupTestStorage(t, tmpDir)

	executionID := "test_exec_006"
	toolName := "test_tool"
	result := "Line 1: warning message\nLine 2: info message\nLine 3: warning again\nLine 4: debug message"

	// Save results
	err := storage.SaveResult(executionID, toolName, result)
	if err != nil {
		t.Fatalf("Failed to save results: %v", err)
	}

	// Filter lines containing "warning" (simple string matching)
	filteredLines, err := storage.FilterResult(executionID, "warning", false)
	if err != nil {
		t.Fatalf("Filtering failed: %v", err)
	}

	if len(filteredLines) != 2 {
		t.Errorf("The number of filter results does not match. Expected: 2, Actual: %d", len(filteredLines))
	}

	// Verify filter result content
	for i, line := range filteredLines {
		if !strings.Contains(line, "warning") {
			t.Errorf("The %d row of filtered results does not contain the keyword: %s", i+1, line)
		}
	}
}

func TestFileResultStorage_DeleteResult(t *testing.T) {
	storage, tmpDir := setupTestStorage(t)
	defer cleanupTestStorage(t, tmpDir)

	executionID := "test_exec_007"
	toolName := "test_tool"
	result := "Test result"

	// Save results
	err := storage.SaveResult(executionID, toolName, result)
	if err != nil {
		t.Fatalf("Failed to save results: %v", err)
	}

	// Verify file exists
	resultPath := filepath.Join(tmpDir, executionID+".txt")
	metadataPath := filepath.Join(tmpDir, executionID+".meta.json")

	if _, err := os.Stat(resultPath); os.IsNotExist(err) {
		t.Fatal("Result file does not exist")
	}

	if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
		t.Fatal("Metadata file does not exist")
	}

	// Delete results
	err = storage.DeleteResult(executionID)
	if err != nil {
		t.Fatalf("Failed to delete result: %v", err)
	}

	// Verification file deleted
	if _, err := os.Stat(resultPath); !os.IsNotExist(err) {
		t.Fatal("The result file was not deleted")
	}

	if _, err := os.Stat(metadataPath); !os.IsNotExist(err) {
		t.Fatal("Metadata files were not deleted")
	}

	// Test deletion of non-existent execution ID (no error should be reported)
	err = storage.DeleteResult("nonexistent_id")
	if err != nil {
		t.Errorf("Deleting a non-existent execution ID should not result in an error: %v", err)
	}
}

func TestFileResultStorage_ConcurrentAccess(t *testing.T) {
	storage, tmpDir := setupTestStorage(t)
	defer cleanupTestStorage(t, tmpDir)

	// Save multiple results concurrently
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			executionID := fmt.Sprintf("test_exec_%d", id)
			toolName := "test_tool"
			result := fmt.Sprintf("Result %d\nLine 2\nLine 3", id)

			err := storage.SaveResult(executionID, toolName, result)
			if err != nil {
				t.Errorf("Concurrent save failed (ID: %s): %v", executionID, err)
			}

			// Concurrent reads
			_, err = storage.GetResult(executionID)
			if err != nil {
				t.Errorf("Concurrent read failed (ID: %s): %v", executionID, err)
			}

			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestFileResultStorage_LargeResult(t *testing.T) {
	storage, tmpDir := setupTestStorage(t)
	defer cleanupTestStorage(t, tmpDir)

	executionID := "test_exec_large"
	toolName := "test_tool"

	// Create large results (1000 rows)
	lines := make([]string, 1000)
	for i := 0; i < 1000; i++ {
		lines[i] = fmt.Sprintf("Line %d: This is a test line with some content", i+1)
	}
	result := strings.Join(lines, "\n")

	// Save big results
	err := storage.SaveResult(executionID, toolName, result)
	if err != nil {
		t.Fatalf("Failed to save large result: %v", err)
	}

	// Verify metadata
	metadata, err := storage.GetResultMetadata(executionID)
	if err != nil {
		t.Fatalf("Failed to get metadata: %v", err)
	}

	if metadata.TotalLines != 1000 {
		t.Errorf("The total number of rows does not match. Expected: 1000, Actual: %d", metadata.TotalLines)
	}

	// Test paging query for large results
	page, err := storage.GetResultPage(executionID, 1, 100)
	if err != nil {
		t.Fatalf("Failed to get first page: %v", err)
	}

	if page.TotalPages != 10 {
		t.Errorf("The total number of pages does not match. Expected: 10, Actual: %d", page.TotalPages)
	}

	if len(page.Lines) != 100 {
		t.Errorf("The number of rows on the first page does not match. Expected: 100, Actual: %d", len(page.Lines))
	}
}
