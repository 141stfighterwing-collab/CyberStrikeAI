package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Conversation
type Conversation struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Pinned    bool      `json:"pinned"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Messages  []Message `json:"messages,omitempty"`
}

// Message message
type Message struct {
	ID              string                   `json:"id"`
	ConversationID  string                   `json:"conversationId"`
	Role            string                   `json:"role"`
	Content         string                   `json:"content"`
	MCPExecutionIDs []string                 `json:"mcpExecutionIds,omitempty"`
	ProcessDetails  []map[string]interface{} `json:"processDetails,omitempty"`
	CreatedAt       time.Time                `json:"createdAt"`
}

// CreateConversation creates a new conversation
func (db *DB) CreateConversation(title string) (*Conversation, error) {
	id := uuid.New().String()
	now := time.Now()

	_, err := db.Exec(
		"INSERT INTO conversations (id, title, created_at, updated_at) VALUES (?, ?, ?, ?)",
		id, title, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("Failed to create conversation: %w", err)
	}

	return &Conversation{
		ID:        id,
		Title:     title,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// GetConversation Get the conversation
func (db *DB) GetConversation(id string) (*Conversation, error) {
	var conv Conversation
	var createdAt, updatedAt string
	var pinned int

	err := db.QueryRow(
		"SELECT id, title, pinned, created_at, updated_at FROM conversations WHERE id = ?",
		id,
	).Scan(&conv.ID, &conv.Title, &pinned, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("Dialogue does not exist")
		}
		return nil, fmt.Errorf("Query conversation failed: %w", err)
	}

	// Try multiple time format parsing
	var err1, err2 error
	conv.CreatedAt, err1 = time.Parse("2006-01-02 15:04:05.999999999-07:00", createdAt)
	if err1 != nil {
		conv.CreatedAt, err1 = time.Parse("2006-01-02 15:04:05", createdAt)
	}
	if err1 != nil {
		conv.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	}

	conv.UpdatedAt, err2 = time.Parse("2006-01-02 15:04:05.999999999-07:00", updatedAt)
	if err2 != nil {
		conv.UpdatedAt, err2 = time.Parse("2006-01-02 15:04:05", updatedAt)
	}
	if err2 != nil {
		conv.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	}

	conv.Pinned = pinned != 0

	// Load message
	messages, err := db.GetMessages(id)
	if err != nil {
		return nil, fmt.Errorf("Loading message failed: %w", err)
	}
	conv.Messages = messages

	// Loading process details (grouped by message ID)
	processDetailsMap, err := db.GetProcessDetailsByConversation(id)
	if err != nil {
		db.logger.Warn("Failed to load process details", zap.Error(err))
		processDetailsMap = make(map[string][]ProcessDetail)
	}

	// Attach process details to the corresponding message
	for i := range conv.Messages {
		if details, ok := processDetailsMap[conv.Messages[i].ID]; ok {
			// Convert ProcessDetail to JSON format for front-end use
			detailsJSON := make([]map[string]interface{}, len(details))
			for j, detail := range details {
				var data interface{}
				if detail.Data != "" {
					if err := json.Unmarshal([]byte(detail.Data), &data); err != nil {
						db.logger.Warn("Failed to parse process details data", zap.Error(err))
					}
				}
				detailsJSON[j] = map[string]interface{}{
					"id":             detail.ID,
					"messageId":      detail.MessageID,
					"conversationId": detail.ConversationID,
					"eventType":      detail.EventType,
					"message":        detail.Message,
					"data":           data,
					"createdAt":      detail.CreatedAt,
				}
			}
			conv.Messages[i].ProcessDetails = detailsJSON
		}
	}

	return &conv, nil
}

// ListConversations lists all conversations
func (db *DB) ListConversations(limit, offset int, search string) ([]*Conversation, error) {
	var rows *sql.Rows
	var err error
	
	if search != "" {
		// Use LIKE for fuzzy search to search titles and message content
		searchPattern := "%" + search + "%"
		// Use DISTINCT to avoid duplication as multiple messages may match a conversation
		rows, err = db.Query(
			`SELECT DISTINCT c.id, c.title, COALESCE(c.pinned, 0), c.created_at, c.updated_at 
			 FROM conversations c
			 LEFT JOIN messages m ON c.id = m.conversation_id
			 WHERE c.title LIKE ? OR m.content LIKE ?
			 ORDER BY c.updated_at DESC 
			 LIMIT ? OFFSET ?`,
			searchPattern, searchPattern, limit, offset,
		)
	} else {
		rows, err = db.Query(
			"SELECT id, title, COALESCE(pinned, 0), created_at, updated_at FROM conversations ORDER BY updated_at DESC LIMIT ? OFFSET ?",
			limit, offset,
		)
	}
	
	if err != nil {
		return nil, fmt.Errorf("Failed to query conversation list: %w", err)
	}
	defer rows.Close()

	var conversations []*Conversation
	for rows.Next() {
		var conv Conversation
		var createdAt, updatedAt string
		var pinned int

		if err := rows.Scan(&conv.ID, &conv.Title, &pinned, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("Scan session failed: %w", err)
		}

		// Try multiple time format parsing
		var err1, err2 error
		conv.CreatedAt, err1 = time.Parse("2006-01-02 15:04:05.999999999-07:00", createdAt)
		if err1 != nil {
			conv.CreatedAt, err1 = time.Parse("2006-01-02 15:04:05", createdAt)
		}
		if err1 != nil {
			conv.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		}

		conv.UpdatedAt, err2 = time.Parse("2006-01-02 15:04:05.999999999-07:00", updatedAt)
		if err2 != nil {
			conv.UpdatedAt, err2 = time.Parse("2006-01-02 15:04:05", updatedAt)
		}
		if err2 != nil {
			conv.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		}

		conv.Pinned = pinned != 0

		conversations = append(conversations, &conv)
	}

	return conversations, nil
}

// UpdateConversationTitle Update conversation title
func (db *DB) UpdateConversationTitle(id, title string) error {
	// Note: updated_at is not updated because the rename operation should not change the update time of the conversation
	_, err := db.Exec(
		"UPDATE conversations SET title = ? WHERE id = ?",
		title, id,
	)
	if err != nil {
		return fmt.Errorf("Failed to update conversation title: %w", err)
	}
	return nil
}

// UpdateConversationTime update conversation time
func (db *DB) UpdateConversationTime(id string) error {
	_, err := db.Exec(
		"UPDATE conversations SET updated_at = ? WHERE id = ?",
		time.Now(), id,
	)
	if err != nil {
		return fmt.Errorf("Failed to update session time: %w", err)
	}
	return nil
}

// DeleteConversation deletes the conversation and all related data
// Since the database foreign key constraint is set to ON DELETE CASCADE, it will be automatically deleted when the conversation is deleted:
// - messages
// - process_details (process details)
// - attack_chain_nodes (attack chain nodes)
// - attack_chain_edges (attack chain edges)
// - vulnerabilities
// - conversation_group_mappings (group mapping)
// Note: knowledge_retrieval_logs uses ON DELETE SET NULL, the records will be retained but conversation_id will be set to NULL
func (db *DB) DeleteConversation(id string) error {
	// Explicitly delete the knowledge retrieval log (although the foreign key is SET NULL, we delete it manually for thorough cleaning)
	_, err := db.Exec("DELETE FROM knowledge_retrieval_logs WHERE conversation_id = ?", id)
	if err != nil {
		db.logger.Warn("Failed to delete knowledge retrieval log", zap.String("conversationId", id), zap.Error(err))
		// Do not return an error and continue deleting the conversation
	}

	// Delete the conversation (the foreign key CASCADE will automatically delete other related data)
	_, err = db.Exec("DELETE FROM conversations WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("Failed to delete conversation: %w", err)
	}

	db.logger.Info("Conversation and all associated data deleted", zap.String("conversationId", id))
	return nil
}

// SaveReActData saves the input and output of the last round of ReAct
func (db *DB) SaveReActData(conversationID, reactInput, reactOutput string) error {
	_, err := db.Exec(
		"UPDATE conversations SET last_react_input = ?, last_react_output = ?, updated_at = ? WHERE id = ?",
		reactInput, reactOutput, time.Now(), conversationID,
	)
	if err != nil {
		return fmt.Errorf("Failed to save ReAct data: %w", err)
	}
	return nil
}

// GetReActData Gets the input and output of the last round of ReAct
func (db *DB) GetReActData(conversationID string) (reactInput, reactOutput string, err error) {
	var input, output sql.NullString
	err = db.QueryRow(
		"SELECT last_react_input, last_react_output FROM conversations WHERE id = ?",
		conversationID,
	).Scan(&input, &output)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", "", fmt.Errorf("Dialogue does not exist")
		}
		return "", "", fmt.Errorf("Failed to obtain ReAct data: %w", err)
	}

	if input.Valid {
		reactInput = input.String
	}
	if output.Valid {
		reactOutput = output.String
	}

	return reactInput, reactOutput, nil
}

// AddMessage Add message
func (db *DB) AddMessage(conversationID, role, content string, mcpExecutionIDs []string) (*Message, error) {
	id := uuid.New().String()

	var mcpIDsJSON string
	if len(mcpExecutionIDs) > 0 {
		jsonData, err := json.Marshal(mcpExecutionIDs)
		if err != nil {
			db.logger.Warn("Serializing MCP execution ID failed", zap.Error(err))
		} else {
			mcpIDsJSON = string(jsonData)
		}
	}

	_, err := db.Exec(
		"INSERT INTO messages (id, conversation_id, role, content, mcp_execution_ids, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		id, conversationID, role, content, mcpIDsJSON, time.Now(),
	)
	if err != nil {
		return nil, fmt.Errorf("Failed to add message: %w", err)
	}

	// Update conversation time
	if err := db.UpdateConversationTime(conversationID); err != nil {
		db.logger.Warn("Failed to update conversation time", zap.Error(err))
	}

	message := &Message{
		ID:              id,
		ConversationID:  conversationID,
		Role:            role,
		Content:         content,
		MCPExecutionIDs: mcpExecutionIDs,
		CreatedAt:       time.Now(),
	}

	return message, nil
}

// GetMessages Gets all messages of the conversation
func (db *DB) GetMessages(conversationID string) ([]Message, error) {
	rows, err := db.Query(
		"SELECT id, conversation_id, role, content, mcp_execution_ids, created_at FROM messages WHERE conversation_id = ? ORDER BY created_at ASC",
		conversationID,
	)
	if err != nil {
		return nil, fmt.Errorf("Query message failed: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var msg Message
		var mcpIDsJSON sql.NullString
		var createdAt string

		if err := rows.Scan(&msg.ID, &msg.ConversationID, &msg.Role, &msg.Content, &mcpIDsJSON, &createdAt); err != nil {
			return nil, fmt.Errorf("Scan message failed: %w", err)
		}

		// Try multiple time format parsing
		var err error
		msg.CreatedAt, err = time.Parse("2006-01-02 15:04:05.999999999-07:00", createdAt)
		if err != nil {
			msg.CreatedAt, err = time.Parse("2006-01-02 15:04:05", createdAt)
		}
		if err != nil {
			msg.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		}

		// Parse MCP execution ID
		if mcpIDsJSON.Valid && mcpIDsJSON.String != "" {
			if err := json.Unmarshal([]byte(mcpIDsJSON.String), &msg.MCPExecutionIDs); err != nil {
				db.logger.Warn("Failed to parse MCP execution ID", zap.Error(err))
			}
		}

		messages = append(messages, msg)
	}

	return messages, nil
}

// ProcessDetail process details event
type ProcessDetail struct {
	ID             string    `json:"id"`
	MessageID      string    `json:"messageId"`
	ConversationID string    `json:"conversationId"`
	EventType      string    `json:"eventType"` // iteration, thinking, tool_calls_detected, tool_call, tool_result, progress, error
	Message        string    `json:"message"`
	Data           string    `json:"data"` // Data in JSON format
	CreatedAt      time.Time `json:"createdAt"`
}

// AddProcessDetail Add process details event
func (db *DB) AddProcessDetail(messageID, conversationID, eventType, message string, data interface{}) error {
	id := uuid.New().String()

	var dataJSON string
	if data != nil {
		jsonData, err := json.Marshal(data)
		if err != nil {
			db.logger.Warn("Serialization process details data failed", zap.Error(err))
		} else {
			dataJSON = string(jsonData)
		}
	}

	_, err := db.Exec(
		"INSERT INTO process_details (id, message_id, conversation_id, event_type, message, data, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		id, messageID, conversationID, eventType, message, dataJSON, time.Now(),
	)
	if err != nil {
		return fmt.Errorf("Failed to add process details: %w", err)
	}

	return nil
}

// GetProcessDetails Gets the process details of the message
func (db *DB) GetProcessDetails(messageID string) ([]ProcessDetail, error) {
	rows, err := db.Query(
		"SELECT id, message_id, conversation_id, event_type, message, data, created_at FROM process_details WHERE message_id = ? ORDER BY created_at ASC",
		messageID,
	)
	if err != nil {
		return nil, fmt.Errorf("Failed to query process details: %w", err)
	}
	defer rows.Close()

	var details []ProcessDetail
	for rows.Next() {
		var detail ProcessDetail
		var createdAt string

		if err := rows.Scan(&detail.ID, &detail.MessageID, &detail.ConversationID, &detail.EventType, &detail.Message, &detail.Data, &createdAt); err != nil {
			return nil, fmt.Errorf("Scan process details failed: %w", err)
		}

		// Try multiple time format parsing
		var err error
		detail.CreatedAt, err = time.Parse("2006-01-02 15:04:05.999999999-07:00", createdAt)
		if err != nil {
			detail.CreatedAt, err = time.Parse("2006-01-02 15:04:05", createdAt)
		}
		if err != nil {
			detail.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		}

		details = append(details, detail)
	}

	return details, nil
}

// GetProcessDetailsByConversation Gets all process details of a conversation (grouped by message)
func (db *DB) GetProcessDetailsByConversation(conversationID string) (map[string][]ProcessDetail, error) {
	rows, err := db.Query(
		"SELECT id, message_id, conversation_id, event_type, message, data, created_at FROM process_details WHERE conversation_id = ? ORDER BY created_at ASC",
		conversationID,
	)
	if err != nil {
		return nil, fmt.Errorf("Failed to query process details: %w", err)
	}
	defer rows.Close()

	detailsMap := make(map[string][]ProcessDetail)
	for rows.Next() {
		var detail ProcessDetail
		var createdAt string

		if err := rows.Scan(&detail.ID, &detail.MessageID, &detail.ConversationID, &detail.EventType, &detail.Message, &detail.Data, &createdAt); err != nil {
			return nil, fmt.Errorf("Scan process details failed: %w", err)
		}

		// Try multiple time format parsing
		var err error
		detail.CreatedAt, err = time.Parse("2006-01-02 15:04:05.999999999-07:00", createdAt)
		if err != nil {
			detail.CreatedAt, err = time.Parse("2006-01-02 15:04:05", createdAt)
		}
		if err != nil {
			detail.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		}

		detailsMap[detail.MessageID] = append(detailsMap[detail.MessageID], detail)
	}

	return detailsMap, nil
}
