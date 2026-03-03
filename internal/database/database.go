package database

import (
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/mattn/go-sqlite3"
	"go.uber.org/zap"
)

// DB database connection
type DB struct {
	*sql.DB
	logger *zap.Logger
}

// NewDB creates a database connection
func NewDB(dbPath string, logger *zap.Logger) (*DB, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_foreign_keys=1")
	if err != nil {
		return nil, fmt.Errorf("Failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("Failed to connect to database: %w", err)
	}

	database := &DB{
		DB:     db,
		logger: logger,
	}

	// Initialization table
	if err := database.initTables(); err != nil {
		return nil, fmt.Errorf("Failed to initialize table: %w", err)
	}

	return database, nil
}

// InitTables initializes database tables
func (db *DB) initTables() error {
	// Create conversation form
	createConversationsTable := `
	CREATE TABLE IF NOT EXISTS conversations (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL,
		last_react_input TEXT,
		last_react_output TEXT
	);`

	// Create message table
	createMessagesTable := `
	CREATE TABLE IF NOT EXISTS messages (
		id TEXT PRIMARY KEY,
		conversation_id TEXT NOT NULL,
		role TEXT NOT NULL,
		content TEXT NOT NULL,
		mcp_execution_ids TEXT,
		created_at DATETIME NOT NULL,
		FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
	);`

	// Create process details table
	createProcessDetailsTable := `
	CREATE TABLE IF NOT EXISTS process_details (
		id TEXT PRIMARY KEY,
		message_id TEXT NOT NULL,
		conversation_id TEXT NOT NULL,
		event_type TEXT NOT NULL,
		message TEXT,
		data TEXT,
		created_at DATETIME NOT NULL,
		FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE,
		FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
	);`

	// Create tool execution record table
	createToolExecutionsTable := `
	CREATE TABLE IF NOT EXISTS tool_executions (
		id TEXT PRIMARY KEY,
		tool_name TEXT NOT NULL,
		arguments TEXT NOT NULL,
		status TEXT NOT NULL,
		result TEXT,
		error TEXT,
		start_time DATETIME NOT NULL,
		end_time DATETIME,
		duration_ms INTEGER,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);`

	// Create tool statistics table
	createToolStatsTable := `
	CREATE TABLE IF NOT EXISTS tool_stats (
		tool_name TEXT PRIMARY KEY,
		total_calls INTEGER NOT NULL DEFAULT 0,
		success_calls INTEGER NOT NULL DEFAULT 0,
		failed_calls INTEGER NOT NULL DEFAULT 0,
		last_call_time DATETIME,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);`

	// Create Skills statistics table
	createSkillStatsTable := `
	CREATE TABLE IF NOT EXISTS skill_stats (
		skill_name TEXT PRIMARY KEY,
		total_calls INTEGER NOT NULL DEFAULT 0,
		success_calls INTEGER NOT NULL DEFAULT 0,
		failed_calls INTEGER NOT NULL DEFAULT 0,
		last_call_time DATETIME,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);`

	// Create attack chain node table
	createAttackChainNodesTable := `
	CREATE TABLE IF NOT EXISTS attack_chain_nodes (
		id TEXT PRIMARY KEY,
		conversation_id TEXT NOT NULL,
		node_type TEXT NOT NULL,
		node_name TEXT NOT NULL,
		tool_execution_id TEXT,
		metadata TEXT,
		risk_score INTEGER DEFAULT 0,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE,
		FOREIGN KEY (tool_execution_id) REFERENCES tool_executions(id) ON DELETE SET NULL
	);`

	// Create an attack link table
	createAttackChainEdgesTable := `
	CREATE TABLE IF NOT EXISTS attack_chain_edges (
		id TEXT PRIMARY KEY,
		conversation_id TEXT NOT NULL,
		source_node_id TEXT NOT NULL,
		target_node_id TEXT NOT NULL,
		edge_type TEXT NOT NULL,
		weight INTEGER DEFAULT 1,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE,
		FOREIGN KEY (source_node_id) REFERENCES attack_chain_nodes(id) ON DELETE CASCADE,
		FOREIGN KEY (target_node_id) REFERENCES attack_chain_nodes(id) ON DELETE CASCADE
	);`

	// Create knowledge retrieval log table (retained in session database because of foreign key association)
	createKnowledgeRetrievalLogsTable := `
	CREATE TABLE IF NOT EXISTS knowledge_retrieval_logs (
		id TEXT PRIMARY KEY,
		conversation_id TEXT,
		message_id TEXT,
		query TEXT NOT NULL,
		risk_type TEXT,
		retrieved_items TEXT,
		created_at DATETIME NOT NULL,
		FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE SET NULL,
		FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE SET NULL
	);`

	// Create conversation grouping table
	createConversationGroupsTable := `
	CREATE TABLE IF NOT EXISTS conversation_groups (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		icon TEXT,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	);`

	// Create conversation grouping map
	createConversationGroupMappingsTable := `
	CREATE TABLE IF NOT EXISTS conversation_group_mappings (
		id TEXT PRIMARY KEY,
		conversation_id TEXT NOT NULL,
		group_id TEXT NOT NULL,
		created_at DATETIME NOT NULL,
		FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE,
		FOREIGN KEY (group_id) REFERENCES conversation_groups(id) ON DELETE CASCADE,
		UNIQUE(conversation_id, group_id)
	);`

	// Create vulnerability table
	createVulnerabilitiesTable := `
	CREATE TABLE IF NOT EXISTS vulnerabilities (
		id TEXT PRIMARY KEY,
		conversation_id TEXT NOT NULL,
		title TEXT NOT NULL,
		description TEXT,
		severity TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'open',
		vulnerability_type TEXT,
		target TEXT,
		proof TEXT,
		impact TEXT,
		recommendation TEXT,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
	);`

	// Create a batch task queue list
	createBatchTaskQueuesTable := `
	CREATE TABLE IF NOT EXISTS batch_task_queues (
		id TEXT PRIMARY KEY,
		title TEXT,
		status TEXT NOT NULL,
		created_at DATETIME NOT NULL,
		started_at DATETIME,
		completed_at DATETIME,
		current_index INTEGER NOT NULL DEFAULT 0
	);`

	// Create batch task list
	createBatchTasksTable := `
	CREATE TABLE IF NOT EXISTS batch_tasks (
		id TEXT PRIMARY KEY,
		queue_id TEXT NOT NULL,
		message TEXT NOT NULL,
		conversation_id TEXT,
		status TEXT NOT NULL,
		started_at DATETIME,
		completed_at DATETIME,
		error TEXT,
		result TEXT,
		FOREIGN KEY (queue_id) REFERENCES batch_task_queues(id) ON DELETE CASCADE
	);`

	// Create index
	createIndexes := `
	CREATE INDEX IF NOT EXISTS idx_messages_conversation_id ON messages(conversation_id);
	CREATE INDEX IF NOT EXISTS idx_conversations_updated_at ON conversations(updated_at);
	CREATE INDEX IF NOT EXISTS idx_process_details_message_id ON process_details(message_id);
	CREATE INDEX IF NOT EXISTS idx_process_details_conversation_id ON process_details(conversation_id);
	CREATE INDEX IF NOT EXISTS idx_tool_executions_tool_name ON tool_executions(tool_name);
	CREATE INDEX IF NOT EXISTS idx_tool_executions_start_time ON tool_executions(start_time);
	CREATE INDEX IF NOT EXISTS idx_tool_executions_status ON tool_executions(status);
	CREATE INDEX IF NOT EXISTS idx_chain_nodes_conversation ON attack_chain_nodes(conversation_id);
	CREATE INDEX IF NOT EXISTS idx_chain_edges_conversation ON attack_chain_edges(conversation_id);
	CREATE INDEX IF NOT EXISTS idx_chain_edges_source ON attack_chain_edges(source_node_id);
	CREATE INDEX IF NOT EXISTS idx_chain_edges_target ON attack_chain_edges(target_node_id);
	CREATE INDEX IF NOT EXISTS idx_knowledge_retrieval_logs_conversation ON knowledge_retrieval_logs(conversation_id);
	CREATE INDEX IF NOT EXISTS idx_knowledge_retrieval_logs_message ON knowledge_retrieval_logs(message_id);
	CREATE INDEX IF NOT EXISTS idx_knowledge_retrieval_logs_created_at ON knowledge_retrieval_logs(created_at);
	CREATE INDEX IF NOT EXISTS idx_conversation_group_mappings_conversation ON conversation_group_mappings(conversation_id);
	CREATE INDEX IF NOT EXISTS idx_conversation_group_mappings_group ON conversation_group_mappings(group_id);
	CREATE INDEX IF NOT EXISTS idx_conversations_pinned ON conversations(pinned);
	CREATE INDEX IF NOT EXISTS idx_vulnerabilities_conversation_id ON vulnerabilities(conversation_id);
	CREATE INDEX IF NOT EXISTS idx_vulnerabilities_severity ON vulnerabilities(severity);
	CREATE INDEX IF NOT EXISTS idx_vulnerabilities_status ON vulnerabilities(status);
	CREATE INDEX IF NOT EXISTS idx_vulnerabilities_created_at ON vulnerabilities(created_at);
	CREATE INDEX IF NOT EXISTS idx_batch_tasks_queue_id ON batch_tasks(queue_id);
	CREATE INDEX IF NOT EXISTS idx_batch_task_queues_created_at ON batch_task_queues(created_at);
	CREATE INDEX IF NOT EXISTS idx_batch_task_queues_title ON batch_task_queues(title);
	`

	if _, err := db.Exec(createConversationsTable); err != nil {
		return fmt.Errorf("Failed to create conversations table: %w", err)
	}

	if _, err := db.Exec(createMessagesTable); err != nil {
		return fmt.Errorf("Failed to create messages table: %w", err)
	}

	if _, err := db.Exec(createProcessDetailsTable); err != nil {
		return fmt.Errorf("Failed to create process_details table: %w", err)
	}

	if _, err := db.Exec(createToolExecutionsTable); err != nil {
		return fmt.Errorf("Failed to create tool_executions table: %w", err)
	}

	if _, err := db.Exec(createToolStatsTable); err != nil {
		return fmt.Errorf("Failed to create tool_stats table: %w", err)
	}

	if _, err := db.Exec(createSkillStatsTable); err != nil {
		return fmt.Errorf("Failed to create skill_stats table: %w", err)
	}

	if _, err := db.Exec(createAttackChainNodesTable); err != nil {
		return fmt.Errorf("Failed to create attack_chain_nodes table: %w", err)
	}

	if _, err := db.Exec(createAttackChainEdgesTable); err != nil {
		return fmt.Errorf("Failed to create attack_chain_edges table: %w", err)
	}

	if _, err := db.Exec(createKnowledgeRetrievalLogsTable); err != nil {
		return fmt.Errorf("Failed to create knowledge_retrieval_logs table: %w", err)
	}

	if _, err := db.Exec(createConversationGroupsTable); err != nil {
		return fmt.Errorf("Failed to create conversation_groups table: %w", err)
	}

	if _, err := db.Exec(createConversationGroupMappingsTable); err != nil {
		return fmt.Errorf("Failed to create conversation_group_mappings table: %w", err)
	}

	if _, err := db.Exec(createVulnerabilitiesTable); err != nil {
		return fmt.Errorf("Failed to create vulnerabilities table: %w", err)
	}

	if _, err := db.Exec(createBatchTaskQueuesTable); err != nil {
		return fmt.Errorf("Failed to create batch_task_queues table: %w", err)
	}

	if _, err := db.Exec(createBatchTasksTable); err != nil {
		return fmt.Errorf("Failed to create batch_tasks table: %w", err)
	}

	// Add a new field to an existing table if it does not exist - must be done before creating an index
	if err := db.migrateConversationsTable(); err != nil {
		db.logger.Warn("Failed to migrate conversations table", zap.Error(err))
		// No error is returned, allowing the run to continue
	}

	if err := db.migrateConversationGroupsTable(); err != nil {
		db.logger.Warn("Failed to migrate conversation_groups table", zap.Error(err))
		// No error is returned, allowing the run to continue
	}

	if err := db.migrateConversationGroupMappingsTable(); err != nil {
		db.logger.Warn("Failed to migrate conversation_group_mappings table", zap.Error(err))
		// No error is returned, allowing the run to continue
	}

	if err := db.migrateBatchTaskQueuesTable(); err != nil {
		db.logger.Warn("Migration of batch_task_queues table failed", zap.Error(err))
		// No error is returned, allowing the run to continue
	}

	if _, err := db.Exec(createIndexes); err != nil {
		return fmt.Errorf("Index creation failed: %w", err)
	}

	db.logger.Info("Database table initialization completed")
	return nil
}

// MigrateConversationsTable migrates the conversations table and adds new fields
func (db *DB) migrateConversationsTable() error {
	// Check if last_react_input field exists
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('conversations') WHERE name='last_react_input'").Scan(&count)
	if err != nil {
		// If query fails, try adding fields
		if _, addErr := db.Exec("ALTER TABLE conversations ADD COLUMN last_react_input TEXT"); addErr != nil {
			// If field already exists, ignore error (SQLite error message may be different)
			errMsg := strings.ToLower(addErr.Error())
			if !strings.Contains(errMsg, "duplicate column") && !strings.Contains(errMsg, "already exists") {
				db.logger.Warn("Adding last_react_input field failed", zap.Error(addErr))
			}
		}
	} else if count == 0 {
		// Field does not exist, add it
		if _, err := db.Exec("ALTER TABLE conversations ADD COLUMN last_react_input TEXT"); err != nil {
			db.logger.Warn("Adding last_react_input field failed", zap.Error(err))
		}
	}

	// Check if last_react_output field exists
	err = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('conversations') WHERE name='last_react_output'").Scan(&count)
	if err != nil {
		// If query fails, try adding fields
		if _, addErr := db.Exec("ALTER TABLE conversations ADD COLUMN last_react_output TEXT"); addErr != nil {
			// If field already exists, ignore error
			errMsg := strings.ToLower(addErr.Error())
			if !strings.Contains(errMsg, "duplicate column") && !strings.Contains(errMsg, "already exists") {
				db.logger.Warn("Adding last_react_output field failed", zap.Error(addErr))
			}
		}
	} else if count == 0 {
		// Field does not exist, add it
		if _, err := db.Exec("ALTER TABLE conversations ADD COLUMN last_react_output TEXT"); err != nil {
			db.logger.Warn("Adding last_react_output field failed", zap.Error(err))
		}
	}

	// Check if pinned field exists
	err = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('conversations') WHERE name='pinned'").Scan(&count)
	if err != nil {
		// If query fails, try adding fields
		if _, addErr := db.Exec("ALTER TABLE conversations ADD COLUMN pinned INTEGER DEFAULT 0"); addErr != nil {
			// If field already exists, ignore error
			errMsg := strings.ToLower(addErr.Error())
			if !strings.Contains(errMsg, "duplicate column") && !strings.Contains(errMsg, "already exists") {
				db.logger.Warn("Adding pinned field failed", zap.Error(addErr))
			}
		}
	} else if count == 0 {
		// Field does not exist, add it
		if _, err := db.Exec("ALTER TABLE conversations ADD COLUMN pinned INTEGER DEFAULT 0"); err != nil {
			db.logger.Warn("Adding pinned field failed", zap.Error(err))
		}
	}

	return nil
}

// MigrateConversationGroupsTable migrates the conversation_groups table and adds new fields
func (db *DB) migrateConversationGroupsTable() error {
	// Check if pinned field exists
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('conversation_groups') WHERE name='pinned'").Scan(&count)
	if err != nil {
		// If query fails, try adding fields
		if _, addErr := db.Exec("ALTER TABLE conversation_groups ADD COLUMN pinned INTEGER DEFAULT 0"); addErr != nil {
			// If field already exists, ignore error
			errMsg := strings.ToLower(addErr.Error())
			if !strings.Contains(errMsg, "duplicate column") && !strings.Contains(errMsg, "already exists") {
				db.logger.Warn("Adding pinned field failed", zap.Error(addErr))
			}
		}
	} else if count == 0 {
		// Field does not exist, add it
		if _, err := db.Exec("ALTER TABLE conversation_groups ADD COLUMN pinned INTEGER DEFAULT 0"); err != nil {
			db.logger.Warn("Adding pinned field failed", zap.Error(err))
		}
	}

	return nil
}

// MigrateConversationGroupMappingsTable migrates the conversation_group_mappings table and adds new fields
func (db *DB) migrateConversationGroupMappingsTable() error {
	// Check if pinned field exists
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('conversation_group_mappings') WHERE name='pinned'").Scan(&count)
	if err != nil {
		// If query fails, try adding fields
		if _, addErr := db.Exec("ALTER TABLE conversation_group_mappings ADD COLUMN pinned INTEGER DEFAULT 0"); addErr != nil {
			// If field already exists, ignore error
			errMsg := strings.ToLower(addErr.Error())
			if !strings.Contains(errMsg, "duplicate column") && !strings.Contains(errMsg, "already exists") {
				db.logger.Warn("Adding pinned field failed", zap.Error(addErr))
			}
		}
	} else if count == 0 {
		// Field does not exist, add it
		if _, err := db.Exec("ALTER TABLE conversation_group_mappings ADD COLUMN pinned INTEGER DEFAULT 0"); err != nil {
			db.logger.Warn("Adding pinned field failed", zap.Error(err))
		}
	}

	return nil
}

// MigrateBatchTaskQueuesTable migrates the batch_task_queues table and adds title and role fields
func (db *DB) migrateBatchTaskQueuesTable() error {
	// Check if title field exists
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('batch_task_queues') WHERE name='title'").Scan(&count)
	if err != nil {
		// If query fails, try adding fields
		if _, addErr := db.Exec("ALTER TABLE batch_task_queues ADD COLUMN title TEXT"); addErr != nil {
			// If field already exists, ignore error
			errMsg := strings.ToLower(addErr.Error())
			if !strings.Contains(errMsg, "duplicate column") && !strings.Contains(errMsg, "already exists") {
				db.logger.Warn("Failed to add title field", zap.Error(addErr))
			}
		}
	} else if count == 0 {
		// Field does not exist, add it
		if _, err := db.Exec("ALTER TABLE batch_task_queues ADD COLUMN title TEXT"); err != nil {
			db.logger.Warn("Failed to add title field", zap.Error(err))
		}
	}

	// Check if role field exists
	var roleCount int
	err = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('batch_task_queues') WHERE name='role'").Scan(&roleCount)
	if err != nil {
		// If query fails, try adding fields
		if _, addErr := db.Exec("ALTER TABLE batch_task_queues ADD COLUMN role TEXT"); addErr != nil {
			// If field already exists, ignore error
			errMsg := strings.ToLower(addErr.Error())
			if !strings.Contains(errMsg, "duplicate column") && !strings.Contains(errMsg, "already exists") {
				db.logger.Warn("Failed to add role field", zap.Error(addErr))
			}
		}
	} else if roleCount == 0 {
		// Field does not exist, add it
		if _, err := db.Exec("ALTER TABLE batch_task_queues ADD COLUMN role TEXT"); err != nil {
			db.logger.Warn("Failed to add role field", zap.Error(err))
		}
	}

	return nil
}

// NewKnowledgeDB creates a knowledge base database connection (only includes tables related to the knowledge base)
func NewKnowledgeDB(dbPath string, logger *zap.Logger) (*DB, error) {
	sqlDB, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_foreign_keys=1")
	if err != nil {
		return nil, fmt.Errorf("Failed to open knowledge base database: %w", err)
	}

	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("Failed to connect to knowledge base database: %w", err)
	}

	database := &DB{
		DB:     sqlDB,
		logger: logger,
	}

	// Initialize knowledge base table
	if err := database.initKnowledgeTables(); err != nil {
		return nil, fmt.Errorf("Failed to initialize knowledge base table: %w", err)
	}

	return database, nil
}

// InitKnowledgeTables initializes the knowledge base database tables (only tables related to the knowledge base)
func (db *DB) initKnowledgeTables() error {
	// Create a knowledge base item table
	createKnowledgeBaseItemsTable := `
	CREATE TABLE IF NOT EXISTS knowledge_base_items (
		id TEXT PRIMARY KEY,
		category TEXT NOT NULL,
		title TEXT NOT NULL,
		file_path TEXT NOT NULL,
		content TEXT,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	);`

	// Create a knowledge base vector table
	createKnowledgeEmbeddingsTable := `
	CREATE TABLE IF NOT EXISTS knowledge_embeddings (
		id TEXT PRIMARY KEY,
		item_id TEXT NOT NULL,
		chunk_index INTEGER NOT NULL,
		chunk_text TEXT NOT NULL,
		embedding TEXT NOT NULL,
		created_at DATETIME NOT NULL,
		FOREIGN KEY (item_id) REFERENCES knowledge_base_items(id) ON DELETE CASCADE
	);`

	// Create a knowledge retrieval log table (in a stand-alone knowledge base database, no foreign key constraints are used because the conversations and messages tables may not be in this database)
	createKnowledgeRetrievalLogsTable := `
	CREATE TABLE IF NOT EXISTS knowledge_retrieval_logs (
		id TEXT PRIMARY KEY,
		conversation_id TEXT,
		message_id TEXT,
		query TEXT NOT NULL,
		risk_type TEXT,
		retrieved_items TEXT,
		created_at DATETIME NOT NULL
	);`

	// Create index
	createIndexes := `
	CREATE INDEX IF NOT EXISTS idx_knowledge_items_category ON knowledge_base_items(category);
	CREATE INDEX IF NOT EXISTS idx_knowledge_embeddings_item_id ON knowledge_embeddings(item_id);
	CREATE INDEX IF NOT EXISTS idx_knowledge_retrieval_logs_conversation ON knowledge_retrieval_logs(conversation_id);
	CREATE INDEX IF NOT EXISTS idx_knowledge_retrieval_logs_message ON knowledge_retrieval_logs(message_id);
	CREATE INDEX IF NOT EXISTS idx_knowledge_retrieval_logs_created_at ON knowledge_retrieval_logs(created_at);
	`

	if _, err := db.Exec(createKnowledgeBaseItemsTable); err != nil {
		return fmt.Errorf("Failed to create knowledge_base_items table: %w", err)
	}

	if _, err := db.Exec(createKnowledgeEmbeddingsTable); err != nil {
		return fmt.Errorf("Failed to create knowledge_embeddings table: %w", err)
	}

	if _, err := db.Exec(createKnowledgeRetrievalLogsTable); err != nil {
		return fmt.Errorf("Failed to create knowledge_retrieval_logs table: %w", err)
	}

	if _, err := db.Exec(createIndexes); err != nil {
		return fmt.Errorf("Index creation failed: %w", err)
	}

	db.logger.Info("Knowledge base database table initialization completed")
	return nil
}

// Close closes the database connection
func (db *DB) Close() error {
	return db.DB.Close()
}
