package database

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ConversationGroup conversation grouping
type ConversationGroup struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Icon      string    `json:"icon"`
	Pinned    bool      `json:"pinned"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// GroupExistsByName checks whether the group name already exists
func (db *DB) GroupExistsByName(name string, excludeID string) (bool, error) {
	var count int
	var err error

	if excludeID != "" {
		err = db.QueryRow(
			"SELECT COUNT(*) FROM conversation_groups WHERE name = ? AND id != ?",
			name, excludeID,
		).Scan(&count)
	} else {
		err = db.QueryRow(
			"SELECT COUNT(*) FROM conversation_groups WHERE name = ?",
			name,
		).Scan(&count)
	}

	if err != nil {
		return false, fmt.Errorf("Failed to check group name: %w", err)
	}

	return count > 0, nil
}

// CreateGroup creates a group
func (db *DB) CreateGroup(name, icon string) (*ConversationGroup, error) {
	// Check if name already exists
	exists, err := db.GroupExistsByName(name, "")
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("Group name already exists")
	}

	id := uuid.New().String()
	now := time.Now()

	if icon == "" {
		icon = "📁"
	}

	_, err = db.Exec(
		"INSERT INTO conversation_groups (id, name, icon, pinned, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		id, name, icon, 0, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("Failed to create group: %w", err)
	}

	return &ConversationGroup{
		ID:        id,
		Name:      name,
		Icon:      icon,
		Pinned:    false,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// ListGroups lists all groups
func (db *DB) ListGroups() ([]*ConversationGroup, error) {
	rows, err := db.Query(
		"SELECT id, name, icon, COALESCE(pinned, 0), created_at, updated_at FROM conversation_groups ORDER BY COALESCE(pinned, 0) DESC, created_at ASC",
	)
	if err != nil {
		return nil, fmt.Errorf("Failed to query group list: %w", err)
	}
	defer rows.Close()

	var groups []*ConversationGroup
	for rows.Next() {
		var group ConversationGroup
		var createdAt, updatedAt string
		var pinned int

		if err := rows.Scan(&group.ID, &group.Name, &group.Icon, &pinned, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("Scan group failed: %w", err)
		}

		group.Pinned = pinned != 0

		// Try multiple time format parsing
		var err1, err2 error
		group.CreatedAt, err1 = time.Parse("2006-01-02 15:04:05.999999999-07:00", createdAt)
		if err1 != nil {
			group.CreatedAt, err1 = time.Parse("2006-01-02 15:04:05", createdAt)
		}
		if err1 != nil {
			group.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		}

		group.UpdatedAt, err2 = time.Parse("2006-01-02 15:04:05.999999999-07:00", updatedAt)
		if err2 != nil {
			group.UpdatedAt, err2 = time.Parse("2006-01-02 15:04:05", updatedAt)
		}
		if err2 != nil {
			group.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		}

		groups = append(groups, &group)
	}

	return groups, nil
}

// GetGroup gets the group
func (db *DB) GetGroup(id string) (*ConversationGroup, error) {
	var group ConversationGroup
	var createdAt, updatedAt string
	var pinned int

	err := db.QueryRow(
		"SELECT id, name, icon, COALESCE(pinned, 0), created_at, updated_at FROM conversation_groups WHERE id = ?",
		id,
	).Scan(&group.ID, &group.Name, &group.Icon, &pinned, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("Group does not exist")
		}
		return nil, fmt.Errorf("Query grouping failed: %w", err)
	}

	// Try multiple time format parsing
	var err1, err2 error
	group.CreatedAt, err1 = time.Parse("2006-01-02 15:04:05.999999999-07:00", createdAt)
	if err1 != nil {
		group.CreatedAt, err1 = time.Parse("2006-01-02 15:04:05", createdAt)
	}
	if err1 != nil {
		group.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	}

	group.UpdatedAt, err2 = time.Parse("2006-01-02 15:04:05.999999999-07:00", updatedAt)
	if err2 != nil {
		group.UpdatedAt, err2 = time.Parse("2006-01-02 15:04:05", updatedAt)
	}
	if err2 != nil {
		group.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	}

	group.Pinned = pinned != 0

	return &group, nil
}

// UpdateGroup update group
func (db *DB) UpdateGroup(id, name, icon string) error {
	// Check if name already exists (exclude current group)
	exists, err := db.GroupExistsByName(name, id)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("Group name already exists")
	}

	_, err = db.Exec(
		"UPDATE conversation_groups SET name = ?, icon = ?, updated_at = ? WHERE id = ?",
		name, icon, time.Now(), id,
	)
	if err != nil {
		return fmt.Errorf("Failed to update group: %w", err)
	}
	return nil
}

// DeleteGroup Delete group
func (db *DB) DeleteGroup(id string) error {
	_, err := db.Exec("DELETE FROM conversation_groups WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("Failed to delete group: %w", err)
	}
	return nil
}

// AddConversationToGroup adds a conversation to a group
// Note: A conversation can only belong to one group, so before adding a new group, all old group associations of the conversation will be deleted.
func (db *DB) AddConversationToGroup(conversationID, groupID string) error {
	// First delete all old group associations of the conversation to ensure that a conversation only belongs to one group
	_, err := db.Exec(
		"DELETE FROM conversation_group_mappings WHERE conversation_id = ?",
		conversationID,
	)
	if err != nil {
		return fmt.Errorf("Failed to delete old group association of conversation: %w", err)
	}

	// Then insert a new group association
	id := uuid.New().String()
	_, err = db.Exec(
		"INSERT INTO conversation_group_mappings (id, conversation_id, group_id, created_at) VALUES (?, ?, ?, ?)",
		id, conversationID, groupID, time.Now(),
	)
	if err != nil {
		return fmt.Errorf("Failed to add conversation to group: %w", err)
	}
	return nil
}

// RemoveConversationFromGroup removes a conversation from a group
func (db *DB) RemoveConversationFromGroup(conversationID, groupID string) error {
	_, err := db.Exec(
		"DELETE FROM conversation_group_mappings WHERE conversation_id = ? AND group_id = ?",
		conversationID, groupID,
	)
	if err != nil {
		return fmt.Errorf("Failed to remove conversation from group: %w", err)
	}
	return nil
}

// GetConversationsByGroup Gets all conversations in the group
func (db *DB) GetConversationsByGroup(groupID string) ([]*Conversation, error) {
	rows, err := db.Query(
		`SELECT c.id, c.title, COALESCE(c.pinned, 0), c.created_at, c.updated_at, COALESCE(cgm.pinned, 0) as group_pinned
		 FROM conversations c
		 INNER JOIN conversation_group_mappings cgm ON c.id = cgm.conversation_id
		 WHERE cgm.group_id = ?
		 ORDER BY COALESCE(cgm.pinned, 0) DESC, c.updated_at DESC`,
		groupID,
	)
	if err != nil {
		return nil, fmt.Errorf("Failed to query group conversations: %w", err)
	}
	defer rows.Close()

	var conversations []*Conversation
	for rows.Next() {
		var conv Conversation
		var createdAt, updatedAt string
		var pinned int
		var groupPinned int

		if err := rows.Scan(&conv.ID, &conv.Title, &pinned, &createdAt, &updatedAt, &groupPinned); err != nil {
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

// SearchConversationsByGroup searches for conversations in groups (fuzzy matching by title and message content)
func (db *DB) SearchConversationsByGroup(groupID string, searchQuery string) ([]*Conversation, error) {
	// Build SQL queries to support searching by title and message content
	// Use DISTINCT to avoid duplication when a conversation has multiple matching messages
	query := `SELECT DISTINCT c.id, c.title, COALESCE(c.pinned, 0), c.created_at, c.updated_at, COALESCE(cgm.pinned, 0) as group_pinned
		 FROM conversations c
		 INNER JOIN conversation_group_mappings cgm ON c.id = cgm.conversation_id
		 WHERE cgm.group_id = ?`

	args := []interface{}{groupID}

	// If there are search keywords, add title and message content search conditions
	if searchQuery != "" {
		searchPattern := "%" + searchQuery + "%"
		// Search title or message content
		// Use LEFT JOIN to join the message table so that even conversations without messages are searchable (by title)
		query += ` AND (
			LOWER(c.title) LIKE LOWER(?)
			OR EXISTS (
				SELECT 1 FROM messages m 
				WHERE m.conversation_id = c.id 
				AND LOWER(m.content) LIKE LOWER(?)
			)
		)`
		args = append(args, searchPattern, searchPattern)
	}

	query += " ORDER BY COALESCE(cgm.pinned, 0) DESC, c.updated_at DESC"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("Search group conversation failed: %w", err)
	}
	defer rows.Close()

	var conversations []*Conversation
	for rows.Next() {
		var conv Conversation
		var createdAt, updatedAt string
		var pinned int
		var groupPinned int

		if err := rows.Scan(&conv.ID, &conv.Title, &pinned, &createdAt, &updatedAt, &groupPinned); err != nil {
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

// GetGroupByConversation Gets the group to which the conversation belongs
func (db *DB) GetGroupByConversation(conversationID string) (string, error) {
	var groupID string
	err := db.QueryRow(
		"SELECT group_id FROM conversation_group_mappings WHERE conversation_id = ? LIMIT 1",
		conversationID,
	).Scan(&groupID)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil // No grouping
		}
		return "", fmt.Errorf("Failed to query conversation grouping: %w", err)
	}
	return groupID, nil
}

// UpdateConversationPinned updates the conversation pinned status
func (db *DB) UpdateConversationPinned(id string, pinned bool) error {
	pinnedValue := 0
	if pinned {
		pinnedValue = 1
	}
	// NOTE: Do not update updated_at because pinning should not change the update time of the conversation
	_, err := db.Exec(
		"UPDATE conversations SET pinned = ? WHERE id = ?",
		pinnedValue, id,
	)
	if err != nil {
		return fmt.Errorf("Failed to update conversation top status: %w", err)
	}
	return nil
}

// UpdateGroupPinned updates the pinned status of the group
func (db *DB) UpdateGroupPinned(id string, pinned bool) error {
	pinnedValue := 0
	if pinned {
		pinnedValue = 1
	}
	_, err := db.Exec(
		"UPDATE conversation_groups SET pinned = ?, updated_at = ? WHERE id = ?",
		pinnedValue, time.Now(), id,
	)
	if err != nil {
		return fmt.Errorf("Failed to update group top status: %w", err)
	}
	return nil
}

// UpdateConversationPinnedInGroup updates the pinned status of the conversation in the group
func (db *DB) UpdateConversationPinnedInGroup(conversationID, groupID string, pinned bool) error {
	pinnedValue := 0
	if pinned {
		pinnedValue = 1
	}
	_, err := db.Exec(
		"UPDATE conversation_group_mappings SET pinned = ? WHERE conversation_id = ? AND group_id = ?",
		pinnedValue, conversationID, groupID,
	)
	if err != nil {
		return fmt.Errorf("Failed to update group conversation top status: %w", err)
	}
	return nil
}
