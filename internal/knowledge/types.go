package knowledge

import (
	"encoding/json"
	"time"
)

// KnowledgeItem knowledge base item
type KnowledgeItem struct {
	ID        string    `json:"id"`
	Category  string    `json:"category"` // Risk type (folder name)
	Title     string    `json:"title"`    // Title (file name)
	FilePath  string    `json:"filePath"` // File path
	Content   string    `json:"content"`  // File content
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// KnowledgeItemSummary Knowledge base item summary (for lists, does not contain full content)
type KnowledgeItemSummary struct {
	ID        string    `json:"id"`
	Category  string    `json:"category"`
	Title     string    `json:"title"`
	FilePath  string    `json:"filePath"`
	Content   string    `json:"content,omitempty"` // Optional: content preview (if provided, usually only contains the first 150 characters)
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// MarshalJSON Custom JSON serialization to ensure correct time format
func (k *KnowledgeItemSummary) MarshalJSON() ([]byte, error) {
	type Alias KnowledgeItemSummary
	aux := &struct {
		*Alias
		CreatedAt string `json:"createdAt"`
		UpdatedAt string `json:"updatedAt"`
	}{
		Alias: (*Alias)(k),
	}

	// Format creation time
	if k.CreatedAt.IsZero() {
		aux.CreatedAt = ""
	} else {
		aux.CreatedAt = k.CreatedAt.Format(time.RFC3339)
	}

	// Format update time
	if k.UpdatedAt.IsZero() {
		aux.UpdatedAt = ""
	} else {
		aux.UpdatedAt = k.UpdatedAt.Format(time.RFC3339)
	}

	return json.Marshal(aux)
}

// MarshalJSON Custom JSON serialization to ensure correct time format
func (k *KnowledgeItem) MarshalJSON() ([]byte, error) {
	type Alias KnowledgeItem
	aux := &struct {
		*Alias
		CreatedAt string `json:"createdAt"`
		UpdatedAt string `json:"updatedAt"`
	}{
		Alias: (*Alias)(k),
	}

	// Format creation time
	if k.CreatedAt.IsZero() {
		aux.CreatedAt = ""
	} else {
		aux.CreatedAt = k.CreatedAt.Format(time.RFC3339)
	}

	// Format update time
	if k.UpdatedAt.IsZero() {
		aux.UpdatedAt = ""
	} else {
		aux.UpdatedAt = k.UpdatedAt.Format(time.RFC3339)
	}

	return json.Marshal(aux)
}

// KnowledgeChunk knowledge chunk (for vectorization)
type KnowledgeChunk struct {
	ID         string    `json:"id"`
	ItemID     string    `json:"itemId"`
	ChunkIndex int       `json:"chunkIndex"`
	ChunkText  string    `json:"chunkText"`
	Embedding  []float32 `json:"-"` // Vector embedding, not serialized to JSON
	CreatedAt  time.Time `json:"createdAt"`
}

// RetrievalResult retrieval results
type RetrievalResult struct {
	Chunk      *KnowledgeChunk `json:"chunk"`
	Item       *KnowledgeItem  `json:"item"`
	Similarity float64         `json:"similarity"` // Similarity score
	Score      float64         `json:"score"`      // Comprehensive Score (Hybrid Search)
}

// RetrievalLog Retrieval log
type RetrievalLog struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversationId,omitempty"`
	MessageID      string    `json:"messageId,omitempty"`
	Query          string    `json:"query"`
	RiskType       string    `json:"riskType,omitempty"`
	RetrievedItems []string  `json:"retrievedItems"` // List of retrieved knowledge item IDs
	CreatedAt      time.Time `json:"createdAt"`
}

// MarshalJSON Custom JSON serialization to ensure correct time format
func (r *RetrievalLog) MarshalJSON() ([]byte, error) {
	type Alias RetrievalLog
	return json.Marshal(&struct {
		*Alias
		CreatedAt string `json:"createdAt"`
	}{
		Alias:     (*Alias)(r),
		CreatedAt: r.CreatedAt.Format(time.RFC3339),
	})
}

// CategoryWithItems category and the knowledge items under it (used for paging by category)
type CategoryWithItems struct {
	Category string                `json:"category"`           // Category name
	ItemCount int                  `json:"itemCount"`          // The total number of knowledge items under this category
	Items     []*KnowledgeItemSummary `json:"items"`          // List of knowledge items under this category
}

// SearchRequest search request
type SearchRequest struct {
	Query     string  `json:"query"`
	RiskType  string  `json:"riskType,omitempty"`  // Optional: Specify the risk type
	TopK      int     `json:"topK,omitempty"`      // Returns Top-K results, default 5
	Threshold float64 `json:"threshold,omitempty"` // Similarity threshold, default 0.7
}
