package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"go.uber.org/zap"
)

// Retriever retriever
type Retriever struct {
	db       *sql.DB
	embedder *Embedder
	config   *RetrievalConfig
	logger   *zap.Logger
}

// RetrievalConfig Retrieve configuration
type RetrievalConfig struct {
	TopK                int
	SimilarityThreshold float64
	HybridWeight        float64
}

// NewRetriever creates a new retriever
func NewRetriever(db *sql.DB, embedder *Embedder, config *RetrievalConfig, logger *zap.Logger) *Retriever {
	return &Retriever{
		db:       db,
		embedder: embedder,
		config:   config,
		logger:   logger,
	}
}

// UpdateConfig Update retrieval configuration
func (r *Retriever) UpdateConfig(config *RetrievalConfig) {
	if config != nil {
		r.config = config
		r.logger.Info("Retriever configuration updated",
			zap.Int("top_k", config.TopK),
			zap.Float64("similarity_threshold", config.SimilarityThreshold),
			zap.Float64("hybrid_weight", config.HybridWeight),
		)
	}
}

// CosineSimilarity calculates cosine similarity
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0.0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += float64(a[i] * b[i])
		normA += float64(a[i] * a[i])
		normB += float64(b[i] * b[i])
	}

	if normA == 0 || normB == 0 {
		return 0.0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// Bm25Score calculates BM25 score (improved version, closer to standard BM25)
// NOTE: This is a single-document version of BM25, missing the global IDF, but more accurate than the previous simplified version
func (r *Retriever) bm25Score(query, text string) float64 {
	queryTerms := strings.Fields(strings.ToLower(query))
	if len(queryTerms) == 0 {
		return 0.0
	}

	textLower := strings.ToLower(text)
	textTerms := strings.Fields(textLower)
	if len(textTerms) == 0 {
		return 0.0
	}

	// BM25 parameters
	k1 := 1.5             // Word frequency saturation parameter
	b := 0.75             // Length normalization parameter
	avgDocLength := 100.0 // Estimated average document length (used for normalization)
	docLength := float64(len(textTerms))

	score := 0.0
	for _, term := range queryTerms {
		// Calculate word frequency (TF)
		termFreq := 0
		for _, textTerm := range textTerms {
			if textTerm == term {
				termFreq++
			}
		}

		if termFreq > 0 {
			// The core part of the BM25 formula
			// TF part: termFreq / (termFreq + k1 * (1 - b + b * (docLength / avgDocLength)))
			tf := float64(termFreq)
			lengthNorm := 1 - b + b*(docLength/avgDocLength)
			tfScore := tf / (tf + k1*lengthNorm)

			// Simplify IDF: use word length as weight (shorter words are usually more important)
			// Actual BM25 requires global document statistics, and a simplified version is used here.
			idfWeight := 1.0
			if len(term) > 2 {
				// Long words slightly reduce the weight (but in actual BM25, the IDF of rare words is higher)
				idfWeight = 1.0 + math.Log(1.0+float64(len(term))/10.0)
			}

			score += tfScore * idfWeight
		}
	}

	// Normalized to the 0-1 range
	if len(queryTerms) > 0 {
		score = score / float64(len(queryTerms))
	}

	return math.Min(score, 1.0)
}

// Search Search knowledge base
func (r *Retriever) Search(ctx context.Context, req *SearchRequest) ([]*RetrievalResult, error) {
	if req.Query == "" {
		return nil, fmt.Errorf("Query cannot be empty")
	}

	topK := req.TopK
	if topK <= 0 {
		topK = r.config.TopK
	}
	if topK == 0 {
		topK = 5
	}

	threshold := req.Threshold
	if threshold <= 0 {
		threshold = r.config.SimilarityThreshold
	}
	if threshold == 0 {
		threshold = 0.7
	}

	// Vectorized query (if risk_type is provided, also included in query text for better matching)
	queryText := req.Query
	if req.RiskType != "" {
		// Include risk_type information into the query, keeping the format consistent with indexing
		queryText = fmt.Sprintf("[Risk Type: %s] %s", req.RiskType, req.Query)
	}
	queryEmbedding, err := r.embedder.EmbedText(ctx, queryText)
	if err != nil {
		return nil, fmt.Errorf("Vectorized query failed: %w", err)
	}

	// Query all vectors (or filter by risk type)
	// Use exact matching (=) to improve performance and accuracy
	// Since the system provides built-in tools to obtain a list of risk types, users should use the exact category name
		// At the same time, vector embedding already contains category information. Even if SQL filtering does not match exactly, vector similarity can help match.
		var rows *sql.Rows
		if req.RiskType != "" {
			// Use exact matching (=) for better performance and more accuracy
			// Use COLLATE NOCASE to achieve case-insensitive matching and improve fault tolerance
			// Note: If the risk_type entered by the user is not exactly the same as category, there may be no match.
			// It is recommended that users first call the corresponding built-in tool to obtain the accurate category name.
		rows, err = r.db.Query(`
			SELECT e.id, e.item_id, e.chunk_index, e.chunk_text, e.embedding, i.category, i.title
			FROM knowledge_embeddings e
			JOIN knowledge_base_items i ON e.item_id = i.id
			WHERE i.category = ? COLLATE NOCASE
		`, req.RiskType)
	} else {
		rows, err = r.db.Query(`
			SELECT e.id, e.item_id, e.chunk_index, e.chunk_text, e.embedding, i.category, i.title
			FROM knowledge_embeddings e
			JOIN knowledge_base_items i ON e.item_id = i.id
		`)
	}
	if err != nil {
		return nil, fmt.Errorf("Query vector failed: %w", err)
	}
	defer rows.Close()

	// Calculate similarity
	type candidate struct {
		chunk                 *KnowledgeChunk
		item                  *KnowledgeItem
		similarity            float64
		bm25Score             float64
		hasStrongKeywordMatch bool
		hybridScore           float64 // Mixed score, used for final ranking
	}

	candidates := make([]candidate, 0)

	for rows.Next() {
		var chunkID, itemID, chunkText, embeddingJSON, category, title string
		var chunkIndex int

		if err := rows.Scan(&chunkID, &itemID, &chunkIndex, &chunkText, &embeddingJSON, &category, &title); err != nil {
			r.logger.Warn("Scan vector failed", zap.Error(err))
			continue
		}

		// Parse vector
		var embedding []float32
		if err := json.Unmarshal([]byte(embeddingJSON), &embedding); err != nil {
			r.logger.Warn("Failed to parse vector", zap.Error(err))
			continue
		}

		// Calculate cosine similarity
		similarity := cosineSimilarity(queryEmbedding, embedding)

		// Calculate BM25 score (considering chunk text, category and title)
		// Category and title are structured fields and should be given priority when matching exactly.
		chunkBM25 := r.bm25Score(req.Query, chunkText)
		categoryBM25 := r.bm25Score(req.Query, category)
		titleBM25 := r.bm25Score(req.Query, title)

		// Check if category or title has a significant match (this is important for structured fields)
		hasStrongKeywordMatch := categoryBM25 > 0.3 || titleBM25 > 0.3

		// Comprehensive BM25 score (used for subsequent ranking)
		bm25Score := math.Max(math.Max(chunkBM25, categoryBM25), titleBM25)

		// Collect all candidates (not strict filtering at first, so that cross-language situations can be handled intelligently later)
		// Only filter out results with extremely low similarity (< 0.1) to avoid noise
		if similarity < 0.1 {
			continue
		}

		chunk := &KnowledgeChunk{
			ID:         chunkID,
			ItemID:     itemID,
			ChunkIndex: chunkIndex,
			ChunkText:  chunkText,
			Embedding:  embedding,
		}

		item := &KnowledgeItem{
			ID:       itemID,
			Category: category,
			Title:    title,
		}

		candidates = append(candidates, candidate{
			chunk:                 chunk,
			item:                  item,
			similarity:            similarity,
			bm25Score:             bm25Score,
			hasStrongKeywordMatch: hasStrongKeywordMatch,
		})
	}

	// Sort by similarity first (use more efficient sorting)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].similarity > candidates[j].similarity
	})

	// Intelligent filtering strategy: Prioritize keyword matching results and use looser thresholds for cross-language queries
	filteredCandidates := make([]candidate, 0)

	// Check if there are any keyword matches (used to determine whether it is a cross-language query)
	hasAnyKeywordMatch := false
	for _, cand := range candidates {
		if cand.hasStrongKeywordMatch {
			hasAnyKeywordMatch = true
			break
		}
	}

	// Check the highest similarity to determine whether there is indeed relevant content
	maxSimilarity := 0.0
	if len(candidates) > 0 {
		maxSimilarity = candidates[0].similarity
	}

	// Apply smart filtering
	// If the user sets a high threshold (>=0.8), adhere to the threshold more strictly and reduce automatic relaxation
	strictMode := threshold >= 0.8

	// Depending on whether there is a keyword match, different threshold strategies are used
	// In strict mode, the cross-language relaxation policy is disabled and the threshold set by the user is strictly adhered to.
	effectiveThreshold := threshold
	if !strictMode && !hasAnyKeywordMatch {
		// In non-strict mode, there is no keyword matching, it may be a cross-language query, and the threshold is moderately relaxed.
		// But even across languages, the threshold cannot be lowered without thinking, and the minimum correlation needs to be ensured.
		// The cross-language threshold is set to 0.6 to ensure that the returned results are at least somewhat relevant.
		effectiveThreshold = math.Max(threshold*0.85, 0.6)
		r.logger.Debug("Possible cross-language query detected, using relaxed threshold",
			zap.Float64("originalThreshold", threshold),
			zap.Float64("effectiveThreshold", effectiveThreshold),
		)
	} else if strictMode {
		// In strict mode, the threshold is strictly adhered to even if there is no keyword match.
		r.logger.Debug("Strict mode: Strictly adhere to user-set thresholds",
			zap.Float64("threshold", threshold),
			zap.Bool("hasKeywordMatch", hasAnyKeywordMatch),
		)
	}
	for _, cand := range candidates {
		if cand.similarity >= effectiveThreshold {
			// Reaches the threshold and passes directly
			filteredCandidates = append(filteredCandidates, cand)
		} else if !strictMode && cand.hasStrongKeywordMatch {
			// In non-strict mode, there are keyword matches but the similarity is slightly lower than the threshold, so relax appropriately.
			// In strict mode, even if there is a keyword match, the threshold is strictly adhered to.
			relaxedThreshold := math.Max(effectiveThreshold*0.85, 0.55)
			if cand.similarity >= relaxedThreshold {
				filteredCandidates = append(filteredCandidates, cand)
			}
		}
		// If there is no keyword match and the similarity is lower than the threshold, filter out
	}

	// Intelligent back-up strategy: Only when the highest similarity reaches a reasonable level, the result will be considered to be returned
	// If the highest similarity is very low (<0.55), it means there is indeed no relevant content and empty should be returned.
	// In strict mode (threshold >= 0.8), the cover-up policy is disabled and the threshold set by the user is strictly adhered to.
	if len(filteredCandidates) == 0 && len(candidates) > 0 && !strictMode {
		// Even if the threshold filtering is not passed, if the highest similarity is okay (>=0.55), you can consider returning Top-K
		// But this is a last resort and should only be used when there is a certain relevance.
		// Do not use the cover-up strategy in strict mode
		minAcceptableSimilarity := 0.55
		if maxSimilarity >= minAcceptableSimilarity {
			r.logger.Debug("There are no results after filtering, but the highest similarity is acceptable, and Top-K results are returned.",
				zap.Int("totalCandidates", len(candidates)),
				zap.Float64("maxSimilarity", maxSimilarity),
				zap.Float64("effectiveThreshold", effectiveThreshold),
			)
			maxResults := topK
			if len(candidates) < maxResults {
				maxResults = len(candidates)
			}
			// Only results with similarity >= 0.55 are returned
			for _, cand := range candidates {
				if cand.similarity >= minAcceptableSimilarity && len(filteredCandidates) < maxResults {
					filteredCandidates = append(filteredCandidates, cand)
				}
			}
		} else {
			r.logger.Debug("There are no results after filtering, and the highest similarity is too low, so empty results are returned.",
				zap.Int("totalCandidates", len(candidates)),
				zap.Float64("maxSimilarity", maxSimilarity),
				zap.Float64("minAcceptableSimilarity", minAcceptableSimilarity),
			)
		}
	} else if len(filteredCandidates) == 0 && strictMode {
		// In strict mode, if there are no results after filtering, empty will be returned directly without using the cover-up strategy.
		r.logger.Debug("Strict mode: no results after filtering, strictly comply with the threshold, and return empty results",
			zap.Float64("threshold", threshold),
			zap.Float64("maxSimilarity", maxSimilarity),
		)
	} else if len(filteredCandidates) > topK {
		// If there are too many results after filtering, only take the Top-K
		filteredCandidates = filteredCandidates[:topK]
	}

	candidates = filteredCandidates

	// Hybrid sorting (vector similarity + BM25)
	// Note: hybridWeight can be 0.0 (pure keyword search), so no default value is set
	// If not set in the configuration file, the default value should be used when the configuration is loaded
	hybridWeight := r.config.HybridWeight
	// If not set, the default value of 0.7 is used (biased towards vector retrieval)
	if hybridWeight < 0 || hybridWeight > 1 {
		r.logger.Warn("Blend weight out of range, use default value 0.7",
			zap.Float64("provided", hybridWeight))
		hybridWeight = 0.7
	}

	// First calculate the mixture score and store it in candidate for sorting
	for i := range candidates {
		normalizedBM25 := math.Min(candidates[i].bm25Score, 1.0)
		candidates[i].hybridScore = hybridWeight*candidates[i].similarity + (1-hybridWeight)*normalizedBM25

		// Debug log: record the score calculation of the first few candidates (only at debug level)
		if i < 3 {
			r.logger.Debug("Mixed Fraction Calculation",
				zap.Int("index", i),
				zap.Float64("similarity", candidates[i].similarity),
				zap.Float64("bm25Score", candidates[i].bm25Score),
				zap.Float64("normalizedBM25", normalizedBM25),
				zap.Float64("hybridWeight", hybridWeight),
				zap.Float64("hybridScore", candidates[i].hybridScore))
		}
	}

	// Reorder based on mixed scores (this is true mixed retrieval)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].hybridScore > candidates[j].hybridScore
	})

	// Convert to result
	results := make([]*RetrievalResult, len(candidates))
	for i, cand := range candidates {
		results[i] = &RetrievalResult{
			Chunk:      cand.chunk,
			Item:       cand.item,
			Similarity: cand.similarity,
			Score:      cand.hybridScore,
		}
	}

	// Context expansion: for each matching chunk, add related chunks from the same document
	// This can prevent the problem of only returning the description and losing the payload when the text description and payload are split separately.
	results = r.expandContext(ctx, results)

	return results, nil
}

// ExpandContext expands the context of the search results
// For each matching chunk, automatically include related chunks in the same document (especially chunks containing code blocks and payloads)
func (r *Retriever) expandContext(ctx context.Context, results []*RetrievalResult) []*RetrievalResult {
	if len(results) == 0 {
		return results
	}

	// Collect all matching document IDs
	itemIDs := make(map[string]bool)
	for _, result := range results {
		itemIDs[result.Item.ID] = true
	}

	// Load all chunks for each document
	itemChunksMap := make(map[string][]*KnowledgeChunk)
	for itemID := range itemIDs {
		chunks, err := r.loadAllChunksForItem(itemID)
		if err != nil {
			r.logger.Warn("Failed to load document chunk", zap.String("itemId", itemID), zap.Error(err))
			continue
		}
		itemChunksMap[itemID] = chunks
	}

	// Group results by document, expanding each document only once
	resultsByItem := make(map[string][]*RetrievalResult)
	for _, result := range results {
		itemID := result.Item.ID
		resultsByItem[itemID] = append(resultsByItem[itemID], result)
	}

	// Expand results for each document
	expandedResults := make([]*RetrievalResult, 0, len(results))
	processedChunkIDs := make(map[string]bool) // Avoid duplicate additions

	for itemID, itemResults := range resultsByItem {
		// Get all chunks of the document
		allChunks, exists := itemChunksMap[itemID]
		if !exists {
			// If the chunk cannot be loaded, add the original result directly
			for _, result := range itemResults {
				if !processedChunkIDs[result.Chunk.ID] {
					expandedResults = append(expandedResults, result)
					processedChunkIDs[result.Chunk.ID] = true
				}
			}
			continue
		}

		// Add original result
		for _, result := range itemResults {
			if !processedChunkIDs[result.Chunk.ID] {
				expandedResults = append(expandedResults, result)
				processedChunkIDs[result.Chunk.ID] = true
			}
		}

		// Collect adjacent chunks that need to be extended for matching chunks of this document
		// Strategy: Only expand the first three matching chunks with the highest mixing scores to avoid excessive expansion.
		// Sort by mixture score first and only expand the top 3 (use mixture score instead of similarity)
		sortedItemResults := make([]*RetrievalResult, len(itemResults))
		copy(sortedItemResults, itemResults)
		sort.Slice(sortedItemResults, func(i, j int) bool {
			return sortedItemResults[i].Score > sortedItemResults[j].Score
		})

		// Expand only the first 3 (or all, if less than 3)
		maxExpandFrom := 3
		if len(sortedItemResults) < maxExpandFrom {
			maxExpandFrom = len(sortedItemResults)
		}

		// Use map to remove duplicates to avoid the same chunk being added multiple times
		relatedChunksMap := make(map[string]*KnowledgeChunk)

		for i := 0; i < maxExpandFrom; i++ {
			result := sortedItemResults[i]
			// Find relevant chunks (two above and below, excluding processed chunks)
			relatedChunks := r.findRelatedChunks(result.Chunk, allChunks, processedChunkIDs)
			for _, relatedChunk := range relatedChunks {
				// Use chunk ID as key to remove duplicates
				if !processedChunkIDs[relatedChunk.ID] {
					relatedChunksMap[relatedChunk.ID] = relatedChunk
				}
			}
		}

		// Limit the maximum number of expanded chunks for each document (to avoid excessive expansion)
		// Strategy: Expand up to 8 chunks, no matter how many chunks are matched
		// This can avoid expanding too many chunks when multiple matching chunks are scattered in different locations in the document.
		maxExpandPerItem := 8

		// Convert relevant chunks into slices and sort them by index, giving priority to the ones closest to matching chunks.
		relatedChunksList := make([]*KnowledgeChunk, 0, len(relatedChunksMap))
		for _, chunk := range relatedChunksMap {
			relatedChunksList = append(relatedChunksList, chunk)
		}

		// Calculate the distance from each relevant chunk to the nearest matching chunk, sorted by distance
		sort.Slice(relatedChunksList, func(i, j int) bool {
			// Calculate the distance to the nearest matching chunk
			minDistI := len(allChunks)
			minDistJ := len(allChunks)
			for _, result := range itemResults {
				distI := abs(relatedChunksList[i].ChunkIndex - result.Chunk.ChunkIndex)
				distJ := abs(relatedChunksList[j].ChunkIndex - result.Chunk.ChunkIndex)
				if distI < minDistI {
					minDistI = distI
				}
				if distJ < minDistJ {
					minDistJ = distJ
				}
			}
			return minDistI < minDistJ
		})

		// Limited quantity
		if len(relatedChunksList) > maxExpandPerItem {
			relatedChunksList = relatedChunksList[:maxExpandPerItem]
		}

		// Add relevant chunks after deduplication
		// Use the result with the highest blending score in that document as a reference
		maxScore := 0.0
		maxSimilarity := 0.0
		for _, result := range itemResults {
			if result.Score > maxScore {
				maxScore = result.Score
			}
			if result.Similarity > maxSimilarity {
				maxSimilarity = result.Similarity
			}
		}

		// Calculate the mixing score of the extended chunk (using the same mixing weight)
		hybridWeight := r.config.HybridWeight
		expandedSimilarity := maxSimilarity * 0.8 // The similarity of related chunks is slightly lower
		// For extended chunks, the BM25 score is set to 0 (because they are contextual extensions, not direct matches)
		expandedBM25 := 0.0
		expandedScore := hybridWeight*expandedSimilarity + (1-hybridWeight)*expandedBM25

		for _, relatedChunk := range relatedChunksList {
			expandedResult := &RetrievalResult{
				Chunk:      relatedChunk,
				Item:       itemResults[0].Item, // Use the Item information of the first result
				Similarity: expandedSimilarity,
				Score:      expandedScore, // Use the correct mixture fraction
			}
			expandedResults = append(expandedResults, expandedResult)
			processedChunkIDs[relatedChunk.ID] = true
		}
	}

	return expandedResults
}

// LoadAllChunksForItem loads all chunks of the document
func (r *Retriever) loadAllChunksForItem(itemID string) ([]*KnowledgeChunk, error) {
	rows, err := r.db.Query(`
		SELECT id, item_id, chunk_index, chunk_text, embedding
		FROM knowledge_embeddings
		WHERE item_id = ?
		ORDER BY chunk_index
	`, itemID)
	if err != nil {
		return nil, fmt.Errorf("Failed to query chunk: %w", err)
	}
	defer rows.Close()

	var chunks []*KnowledgeChunk
	for rows.Next() {
		var chunkID, itemID, chunkText, embeddingJSON string
		var chunkIndex int

		if err := rows.Scan(&chunkID, &itemID, &chunkIndex, &chunkText, &embeddingJSON); err != nil {
			r.logger.Warn("Scanning chunk failed", zap.Error(err))
			continue
		}

		// Parse vector (optional, not needed here)
		var embedding []float32
		if embeddingJSON != "" {
			json.Unmarshal([]byte(embeddingJSON), &embedding)
		}

		chunk := &KnowledgeChunk{
			ID:         chunkID,
			ItemID:     itemID,
			ChunkIndex: chunkIndex,
			ChunkText:  chunkText,
			Embedding:  embedding,
		}
		chunks = append(chunks, chunk)
	}

	return chunks, nil
}

// FindRelatedChunks finds other chunks related to a given chunk
// Strategy: Only return 2 adjacent chunks above and below (up to 4 in total)
// Exclude processed chunks to avoid repeated additions
func (r *Retriever) findRelatedChunks(targetChunk *KnowledgeChunk, allChunks []*KnowledgeChunk, processedChunkIDs map[string]bool) []*KnowledgeChunk {
	related := make([]*KnowledgeChunk, 0)

	// Find 2 adjacent chunks above and below
	for _, chunk := range allChunks {
		if chunk.ID == targetChunk.ID {
			continue
		}

		// Check if it has been processed (may be already in the search results)
		if processedChunkIDs[chunk.ID] {
			continue
		}

		// Check whether they are adjacent chunks (the index difference does not exceed 2 and is not 0)
		indexDiff := chunk.ChunkIndex - targetChunk.ChunkIndex
		if indexDiff >= -2 && indexDiff <= 2 && indexDiff != 0 {
			related = append(related, chunk)
		}
	}

	// Sort by index distance, giving priority to the nearest
	sort.Slice(related, func(i, j int) bool {
		diffI := abs(related[i].ChunkIndex - targetChunk.ChunkIndex)
		diffJ := abs(related[j].ChunkIndex - targetChunk.ChunkIndex)
		return diffI < diffJ
	})

	// Limit returns to 4 at most (2 above and 2 below)
	if len(related) > 4 {
		related = related[:4]
	}

	return related
}

// Abs returns the absolute value of an integer
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
