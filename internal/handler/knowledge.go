package handler

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/knowledge"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// KnowledgeHandler knowledge base handler
type KnowledgeHandler struct {
	manager   *knowledge.Manager
	retriever *knowledge.Retriever
	indexer   *knowledge.Indexer
	db        *database.DB
	logger    *zap.Logger
}

// NewKnowledgeHandler creates a new knowledge base handler
func NewKnowledgeHandler(
	manager *knowledge.Manager,
	retriever *knowledge.Retriever,
	indexer *knowledge.Indexer,
	db *database.DB,
	logger *zap.Logger,
) *KnowledgeHandler {
	return &KnowledgeHandler{
		manager:   manager,
		retriever: retriever,
		indexer:   indexer,
		db:        db,
		logger:    logger,
	}
}

// GetCategories Get all categories
func (h *KnowledgeHandler) GetCategories(c *gin.Context) {
	categories, err := h.manager.GetCategories()
	if err != nil {
		h.logger.Error("Failed to obtain classification", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"categories": categories})
}

// GetItems Gets a list of knowledge items (supports paging by category and keyword search, and does not return complete content by default)
func (h *KnowledgeHandler) GetItems(c *gin.Context) {
	category := c.Query("category")
	searchKeyword := c.Query("search") // Search keywords

	// If search keywords are provided, perform a keyword search (search in all data)
	if searchKeyword != "" {
		items, err := h.manager.SearchItemsByKeyword(searchKeyword, category)
		if err != nil {
			h.logger.Error("Search for knowledge items failed", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Group results by category
		groupedByCategory := make(map[string][]*knowledge.KnowledgeItemSummary)
		for _, item := range items {
			cat := item.Category
			if cat == "" {
				cat = "Uncategorized"
			}
			groupedByCategory[cat] = append(groupedByCategory[cat], item)
		}

		// Convert to CategoryWithItems format
		categoriesWithItems := make([]*knowledge.CategoryWithItems, 0, len(groupedByCategory))
		for cat, catItems := range groupedByCategory {
			categoriesWithItems = append(categoriesWithItems, &knowledge.CategoryWithItems{
				Category:  cat,
				ItemCount: len(catItems),
				Items:     catItems,
			})
		}

		// Sort by category name
		for i := 0; i < len(categoriesWithItems)-1; i++ {
			for j := i + 1; j < len(categoriesWithItems); j++ {
				if categoriesWithItems[i].Category > categoriesWithItems[j].Category {
					categoriesWithItems[i], categoriesWithItems[j] = categoriesWithItems[j], categoriesWithItems[i]
				}
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"categories": categoriesWithItems,
			"total":      len(categoriesWithItems),
			"search":     searchKeyword,
			"is_search":  true,
		})
		return
	}

	// Paging mode: categoryPage=true means paging by category, otherwise paging by item (backward compatibility)
	categoryPageMode := c.Query("categoryPage") != "false" // Use category paging by default

	// Paging parameters
	limit := 50 // The default is 50 items per page (the number of categories when paging by categories, the number of items when paging by items)
	offset := 0
	if limitStr := c.Query("limit"); limitStr != "" {
		if parsed, err := parseInt(limitStr); err == nil && parsed > 0 && parsed <= 500 {
			limit = parsed
		}
	}
	if offsetStr := c.Query("offset"); offsetStr != "" {
		if parsed, err := parseInt(offsetStr); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	// If the category parameter is specified and the category paging mode is used, only the category will be returned.
	if category != "" && categoryPageMode {
		// Single category mode: Return all knowledge items of this category (no paging)
		items, total, err := h.manager.GetItemsSummary(category, 0, 0)
		if err != nil {
			h.logger.Error("Failed to obtain knowledge item", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Packed into a classification structure
		categoriesWithItems := []*knowledge.CategoryWithItems{
			{
				Category:  category,
				ItemCount: total,
				Items:     items,
			},
		}

		c.JSON(http.StatusOK, gin.H{
			"categories": categoriesWithItems,
			"total":      1, // There is only one category
			"limit":      limit,
			"offset":     offset,
		})
		return
	}

	if categoryPageMode {
		// Paging mode by category (default)
		// Limit indicates the number of categories per page, 5-10 categories are recommended
		if limit <= 0 || limit > 100 {
			limit = 10 // Default is 10 categories per page
		}

		categoriesWithItems, totalCategories, err := h.manager.GetCategoriesWithItems(limit, offset)
		if err != nil {
			h.logger.Error("Failed to obtain classification knowledge items", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"categories": categoriesWithItems,
			"total":      totalCategories,
			"limit":      limit,
			"offset":     offset,
		})
		return
	}

	// Paging by item mode (backwards compatible)
	// Whether to include complete content (default false, only a summary is returned)
	includeContent := c.Query("includeContent") == "true"

	if includeContent {
		// Return full content (backwards compatible)
		items, err := h.manager.GetItemsWithOptions(category, limit, offset, true)
		if err != nil {
			h.logger.Error("Failed to obtain knowledge item", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Get total
		total, err := h.manager.GetItemsCount(category)
		if err != nil {
			h.logger.Warn("Failed to obtain the total number of knowledge items", zap.Error(err))
			total = len(items)
		}

		c.JSON(http.StatusOK, gin.H{
			"items":  items,
			"total":  total,
			"limit":  limit,
			"offset": offset,
		})
	} else {
		// Return summary (does not contain complete content, recommended method)
		items, total, err := h.manager.GetItemsSummary(category, limit, offset)
		if err != nil {
			h.logger.Error("Failed to obtain knowledge item", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"items":  items,
			"total":  total,
			"limit":  limit,
			"offset": offset,
		})
	}
}

// GetItem Gets a single knowledge item
func (h *KnowledgeHandler) GetItem(c *gin.Context) {
	id := c.Param("id")

	item, err := h.manager.GetItem(id)
	if err != nil {
		h.logger.Error("Failed to obtain knowledge item", zap.Error(err))
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, item)
}

// CreateItem creates a knowledge item
func (h *KnowledgeHandler) CreateItem(c *gin.Context) {
	var req struct {
		Category string `json:"category" binding:"required"`
		Title    string `json:"title" binding:"required"`
		Content  string `json:"content" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	item, err := h.manager.CreateItem(req.Category, req.Title, req.Content)
	if err != nil {
		h.logger.Error("Failed to create knowledge item", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Asynchronous indexing
	go func() {
		ctx := context.Background()
		if err := h.indexer.IndexItem(ctx, item.ID); err != nil {
			h.logger.Warn("Indexing knowledge items failed", zap.String("itemId", item.ID), zap.Error(err))
		}
	}()

	c.JSON(http.StatusOK, item)
}

// UpdateItem updates knowledge items
func (h *KnowledgeHandler) UpdateItem(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		Category string `json:"category" binding:"required"`
		Title    string `json:"title" binding:"required"`
		Content  string `json:"content" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	item, err := h.manager.UpdateItem(id, req.Category, req.Title, req.Content)
	if err != nil {
		h.logger.Error("Failed to update knowledge item", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Asynchronous reindexing
	go func() {
		ctx := context.Background()
		if err := h.indexer.IndexItem(ctx, item.ID); err != nil {
			h.logger.Warn("Reindexing knowledge items failed", zap.String("itemId", item.ID), zap.Error(err))
		}
	}()

	c.JSON(http.StatusOK, item)
}

// DeleteItem deletes knowledge items
func (h *KnowledgeHandler) DeleteItem(c *gin.Context) {
	id := c.Param("id")

	if err := h.manager.DeleteItem(id); err != nil {
		h.logger.Error("Failed to delete knowledge item", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Delete successfully"})
}

// RebuildIndex rebuild index
func (h *KnowledgeHandler) RebuildIndex(c *gin.Context) {
	// Rebuild index asynchronously
	go func() {
		ctx := context.Background()
		if err := h.indexer.RebuildIndex(ctx); err != nil {
			h.logger.Error("Rebuilding index failed", zap.Error(err))
		}
	}()

	c.JSON(http.StatusOK, gin.H{"message": "Index rebuild has started and will occur in the background"})
}

// ScanKnowledgeBase Scan knowledge base
func (h *KnowledgeHandler) ScanKnowledgeBase(c *gin.Context) {
	itemsToIndex, err := h.manager.ScanKnowledgeBase()
	if err != nil {
		h.logger.Error("Scanning knowledge base failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if len(itemsToIndex) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": "Scan complete, no new or updated items need to be indexed"})
		return
	}

	// Asynchronously index newly added or updated items (incremental indexing)
	go func() {
		ctx := context.Background()
		h.logger.Info("Start incremental indexing", zap.Int("count", len(itemsToIndex)))
		failedCount := 0
		consecutiveFailures := 0
		var firstFailureItemID string
		var firstFailureError error

		for i, itemID := range itemsToIndex {
			if err := h.indexer.IndexItem(ctx, itemID); err != nil {
				failedCount++
				consecutiveFailures++

				// Only log verbose on first failure
				if consecutiveFailures == 1 {
					firstFailureItemID = itemID
					firstFailureError = err
					h.logger.Warn("Indexing knowledge items failed",
						zap.String("itemId", itemID),
						zap.Int("totalItems", len(itemsToIndex)),
						zap.Error(err),
					)
				}

				// If it fails 2 times in a row, stop incremental indexing immediately
				if consecutiveFailures >= 2 {
					h.logger.Error("There are too many consecutive indexing failures. Stop incremental indexing immediately.",
						zap.Int("consecutiveFailures", consecutiveFailures),
						zap.Int("totalItems", len(itemsToIndex)),
						zap.Int("processedItems", i+1),
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

			// Reduce progress log frequency
			if (i+1)%10 == 0 || i+1 == len(itemsToIndex) {
				h.logger.Info("Index progress", zap.Int("current", i+1), zap.Int("total", len(itemsToIndex)), zap.Int("failed", failedCount))
			}
		}
		h.logger.Info("Incremental indexing completed", zap.Int("totalItems", len(itemsToIndex)), zap.Int("failedCount", failedCount))
	}()

	c.JSON(http.StatusOK, gin.H{
		"message":        fmt.Sprintf("Scan completed, start indexing %d newly added or updated knowledge items", len(itemsToIndex)),
		"items_to_index": len(itemsToIndex),
	})
}

// GetRetrievalLogs Get retrieval logs
func (h *KnowledgeHandler) GetRetrievalLogs(c *gin.Context) {
	conversationID := c.Query("conversationId")
	messageID := c.Query("messageId")
	limit := 50 // Default 50 items

	if limitStr := c.Query("limit"); limitStr != "" {
		if parsed, err := parseInt(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	logs, err := h.manager.GetRetrievalLogs(conversationID, messageID, limit)
	if err != nil {
		h.logger.Error("Failed to obtain retrieval log", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"logs": logs})
}

// DeleteRetrievalLog deletes the retrieval log
func (h *KnowledgeHandler) DeleteRetrievalLog(c *gin.Context) {
	id := c.Param("id")

	if err := h.manager.DeleteRetrievalLog(id); err != nil {
		h.logger.Error("Failed to delete retrieval log", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Delete successfully"})
}

// GetIndexStatus gets index status
func (h *KnowledgeHandler) GetIndexStatus(c *gin.Context) {
	status, err := h.manager.GetIndexStatus()
	if err != nil {
		h.logger.Error("Failed to get index status", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Get indexer error information
	if h.indexer != nil {
		lastError, lastErrorTime := h.indexer.GetLastError()
		if lastError != "" {
			// If the error occurred recently (within 5 minutes), an error message is returned
			if time.Since(lastErrorTime) < 5*time.Minute {
				status["last_error"] = lastError
				status["last_error_time"] = lastErrorTime.Format(time.RFC3339)
			}
		}
	}

	c.JSON(http.StatusOK, status)
}

// Search Search knowledge base (used for API calls, Agent uses Retriever internally)
func (h *KnowledgeHandler) Search(c *gin.Context) {
	var req knowledge.SearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	results, err := h.retriever.Search(c.Request.Context(), &req)
	if err != nil {
		h.logger.Error("Search knowledge base failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"results": results})
}

// GetStats Get knowledge base statistics
func (h *KnowledgeHandler) GetStats(c *gin.Context) {
	totalCategories, totalItems, err := h.manager.GetStats()
	if err != nil {
		h.logger.Error("Failed to obtain knowledge base statistics", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"enabled":          true,
		"total_categories": totalCategories,
		"total_items":      totalItems,
	})
}

// Helper functions: parsing integers
func parseInt(s string) (int, error) {
	var result int
	_, err := fmt.Sscanf(s, "%d", &result)
	return result, err
}
