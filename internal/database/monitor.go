package database

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"cyberstrike-ai/internal/mcp"

	"go.uber.org/zap"
)

// SaveToolExecution saves tool execution records
func (db *DB) SaveToolExecution(exec *mcp.ToolExecution) error {
	argsJSON, err := json.Marshal(exec.Arguments)
	if err != nil {
		db.logger.Warn("Failed to serialize execution parameters", zap.Error(err))
		argsJSON = []byte("{}")
	}

	var resultJSON sql.NullString
	if exec.Result != nil {
		resultBytes, err := json.Marshal(exec.Result)
		if err != nil {
			db.logger.Warn("Serialization execution result failed", zap.Error(err))
		} else {
			resultJSON = sql.NullString{String: string(resultBytes), Valid: true}
		}
	}

	var errorText sql.NullString
	if exec.Error != "" {
		errorText = sql.NullString{String: exec.Error, Valid: true}
	}

	var endTime sql.NullTime
	if exec.EndTime != nil {
		endTime = sql.NullTime{Time: *exec.EndTime, Valid: true}
	}

	var durationMs sql.NullInt64
	if exec.Duration > 0 {
		durationMs = sql.NullInt64{Int64: exec.Duration.Milliseconds(), Valid: true}
	}

	query := `
		INSERT OR REPLACE INTO tool_executions 
		(id, tool_name, arguments, status, result, error, start_time, end_time, duration_ms, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = db.Exec(query,
		exec.ID,
		exec.ToolName,
		string(argsJSON),
		exec.Status,
		resultJSON,
		errorText,
		exec.StartTime,
		endTime,
		durationMs,
		time.Now(),
	)

	if err != nil {
		db.logger.Error("Failed to save tool execution record", zap.Error(err), zap.String("executionId", exec.ID))
		return err
	}

	return nil
}

// CountToolExecutions counts the total number of tool execution records
func (db *DB) CountToolExecutions(status, toolName string) (int, error) {
	query := `SELECT COUNT(*) FROM tool_executions`
	args := []interface{}{}
	conditions := []string{}
	if status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, status)
	}
	if toolName != "" {
		// Supports partial matching (fuzzy search), case-insensitive
		conditions = append(conditions, "LOWER(tool_name) LIKE ?")
		args = append(args, "%"+strings.ToLower(toolName)+"%")
	}
	if len(conditions) > 0 {
		query += ` WHERE ` + conditions[0]
		for i := 1; i < len(conditions); i++ {
			query += ` AND ` + conditions[i]
		}
	}
	var count int
	err := db.QueryRow(query, args...).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// LoadToolExecutions loads all tool execution records (supports paging)
func (db *DB) LoadToolExecutions() ([]*mcp.ToolExecution, error) {
	return db.LoadToolExecutionsWithPagination(0, 1000, "", "")
}

// LoadToolExecutionsWithPagination paging loading tool execution record
// Limit: the maximum number of records returned, 0 means using the default value of 1000
// Offset: number of records to skip, used for paging
// Status: status filtering, an empty string means no filtering
// ToolName: Tool name filtering, an empty string means no filtering
func (db *DB) LoadToolExecutionsWithPagination(offset, limit int, status, toolName string) ([]*mcp.ToolExecution, error) {
	if limit <= 0 {
		limit = 1000 // Default limits
	}
	if limit > 10000 {
		limit = 10000 // Maximum limit to prevent loading too much data at once
	}

	query := `
		SELECT id, tool_name, arguments, status, result, error, start_time, end_time, duration_ms
		FROM tool_executions
	`
	args := []interface{}{}
	conditions := []string{}
	if status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, status)
	}
	if toolName != "" {
		// Supports partial matching (fuzzy search), case-insensitive
		conditions = append(conditions, "LOWER(tool_name) LIKE ?")
		args = append(args, "%"+strings.ToLower(toolName)+"%")
	}
	if len(conditions) > 0 {
		query += ` WHERE ` + conditions[0]
		for i := 1; i < len(conditions); i++ {
			query += ` AND ` + conditions[i]
		}
	}
	query += ` ORDER BY start_time DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var executions []*mcp.ToolExecution
	for rows.Next() {
		var exec mcp.ToolExecution
		var argsJSON string
		var resultJSON sql.NullString
		var errorText sql.NullString
		var endTime sql.NullTime
		var durationMs sql.NullInt64

		err := rows.Scan(
			&exec.ID,
			&exec.ToolName,
			&argsJSON,
			&exec.Status,
			&resultJSON,
			&errorText,
			&exec.StartTime,
			&endTime,
			&durationMs,
		)
		if err != nil {
			db.logger.Warn("Failed to load execution record", zap.Error(err))
			continue
		}

		// Parse parameters
		if err := json.Unmarshal([]byte(argsJSON), &exec.Arguments); err != nil {
			db.logger.Warn("Failed to parse execution parameters", zap.Error(err))
			exec.Arguments = make(map[string]interface{})
		}

		// Parse results
		if resultJSON.Valid && resultJSON.String != "" {
			var result mcp.ToolResult
			if err := json.Unmarshal([]byte(resultJSON.String), &result); err != nil {
				db.logger.Warn("Failed to parse execution results", zap.Error(err))
			} else {
				exec.Result = &result
			}
		}

		// Setting error
		if errorText.Valid {
			exec.Error = errorText.String
		}

		// Set end time
		if endTime.Valid {
			exec.EndTime = &endTime.Time
		}

		// Set duration
		if durationMs.Valid {
			exec.Duration = time.Duration(durationMs.Int64) * time.Millisecond
		}

		executions = append(executions, &exec)
	}

	return executions, nil
}

// GetToolExecution Gets a single tool execution record based on ID
func (db *DB) GetToolExecution(id string) (*mcp.ToolExecution, error) {
	query := `
		SELECT id, tool_name, arguments, status, result, error, start_time, end_time, duration_ms
		FROM tool_executions
		WHERE id = ?
	`

	row := db.QueryRow(query, id)

	var exec mcp.ToolExecution
	var argsJSON string
	var resultJSON sql.NullString
	var errorText sql.NullString
	var endTime sql.NullTime
	var durationMs sql.NullInt64

	err := row.Scan(
		&exec.ID,
		&exec.ToolName,
		&argsJSON,
		&exec.Status,
		&resultJSON,
		&errorText,
		&exec.StartTime,
		&endTime,
		&durationMs,
	)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(argsJSON), &exec.Arguments); err != nil {
		db.logger.Warn("Failed to parse execution parameters", zap.Error(err))
		exec.Arguments = make(map[string]interface{})
	}

	if resultJSON.Valid && resultJSON.String != "" {
		var result mcp.ToolResult
		if err := json.Unmarshal([]byte(resultJSON.String), &result); err != nil {
			db.logger.Warn("Failed to parse execution results", zap.Error(err))
		} else {
			exec.Result = &result
		}
	}

	if errorText.Valid {
		exec.Error = errorText.String
	}

	if endTime.Valid {
		exec.EndTime = &endTime.Time
	}

	if durationMs.Valid {
		exec.Duration = time.Duration(durationMs.Int64) * time.Millisecond
	}

	return &exec, nil
}

// DeleteToolExecution deletes tool execution records
func (db *DB) DeleteToolExecution(id string) error {
	query := `DELETE FROM tool_executions WHERE id = ?`
	_, err := db.Exec(query, id)
	if err != nil {
		db.logger.Error("Failed to delete tool execution record", zap.Error(err), zap.String("executionId", id))
		return err
	}
	return nil
}

// DeleteToolExecutions batch delete tool execution records
func (db *DB) DeleteToolExecutions(ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	// Placeholders for building IN queries
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := `DELETE FROM tool_executions WHERE id IN (` + strings.Join(placeholders, ",") + `)`
	_, err := db.Exec(query, args...)
	if err != nil {
		db.logger.Error("Batch deletion tool execution record failed", zap.Error(err), zap.Int("count", len(ids)))
		return err
	}
	return nil
}

// GetToolExecutionsByIds Gets tool execution records based on ID list (used to obtain statistical information before batch deletion)
func (db *DB) GetToolExecutionsByIds(ids []string) ([]*mcp.ToolExecution, error) {
	if len(ids) == 0 {
		return []*mcp.ToolExecution{}, nil
	}

	// Placeholders for building IN queries
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := `
		SELECT id, tool_name, arguments, status, result, error, start_time, end_time, duration_ms
		FROM tool_executions
		WHERE id IN (` + strings.Join(placeholders, ",") + `)
	`

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var executions []*mcp.ToolExecution
	for rows.Next() {
		var exec mcp.ToolExecution
		var argsJSON string
		var resultJSON sql.NullString
		var errorText sql.NullString
		var endTime sql.NullTime
		var durationMs sql.NullInt64

		err := rows.Scan(
			&exec.ID,
			&exec.ToolName,
			&argsJSON,
			&exec.Status,
			&resultJSON,
			&errorText,
			&exec.StartTime,
			&endTime,
			&durationMs,
		)
		if err != nil {
			db.logger.Warn("Failed to load execution record", zap.Error(err))
			continue
		}

		// Parse parameters
		if err := json.Unmarshal([]byte(argsJSON), &exec.Arguments); err != nil {
			db.logger.Warn("Failed to parse execution parameters", zap.Error(err))
			exec.Arguments = make(map[string]interface{})
		}

		// Parse results
		if resultJSON.Valid && resultJSON.String != "" {
			var result mcp.ToolResult
			if err := json.Unmarshal([]byte(resultJSON.String), &result); err != nil {
				db.logger.Warn("Failed to parse execution results", zap.Error(err))
			} else {
				exec.Result = &result
			}
		}

		// Setting error
		if errorText.Valid {
			exec.Error = errorText.String
		}

		// Set end time
		if endTime.Valid {
			exec.EndTime = &endTime.Time
		}

		// Set duration
		if durationMs.Valid {
			exec.Duration = time.Duration(durationMs.Int64) * time.Millisecond
		}

		executions = append(executions, &exec)
	}

	return executions, nil
}

// SaveToolStats Save tool statistics
func (db *DB) SaveToolStats(toolName string, stats *mcp.ToolStats) error {
	var lastCallTime sql.NullTime
	if stats.LastCallTime != nil {
		lastCallTime = sql.NullTime{Time: *stats.LastCallTime, Valid: true}
	}

	query := `
		INSERT OR REPLACE INTO tool_stats 
		(tool_name, total_calls, success_calls, failed_calls, last_call_time, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`

	_, err := db.Exec(query,
		toolName,
		stats.TotalCalls,
		stats.SuccessCalls,
		stats.FailedCalls,
		lastCallTime,
		time.Now(),
	)

	if err != nil {
		db.logger.Error("Failed to save tool statistics", zap.Error(err), zap.String("toolName", toolName))
		return err
	}

	return nil
}

// LoadToolStats loads all tool statistics
func (db *DB) LoadToolStats() (map[string]*mcp.ToolStats, error) {
	query := `
		SELECT tool_name, total_calls, success_calls, failed_calls, last_call_time
		FROM tool_stats
	`

	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := make(map[string]*mcp.ToolStats)
	for rows.Next() {
		var stat mcp.ToolStats
		var lastCallTime sql.NullTime

		err := rows.Scan(
			&stat.ToolName,
			&stat.TotalCalls,
			&stat.SuccessCalls,
			&stat.FailedCalls,
			&lastCallTime,
		)
		if err != nil {
			db.logger.Warn("Failed to load statistics", zap.Error(err))
			continue
		}

		if lastCallTime.Valid {
			stat.LastCallTime = &lastCallTime.Time
		}

		stats[stat.ToolName] = &stat
	}

	return stats, nil
}

// UpdateToolStats Update tool statistics (cumulative mode)
func (db *DB) UpdateToolStats(toolName string, totalCalls, successCalls, failedCalls int, lastCallTime *time.Time) error {
	var lastCallTimeSQL sql.NullTime
	if lastCallTime != nil {
		lastCallTimeSQL = sql.NullTime{Time: *lastCallTime, Valid: true}
	}

	query := `
		INSERT INTO tool_stats (tool_name, total_calls, success_calls, failed_calls, last_call_time, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(tool_name) DO UPDATE SET
			total_calls = total_calls + ?,
			success_calls = success_calls + ?,
			failed_calls = failed_calls + ?,
			last_call_time = COALESCE(?, last_call_time),
			updated_at = ?
	`

	_, err := db.Exec(query,
		toolName, totalCalls, successCalls, failedCalls, lastCallTimeSQL, time.Now(),
		totalCalls, successCalls, failedCalls, lastCallTimeSQL, time.Now(),
	)

	if err != nil {
		db.logger.Error("Failed to update tool statistics", zap.Error(err), zap.String("toolName", toolName))
		return err
	}

	return nil
}

// DecreaseToolStats Decrease tool statistics (used when deleting execution records)
// If the statistical information becomes 0, delete the statistical record
func (db *DB) DecreaseToolStats(toolName string, totalCalls, successCalls, failedCalls int) error {
	// Update statistics first
	query := `
		UPDATE tool_stats SET
			total_calls = CASE WHEN total_calls - ? < 0 THEN 0 ELSE total_calls - ? END,
			success_calls = CASE WHEN success_calls - ? < 0 THEN 0 ELSE success_calls - ? END,
			failed_calls = CASE WHEN failed_calls - ? < 0 THEN 0 ELSE failed_calls - ? END,
			updated_at = ?
		WHERE tool_name = ?
	`

	_, err := db.Exec(query, totalCalls, totalCalls, successCalls, successCalls, failedCalls, failedCalls, time.Now(), toolName)
	if err != nil {
		db.logger.Error("Failed to reduce tool statistics", zap.Error(err), zap.String("toolName", toolName))
		return err
	}

	// Check whether the updated total_calls is 0, if so, delete the statistical record
	checkQuery := `SELECT total_calls FROM tool_stats WHERE tool_name = ?`
	var newTotalCalls int
	err = db.QueryRow(checkQuery, toolName).Scan(&newTotalCalls)
	if err != nil {
		// If the query fails (the record does not exist), return directly
		return nil
	}

	// If total_calls is 0, delete the statistical record
	if newTotalCalls == 0 {
		deleteQuery := `DELETE FROM tool_stats WHERE tool_name = ?`
		_, err = db.Exec(deleteQuery, toolName)
		if err != nil {
			db.logger.Warn("Failed to delete zero statistics record", zap.Error(err), zap.String("toolName", toolName))
			// No error is returned because the main operation (updating statistics) was successful
		} else {
			db.logger.Info("Zero statistics records deleted", zap.String("toolName", toolName))
		}
	}

	return nil
}
