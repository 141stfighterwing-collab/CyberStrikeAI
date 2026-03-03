package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Indexer indexer, responsible for dividing knowledge items into chunks and quantifying them
type Indexer struct {
	db        *sql.DB
	embedder  *Embedder
	logger    *zap.Logger
	chunkSize int // Maximum number of tokens per block (estimate)
	overlap   int // Number of overlapping tokens between blocks
	
	// Error tracking
	mu           sync.RWMutex
	lastError    string    // Latest error message
	lastErrorTime time.Time // Last error time
	errorCount   int       // Continuous error count
}

// NewIndexer creates a new indexer
func NewIndexer(db *sql.DB, embedder *Embedder, logger *zap.Logger) *Indexer {
	return &Indexer{
		db:        db,
		embedder:  embedder,
		logger:    logger,
		chunkSize: 512, // Default 512 tokens
		overlap:   50,  // Default 50 tokens overlap
	}
}

// ChunkText chunks text (supports overlap)
func (idx *Indexer) ChunkText(text string) []string {
	// Split by Markdown title
	chunks := idx.splitByMarkdownHeaders(text)

	// If the block is too big, split it further
	result := make([]string, 0)
	for _, chunk := range chunks {
		if idx.estimateTokens(chunk) <= idx.chunkSize {
			result = append(result, chunk)
		} else {
			// Split by paragraph
			subChunks := idx.splitByParagraphs(chunk)
			for _, subChunk := range subChunks {
				if idx.estimateTokens(subChunk) <= idx.chunkSize {
					result = append(result, subChunk)
				} else {
					// Split by sentence (supports overlap)
					chunksWithOverlap := idx.splitBySentencesWithOverlap(subChunk)
					result = append(result, chunksWithOverlap...)
				}
			}
		}
	}

	return result
}

// SplitByMarkdownHeaders split by Markdown headers
func (idx *Indexer) splitByMarkdownHeaders(text string) []string {
	// Match Markdown titles (# ## ### etc.)
	headerRegex := regexp.MustCompile(`(?m)^#{1,6}\s+.+$`)

	// Find all title positions
	matches := headerRegex.FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return []string{text}
	}

	chunks := make([]string, 0)
	lastPos := 0

	for _, match := range matches {
		start := match[0]
		if start > lastPos {
			chunks = append(chunks, strings.TrimSpace(text[lastPos:start]))
		}
		lastPos = start
	}

	// Add last part
	if lastPos < len(text) {
		chunks = append(chunks, strings.TrimSpace(text[lastPos:]))
	}

	// Filter empty blocks
	result := make([]string, 0)
	for _, chunk := range chunks {
		if strings.TrimSpace(chunk) != "" {
			result = append(result, chunk)
		}
	}

	if len(result) == 0 {
		return []string{text}
	}

	return result
}

// SplitByParagraphs split by paragraph
func (idx *Indexer) splitByParagraphs(text string) []string {
	paragraphs := strings.Split(text, "\n\n")
	result := make([]string, 0)
	for _, p := range paragraphs {
		if strings.TrimSpace(p) != "" {
			result = append(result, strings.TrimSpace(p))
		}
	}
	return result
}

// SplitBySentences splits by sentence (used internally, does not contain overlapping logic)
func (idx *Indexer) splitBySentences(text string) []string {
	// Simple sentence segmentation (by periods, question marks, exclamation points)
	sentenceRegex := regexp.MustCompile(`[.!?]+\s+`)
	sentences := sentenceRegex.Split(text, -1)
	result := make([]string, 0)
	for _, s := range sentences {
		if strings.TrimSpace(s) != "" {
			result = append(result, strings.TrimSpace(s))
		}
	}
	return result
}

// SplitBySentencesWithOverlap splits by sentences and applies overlap strategy
func (idx *Indexer) splitBySentencesWithOverlap(text string) []string {
	if idx.overlap <= 0 {
		// If there is no overlap, use simple split
		return idx.splitBySentencesSimple(text)
	}

	sentences := idx.splitBySentences(text)
	if len(sentences) == 0 {
		return []string{}
	}

	result := make([]string, 0)
	currentChunk := ""

	for _, sentence := range sentences {
		testChunk := currentChunk
		if testChunk != "" {
			testChunk += "\n"
		}
		testChunk += sentence

		testTokens := idx.estimateTokens(testChunk)

		if testTokens > idx.chunkSize && currentChunk != "" {
			// Current block has reached size limit, save it
			result = append(result, currentChunk)

			// Extract the overlapping portion from the end of the current block
			overlapText := idx.extractLastTokens(currentChunk, idx.overlap)
			if overlapText != "" {
				// If there is overlapping content, use it as the start of the next block
				currentChunk = overlapText + "\n" + sentence
			} else {
				// If sufficient overlapping content cannot be extracted, the current sentence is used directly.
				currentChunk = sentence
			}
		} else {
			currentChunk = testChunk
		}
	}

	// Add last block
	if strings.TrimSpace(currentChunk) != "" {
		result = append(result, currentChunk)
	}

	// Filter empty blocks
	filtered := make([]string, 0)
	for _, chunk := range result {
		if strings.TrimSpace(chunk) != "" {
			filtered = append(filtered, chunk)
		}
	}

	return filtered
}

// SplitBySentencesSimple Split by sentences (simple version, no overlap)
func (idx *Indexer) splitBySentencesSimple(text string) []string {
	sentences := idx.splitBySentences(text)
	result := make([]string, 0)
	currentChunk := ""

	for _, sentence := range sentences {
		testChunk := currentChunk
		if testChunk != "" {
			testChunk += "\n"
		}
		testChunk += sentence

		if idx.estimateTokens(testChunk) > idx.chunkSize && currentChunk != "" {
			result = append(result, currentChunk)
			currentChunk = sentence
		} else {
			currentChunk = testChunk
		}
	}
	if currentChunk != "" {
		result = append(result, currentChunk)
	}

	return result
}

// ExtractLastTokens extracts the specified number of tokens from the end of the text
func (idx *Indexer) extractLastTokens(text string, tokenCount int) string {
	if tokenCount <= 0 || text == "" {
		return ""
	}

	// Estimated number of characters (1 token ≈ 4 characters)
	charCount := tokenCount * 4
	runes := []rune(text)

	if len(runes) <= charCount {
		return text
	}

	// Extract a specified number of characters from the end
	// Try to truncate at sentence boundaries and avoid truncation in the middle of sentences
	startPos := len(runes) - charCount
	extracted := string(runes[startPos:])

	// Try to find the first sentence boundary (space after period, question mark, exclamation mark)
	sentenceBoundary := regexp.MustCompile(`[.!?]+\s+`)
	matches := sentenceBoundary.FindStringIndex(extracted)
	if len(matches) > 0 && matches[0] > 0 {
		// Truncate at sentence boundaries, leaving complete sentences
		extracted = extracted[matches[0]:]
	}

	return strings.TrimSpace(extracted)
}

// EstimateTokens estimate the number of tokens (simple estimate: 1 token ≈ 4 characters)
func (idx *Indexer) estimateTokens(text string) int {
	return len([]rune(text)) / 4
}

// IndexItem index knowledge item (blocked and quantized)
func (idx *Indexer) IndexItem(ctx context.Context, itemID string) error {
	// Get knowledge items (including category and title, used for vectorization)
	var content, category, title string
	err := idx.db.QueryRow("SELECT content, category, title FROM knowledge_base_items WHERE id = ?", itemID).Scan(&content, &category, &title)
	if err != nil {
		return fmt.Errorf("Failed to obtain knowledge item: %w", err)
	}

	// Delete the old vector (already cleared in RebuildIndex, retained here for compatibility when calling IndexItem separately)
	_, err = idx.db.Exec("DELETE FROM knowledge_embeddings WHERE item_id = ?", itemID)
	if err != nil {
		return fmt.Errorf("Failed to delete old vector: %w", err)
	}

	// Chunking
	chunks := idx.ChunkText(content)
	idx.logger.Info("Knowledge items are completed in chunks", zap.String("itemId", itemID), zap.Int("chunks", len(chunks)))

	// Track errors for this knowledge item
	itemErrorCount := 0
	var firstError error
	firstErrorChunkIndex := -1
	
	// Vectorize each block (contain category and title information so that the risk type can be matched during vector retrieval)
	for i, chunk := range chunks {
		// Include category and title information into vectorized text
		// Format: "[Risk type: {category}] [Title: {title}]\n{chunk content}"
		// In this way, vector embedding will contain risk type information, and vector similarity can help match even if SQL filtering fails.
		textForEmbedding := fmt.Sprintf("[Risk Type: %s] [Title: %s]\n%s", category, title, chunk)

		embedding, err := idx.embedder.EmbedText(ctx, textForEmbedding)
		if err != nil {
			itemErrorCount++
			if firstError == nil {
				firstError = err
				firstErrorChunkIndex = i
				// Only log verbose if first block fails
				chunkPreview := chunk
				if len(chunkPreview) > 200 {
					chunkPreview = chunkPreview[:200] + "..."
				}
				idx.logger.Warn("Vectorization failed",
					zap.String("itemId", itemID),
					zap.Int("chunkIndex", i),
					zap.Int("totalChunks", len(chunks)),
					zap.String("chunkPreview", chunkPreview),
					zap.Error(err),
				)
				
				// Update global error tracking
				errorMsg := fmt.Sprintf("Vectorization failed (knowledge item: %s): %v", itemID, err)
				idx.mu.Lock()
				idx.lastError = errorMsg
				idx.lastErrorTime = time.Now()
				idx.mu.Unlock()
			}
			
			// If 2 blocks fail in a row, stop processing the knowledge item immediately (lower the threshold, stop faster)
			// This avoids further wasted API calls and allows for faster detection of configuration issues
			if itemErrorCount >= 2 {
				idx.logger.Error("Continuous vectorization of knowledge items failed, processing stopped",
					zap.String("itemId", itemID),
					zap.Int("totalChunks", len(chunks)),
					zap.Int("failedChunks", itemErrorCount),
					zap.Int("firstErrorChunkIndex", firstErrorChunkIndex),
					zap.Error(firstError),
				)
				return fmt.Errorf("Continuous vectorization of knowledge items failed (%d blocks failed): %v", itemErrorCount, firstError)
			}
			continue
		}

		// Save vector
		chunkID := uuid.New().String()
		embeddingJSON, _ := json.Marshal(embedding)

		_, err = idx.db.Exec(
			"INSERT INTO knowledge_embeddings (id, item_id, chunk_index, chunk_text, embedding, created_at) VALUES (?, ?, ?, ?, ?, datetime('now'))",
			chunkID, itemID, i, chunk, string(embeddingJSON),
		)
		if err != nil {
			idx.logger.Warn("Failed to save vector", zap.String("itemId", itemID), zap.Int("chunkIndex", i), zap.Error(err))
			continue
		}
	}

	idx.logger.Info("Knowledge item index completed", zap.String("itemId", itemID), zap.Int("chunks", len(chunks)))
	return nil
}

// HasIndex checks whether an index exists
func (idx *Indexer) HasIndex() (bool, error) {
	var count int
	err := idx.db.QueryRow("SELECT COUNT(*) FROM knowledge_embeddings").Scan(&count)
	if err != nil {
		return false, fmt.Errorf("Failed to check index: %w", err)
	}
	return count > 0, nil
}

// RebuildIndex rebuilds all indexes
func (idx *Indexer) RebuildIndex(ctx context.Context) error {
	// Reset error tracking
	idx.mu.Lock()
	idx.lastError = ""
	idx.lastErrorTime = time.Time{}
	idx.errorCount = 0
	idx.mu.Unlock()
	
	rows, err := idx.db.Query("SELECT id FROM knowledge_base_items")
	if err != nil {
		return fmt.Errorf("Failed to query knowledge items: %w", err)
	}
	defer rows.Close()

	var itemIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("Failed to scan knowledge item ID: %w", err)
		}
		itemIDs = append(itemIDs, id)
	}

	idx.logger.Info("Start rebuilding index", zap.Int("totalItems", len(itemIDs)))

	// Before starting the reconstruction, clear all old vectors to ensure that the progress starts from 0
	// This way GetIndexStatus can accurately reflect the reconstruction progress
	_, err = idx.db.Exec("DELETE FROM knowledge_embeddings")
	if err != nil {
		idx.logger.Warn("Failed to clear old index", zap.Error(err))
		// Continue execution and try to rebuild even if cleanup fails
	} else {
		idx.logger.Info("The old index has been cleared and reconstruction has started")
	}

	failedCount := 0
	consecutiveFailures := 0
	maxConsecutiveFailures := 2 // Stop immediately after 2 consecutive failures (lower threshold, stop faster)
	firstFailureItemID := ""
	var firstFailureError error
	
	for i, itemID := range itemIDs {
		if err := idx.IndexItem(ctx, itemID); err != nil {
			failedCount++
			consecutiveFailures++
			
			// Only log verbose on first failure
			if consecutiveFailures == 1 {
				firstFailureItemID = itemID
				firstFailureError = err
				idx.logger.Warn("Indexing knowledge items failed",
					zap.String("itemId", itemID),
					zap.Int("totalItems", len(itemIDs)),
					zap.Error(err),
				)
			}
			
			// If there are too many consecutive failures, it may be a configuration problem and stop indexing immediately.
			if consecutiveFailures >= maxConsecutiveFailures {
				errorMsg := fmt.Sprintf("Indexing of %d consecutive knowledge items failed. There may be configuration issues (such as embedded model configuration errors, invalid API keys, insufficient balance, etc.). First failure: %s, error: %v", consecutiveFailures, firstFailureItemID, firstFailureError)
				idx.mu.Lock()
				idx.lastError = errorMsg
				idx.lastErrorTime = time.Now()
				idx.mu.Unlock()
				
				idx.logger.Error("There are too many consecutive indexing failures. Stop indexing immediately.",
					zap.Int("consecutiveFailures", consecutiveFailures),
					zap.Int("totalItems", len(itemIDs)),
					zap.Int("processedItems", i+1),
					zap.String("firstFailureItemId", firstFailureItemID),
					zap.Error(firstFailureError),
				)
				return fmt.Errorf("Too many consecutive index failures: %v", firstFailureError)
			}
			
			// If too many knowledge items fail, log a warning but continue processing (lower the threshold to 30%)
			if failedCount > len(itemIDs)*3/10 && failedCount == len(itemIDs)*3/10+1 {
				errorMsg := fmt.Sprintf("Too many knowledge items (%d/%d) failed to be indexed, there may be a configuration issue. First failure: %s, error: %v", failedCount, len(itemIDs), firstFailureItemID, firstFailureError)
				idx.mu.Lock()
				idx.lastError = errorMsg
				idx.lastErrorTime = time.Now()
				idx.mu.Unlock()
				
				idx.logger.Error("There are too many knowledge items that failed to be indexed. There may be a configuration problem.",
					zap.Int("failedCount", failedCount),
					zap.Int("totalItems", len(itemIDs)),
					zap.String("firstFailureItemId", firstFailureItemID),
					zap.Error(firstFailureError),
				)
			}
			continue
		}
		
		// Reset the consecutive failure count and first failure information on success
		if consecutiveFailures > 0 {
			consecutiveFailures = 0
			firstFailureItemID = ""
			firstFailureError = nil
		}
		
		// Reduce progress log frequency (log every 10 or 10%)
		if (i+1)%10 == 0 || (len(itemIDs) > 0 && (i+1)*100/len(itemIDs)%10 == 0 && (i+1)*100/len(itemIDs) > 0) {
			idx.logger.Info("Index progress", zap.Int("current", i+1), zap.Int("total", len(itemIDs)), zap.Int("failed", failedCount))
		}
	}

	idx.logger.Info("Index rebuild completed", zap.Int("totalItems", len(itemIDs)), zap.Int("failedCount", failedCount))
	return nil
}

// GetLastError gets the latest error information
func (idx *Indexer) GetLastError() (string, time.Time) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.lastError, idx.lastErrorTime
}
