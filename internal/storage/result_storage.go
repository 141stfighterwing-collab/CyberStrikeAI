package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// ResultStorage result storage interface
type ResultStorage interface {
	// SaveResult saves tool execution results
	SaveResult(executionID string, toolName string, result string) error

	// GetResult Get the complete result
	GetResult(executionID string) (string, error)

	// GetResultPage Gets results by page
	GetResultPage(executionID string, page int, limit int) (*ResultPage, error)

	// SearchResult Search results
	// UseRegex: If true, use keyword as a regular expression; if false, use simple string inclusion matching
	SearchResult(executionID string, keyword string, useRegex bool) ([]string, error)

	// FilterResult filter results
	// UseRegex: If true, use filter as a regular expression; if false, use a simple string inclusion match
	FilterResult(executionID string, filter string, useRegex bool) ([]string, error)

	// GetResultMetadata Gets result meta information
	GetResultMetadata(executionID string) (*ResultMetadata, error)

	// GetResultPath gets the result file path
	GetResultPath(executionID string) string

	// DeleteResult deletes the result
	DeleteResult(executionID string) error
}

// ResultPage paging results
type ResultPage struct {
	Lines      []string `json:"lines"`
	Page       int      `json:"page"`
	Limit      int      `json:"limit"`
	TotalLines int      `json:"total_lines"`
	TotalPages int      `json:"total_pages"`
}

// ResultMetadata result meta information
type ResultMetadata struct {
	ExecutionID string    `json:"execution_id"`
	ToolName    string    `json:"tool_name"`
	TotalSize   int       `json:"total_size"`
	TotalLines  int       `json:"total_lines"`
	CreatedAt   time.Time `json:"created_at"`
}

// FileResultStorage file-based result storage implementation
type FileResultStorage struct {
	baseDir string
	logger  *zap.Logger
	mu      sync.RWMutex
}

// NewFileResultStorage creates a new file result storage
func NewFileResultStorage(baseDir string, logger *zap.Logger) (*FileResultStorage, error) {
	// Make sure the directory exists
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("Failed to create storage directory: %w", err)
	}

	return &FileResultStorage{
		baseDir: baseDir,
		logger:  logger,
	}, nil
}

// GetResultPath gets the result file path
func (s *FileResultStorage) getResultPath(executionID string) string {
	return filepath.Join(s.baseDir, executionID+".txt")
}

// GetMetadataPath gets the metadata file path
func (s *FileResultStorage) getMetadataPath(executionID string) string {
	return filepath.Join(s.baseDir, executionID+".meta.json")
}

// SaveResult saves tool execution results
func (s *FileResultStorage) SaveResult(executionID string, toolName string, result string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Save results file
	resultPath := s.getResultPath(executionID)
	if err := os.WriteFile(resultPath, []byte(result), 0644); err != nil {
		return fmt.Errorf("Failed to save results file: %w", err)
	}

	// Calculate statistics
	lines := strings.Split(result, "\n")
	metadata := &ResultMetadata{
		ExecutionID: executionID,
		ToolName:    toolName,
		TotalSize:   len(result),
		TotalLines:  len(lines),
		CreatedAt:   time.Now(),
	}

	// Save metadata
	metadataPath := s.getMetadataPath(executionID)
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("Failed to serialize metadata: %w", err)
	}

	if err := os.WriteFile(metadataPath, metadataJSON, 0644); err != nil {
		return fmt.Errorf("Failed to save metadata file: %w", err)
	}

	s.logger.Info("Save tool execution results",
		zap.String("executionID", executionID),
		zap.String("toolName", toolName),
		zap.Int("size", len(result)),
		zap.Int("lines", len(lines)),
	)

	return nil
}

// GetResult Get the complete result
func (s *FileResultStorage) GetResult(executionID string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	resultPath := s.getResultPath(executionID)
	data, err := os.ReadFile(resultPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("Result does not exist: %s", executionID)
		}
		return "", fmt.Errorf("Failed to read result file: %w", err)
	}

	return string(data), nil
}

// GetResultMetadata Gets result meta information
func (s *FileResultStorage) GetResultMetadata(executionID string) (*ResultMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	metadataPath := s.getMetadataPath(executionID)
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("Result does not exist: %s", executionID)
		}
		return nil, fmt.Errorf("Failed to read metadata file: %w", err)
	}

	var metadata ResultMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("Failed to parse metadata: %w", err)
	}

	return &metadata, nil
}

// GetResultPage Gets results by page
func (s *FileResultStorage) GetResultPage(executionID string, page int, limit int) (*ResultPage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Get full results
	result, err := s.GetResult(executionID)
	if err != nil {
		return nil, err
	}

	// Split into rows
	lines := strings.Split(result, "\n")
	totalLines := len(lines)

	// Calculate pagination
	totalPages := (totalLines + limit - 1) / limit
	if page < 1 {
		page = 1
	}
	if page > totalPages && totalPages > 0 {
		page = totalPages
	}

	// Calculate start and end index
	start := (page - 1) * limit
	end := start + limit
	if end > totalLines {
		end = totalLines
	}

	// Extract rows from specified page
	var pageLines []string
	if start < totalLines {
		pageLines = lines[start:end]
	} else {
		pageLines = []string{}
	}

	return &ResultPage{
		Lines:      pageLines,
		Page:       page,
		Limit:      limit,
		TotalLines: totalLines,
		TotalPages: totalPages,
	}, nil
}

// SearchResult Search results
func (s *FileResultStorage) SearchResult(executionID string, keyword string, useRegex bool) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Get full results
	result, err := s.GetResult(executionID)
	if err != nil {
		return nil, err
	}

	// If you use regular expressions, compile the regular expression first
	var regex *regexp.Regexp
	if useRegex {
		compiledRegex, err := regexp.Compile(keyword)
		if err != nil {
			return nil, fmt.Errorf("Invalid regular expression: %w", err)
		}
		regex = compiledRegex
	}

	// Split into lines and search
	lines := strings.Split(result, "\n")
	var matchedLines []string

	for _, line := range lines {
		var matched bool
		if useRegex {
			matched = regex.MatchString(line)
		} else {
			matched = strings.Contains(line, keyword)
		}

		if matched {
			matchedLines = append(matchedLines, line)
		}
	}

	return matchedLines, nil
}

// FilterResult filter results
func (s *FileResultStorage) FilterResult(executionID string, filter string, useRegex bool) ([]string, error) {
	// The filtering and search logic are the same, they both find rows containing keywords
	return s.SearchResult(executionID, filter, useRegex)
}

// GetResultPath gets the result file path
func (s *FileResultStorage) GetResultPath(executionID string) string {
	return s.getResultPath(executionID)
}

// DeleteResult deletes the result
func (s *FileResultStorage) DeleteResult(executionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	resultPath := s.getResultPath(executionID)
	metadataPath := s.getMetadataPath(executionID)

	// Delete results file
	if err := os.Remove(resultPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("Failed to delete results file: %w", err)
	}

	// Delete metadata files
	if err := os.Remove(metadataPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("Failed to delete metadata file: %w", err)
	}

	s.logger.Info("Delete tool execution results",
		zap.String("executionID", executionID),
	)

	return nil
}
