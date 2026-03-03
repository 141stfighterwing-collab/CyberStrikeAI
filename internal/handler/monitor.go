package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/security"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// MonitorHandler monitoring processor
type MonitorHandler struct {
	mcpServer      *mcp.Server
	externalMCPMgr *mcp.ExternalMCPManager
	executor       *security.Executor
	db             *database.DB
	logger         *zap.Logger
}

// NewMonitorHandler creates a new monitoring handler
func NewMonitorHandler(mcpServer *mcp.Server, executor *security.Executor, db *database.DB, logger *zap.Logger) *MonitorHandler {
	return &MonitorHandler{
		mcpServer:      mcpServer,
		externalMCPMgr: nil, // Will be set after creation
		executor:       executor,
		db:             db,
		logger:         logger,
	}
}

// SetExternalMCPManager sets the external MCP manager
func (h *MonitorHandler) SetExternalMCPManager(mgr *mcp.ExternalMCPManager) {
	h.externalMCPMgr = mgr
}

// MonitorResponse monitoring response
type MonitorResponse struct {
	Executions []*mcp.ToolExecution      `json:"executions"`
	Stats      map[string]*mcp.ToolStats `json:"stats"`
	Timestamp  time.Time                  `json:"timestamp"`
	Total      int                        `json:"total,omitempty"`
	Page       int                        `json:"page,omitempty"`
	PageSize   int                        `json:"page_size,omitempty"`
	TotalPages int                        `json:"total_pages,omitempty"`
}

// Monitor Get monitoring information
func (h *MonitorHandler) Monitor(c *gin.Context) {
	// Parse paging parameters
	page := 1
	pageSize := 20
	if pageStr := c.Query("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}
	if pageSizeStr := c.Query("page_size"); pageSizeStr != "" {
		if ps, err := strconv.Atoi(pageSizeStr); err == nil && ps > 0 && ps <= 100 {
			pageSize = ps
		}
	}

	// Parse status filter parameters
	status := c.Query("status")
	// Analysis tool filter parameters
	toolName := c.Query("tool")

	executions, total := h.loadExecutionsWithPagination(page, pageSize, status, toolName)
	stats := h.loadStats()

	totalPages := (total + pageSize - 1) / pageSize
	if totalPages == 0 {
		totalPages = 1
	}

	c.JSON(http.StatusOK, MonitorResponse{
		Executions: executions,
		Stats:      stats,
		Timestamp:  time.Now(),
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	})
}

func (h *MonitorHandler) loadExecutions() []*mcp.ToolExecution {
	executions, _ := h.loadExecutionsWithPagination(1, 1000, "", "")
	return executions
}

func (h *MonitorHandler) loadExecutionsWithPagination(page, pageSize int, status, toolName string) ([]*mcp.ToolExecution, int) {
	if h.db == nil {
		allExecutions := h.mcpServer.GetAllExecutions()
		// If status filter or tool filter is specified, filter first
		if status != "" || toolName != "" {
			filtered := make([]*mcp.ToolExecution, 0)
			for _, exec := range allExecutions {
				matchStatus := status == "" || exec.Status == status
				// Support partial matching (fuzzy search)
				matchTool := toolName == "" || strings.Contains(strings.ToLower(exec.ToolName), strings.ToLower(toolName))
				if matchStatus && matchTool {
					filtered = append(filtered, exec)
				}
			}
			allExecutions = filtered
		}
		total := len(allExecutions)
		offset := (page - 1) * pageSize
		end := offset + pageSize
		if end > total {
			end = total
		}
		if offset >= total {
			return []*mcp.ToolExecution{}, total
		}
		return allExecutions[offset:end], total
	}

	offset := (page - 1) * pageSize
	executions, err := h.db.LoadToolExecutionsWithPagination(offset, pageSize, status, toolName)
	if err != nil {
		h.logger.Warn("Failed to load execution records from the database and fell back to memory data", zap.Error(err))
		allExecutions := h.mcpServer.GetAllExecutions()
		// If status filter or tool filter is specified, filter first
		if status != "" || toolName != "" {
			filtered := make([]*mcp.ToolExecution, 0)
			for _, exec := range allExecutions {
				matchStatus := status == "" || exec.Status == status
				// Support partial matching (fuzzy search)
				matchTool := toolName == "" || strings.Contains(strings.ToLower(exec.ToolName), strings.ToLower(toolName))
				if matchStatus && matchTool {
					filtered = append(filtered, exec)
				}
			}
			allExecutions = filtered
		}
		total := len(allExecutions)
		offset := (page - 1) * pageSize
		end := offset + pageSize
		if end > total {
			end = total
		}
		if offset >= total {
			return []*mcp.ToolExecution{}, total
		}
		return allExecutions[offset:end], total
	}

	// Get the total number (considering status filtering and tool filtering)
	total, err := h.db.CountToolExecutions(status, toolName)
	if err != nil {
		h.logger.Warn("Failed to obtain the total number of execution records", zap.Error(err))
		// Fallback: Use estimated number of records loaded
		total = offset + len(executions)
		if len(executions) == pageSize {
			total = offset + len(executions) + 1
		}
	}

	return executions, total
}

func (h *MonitorHandler) loadStats() map[string]*mcp.ToolStats {
	// Merge statistics of internal MCP server and external MCP manager
	stats := make(map[string]*mcp.ToolStats)

	// Load statistics for the internal MCP server
	if h.db == nil {
		internalStats := h.mcpServer.GetStats()
		for k, v := range internalStats {
			stats[k] = v
		}
	} else {
		dbStats, err := h.db.LoadToolStats()
		if err != nil {
			h.logger.Warn("Failed to load statistics from database, falling back to memory data", zap.Error(err))
			internalStats := h.mcpServer.GetStats()
			for k, v := range internalStats {
				stats[k] = v
			}
		} else {
			for k, v := range dbStats {
				stats[k] = v
			}
		}
	}

	// Merge statistics from external MCP managers
	if h.externalMCPMgr != nil {
		externalStats := h.externalMCPMgr.GetToolStats()
		for k, v := range externalStats {
			// If existing, merge statistics
			if existing, exists := stats[k]; exists {
				existing.TotalCalls += v.TotalCalls
				existing.SuccessCalls += v.SuccessCalls
				existing.FailedCalls += v.FailedCalls
				// Use the latest call time
				if v.LastCallTime != nil && (existing.LastCallTime == nil || v.LastCallTime.After(*existing.LastCallTime)) {
					existing.LastCallTime = v.LastCallTime
				}
			} else {
				stats[k] = v
			}
		}
	}

	return stats
}


// GetExecution gets specific execution records
func (h *MonitorHandler) GetExecution(c *gin.Context) {
	id := c.Param("id")

	// First search from the internal MCP server
	exec, exists := h.mcpServer.GetExecution(id)
	if exists {
		c.JSON(http.StatusOK, exec)
		return
	}

	// If not found, try looking from an external MCP manager
	if h.externalMCPMgr != nil {
		exec, exists = h.externalMCPMgr.GetExecution(id)
		if exists {
			c.JSON(http.StatusOK, exec)
			return
		}
	}

	// If neither is found, try to find it from the database (if using database storage)
	if h.db != nil {
		exec, err := h.db.GetToolExecution(id)
		if err == nil && exec != nil {
			c.JSON(http.StatusOK, exec)
			return
		}
	}

	c.JSON(http.StatusNotFound, gin.H{"error": "Execution record not found"})
}

// GetStats Get statistics
func (h *MonitorHandler) GetStats(c *gin.Context) {
	stats := h.loadStats()
	c.JSON(http.StatusOK, stats)
}

// DeleteExecution deletes execution records
func (h *MonitorHandler) DeleteExecution(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Execution record ID cannot be empty"})
		return
	}

	// If using a database, first obtain execution record information, then delete and update statistics
	if h.db != nil {
		// First obtain the execution record information (used to update statistics)
		exec, err := h.db.GetToolExecution(id)
		if err != nil {
			// If the record cannot be found, it may have been deleted and return success directly.
			h.logger.Warn("The execution record does not exist and may have been deleted.", zap.String("executionId", id), zap.Error(err))
			c.JSON(http.StatusOK, gin.H{"message": "The execution record does not exist or has been deleted"})
			return
		}

		// Delete execution record
		err = h.db.DeleteToolExecution(id)
		if err != nil {
			h.logger.Error("Failed to delete execution record", zap.Error(err), zap.String("executionId", id))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete execution record:" + err.Error()})
			return
		}

		// Update statistics (decrement the corresponding count)
		totalCalls := 1
		successCalls := 0
		failedCalls := 0
		if exec.Status == "failed" {
			failedCalls = 1
		} else if exec.Status == "completed" {
			successCalls = 1
		}

		if exec.ToolName != "" {
			if err := h.db.DecreaseToolStats(exec.ToolName, totalCalls, successCalls, failedCalls); err != nil {
				h.logger.Warn("Failed to update statistics", zap.Error(err), zap.String("toolName", exec.ToolName))
				// No error is returned because the record has been deleted successfully
			}
		}

		h.logger.Info("Execution records have been deleted from the database", zap.String("executionId", id), zap.String("toolName", exec.ToolName))
		c.JSON(http.StatusOK, gin.H{"message": "Execution record deleted"})
		return
	}

	// If the database is not used, try removing it from memory (internal MCP server)
	// Note: The records in memory may have been cleared, so only logs are recorded here.
	h.logger.Info("Try to delete the execution record in memory", zap.String("executionId", id))
	c.JSON(http.StatusOK, gin.H{"message": "Execution records deleted (if present)"})
}

// DeleteExecutions deletes execution records in batches
func (h *MonitorHandler) DeleteExecutions(c *gin.Context) {
	var request struct {
		IDs []string `json:"ids"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request parameters:" + err.Error()})
		return
	}

	if len(request.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "The execution record ID list cannot be empty"})
		return
	}

	// If using a database, first obtain execution record information, then delete and update statistics
	if h.db != nil {
		// First obtain the execution record information (used to update statistics)
		executions, err := h.db.GetToolExecutionsByIds(request.IDs)
		if err != nil {
			h.logger.Error("Failed to obtain execution record", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to obtain execution record:" + err.Error()})
			return
		}

		// Statistics of the quantity to be reduced grouped by tool name
		toolStats := make(map[string]struct {
			totalCalls   int
			successCalls int
			failedCalls  int
		})

		for _, exec := range executions {
			if exec.ToolName == "" {
				continue
			}

			stats := toolStats[exec.ToolName]
			stats.totalCalls++
			if exec.Status == "failed" {
				stats.failedCalls++
			} else if exec.Status == "completed" {
				stats.successCalls++
			}
			toolStats[exec.ToolName] = stats
		}

		// Delete execution records in batches
		err = h.db.DeleteToolExecutions(request.IDs)
		if err != nil {
			h.logger.Error("Batch deletion of execution records failed", zap.Error(err), zap.Int("count", len(request.IDs)))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Batch deletion of execution records failed:" + err.Error()})
			return
		}

		// Update statistics (decrement the corresponding count)
		for toolName, stats := range toolStats {
			if err := h.db.DecreaseToolStats(toolName, stats.totalCalls, stats.successCalls, stats.failedCalls); err != nil {
				h.logger.Warn("Failed to update statistics", zap.Error(err), zap.String("toolName", toolName))
				// No error is returned because the record has been deleted successfully
			}
		}

		h.logger.Info("Batch deletion of execution records successful", zap.Int("count", len(request.IDs)))
		c.JSON(http.StatusOK, gin.H{"message": "Execution records successfully deleted", "deleted": len(executions)})
		return
	}

	// If the database is not used, try removing it from memory (internal MCP server)
	// Note: The records in memory may have been cleared, so only logs are recorded here.
	h.logger.Info("Try to delete execution records in memory in batches", zap.Int("count", len(request.IDs)))
	c.JSON(http.StatusOK, gin.H{"message": "Execution records deleted (if present)"})
}


