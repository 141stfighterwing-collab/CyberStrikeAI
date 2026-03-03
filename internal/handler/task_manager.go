package handler

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrTaskCancelled Error when user cancels task
var ErrTaskCancelled = errors.New("agent task cancelled by user")

// ErrTaskAlreadyRunning The session already has a task being executed.
var ErrTaskAlreadyRunning = errors.New("agent task already running for conversation")

// AgentTask describes the running Agent task
type AgentTask struct {
	ConversationID string    `json:"conversationId"`
	Message        string    `json:"message,omitempty"`
	StartedAt      time.Time `json:"startedAt"`
	Status         string    `json:"status"`

	cancel func(error)
}

// CompletedTask Completed task (for history)
type CompletedTask struct {
	ConversationID string    `json:"conversationId"`
	Message        string    `json:"message,omitempty"`
	StartedAt      time.Time `json:"startedAt"`
	CompletedAt    time.Time `json:"completedAt"`
	Status         string    `json:"status"`
}

// AgentTaskManager manages running Agent tasks
type AgentTaskManager struct {
	mu             sync.RWMutex
	tasks          map[string]*AgentTask
	completedTasks []*CompletedTask // History of recently completed tasks
	maxHistorySize int              // Maximum number of historical records
	historyRetention time.Duration  // History retention time
}

// NewAgentTaskManager creates a task manager
func NewAgentTaskManager() *AgentTaskManager {
	return &AgentTaskManager{
		tasks:            make(map[string]*AgentTask),
		completedTasks:   make([]*CompletedTask, 0),
		maxHistorySize:   50,                    // Keep up to 50 historical records
		historyRetention: 24 * time.Hour,       // Keep for 24 hours
	}
}

// StartTask registers and starts a new task
func (m *AgentTaskManager) StartTask(conversationID, message string, cancel context.CancelCauseFunc) (*AgentTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.tasks[conversationID]; exists {
		return nil, ErrTaskAlreadyRunning
	}

	task := &AgentTask{
		ConversationID: conversationID,
		Message:        message,
		StartedAt:      time.Now(),
		Status:         "running",
		cancel: func(err error) {
			if cancel != nil {
				cancel(err)
			}
		},
	}

	m.tasks[conversationID] = task
	return task, nil
}

// CancelTask ​​Cancels the task of the specified session
func (m *AgentTaskManager) CancelTask(conversationID string, cause error) (bool, error) {
	m.mu.Lock()
	task, exists := m.tasks[conversationID]
	if !exists {
		m.mu.Unlock()
		return false, nil
	}

	// If you are already in the cancellation process, return directly
	if task.Status == "cancelling" {
		m.mu.Unlock()
		return false, nil
	}

	task.Status = "cancelling"
	cancel := task.cancel
	m.mu.Unlock()

	if cause == nil {
		cause = ErrTaskCancelled
	}
	if cancel != nil {
		cancel(cause)
	}
	return true, nil
}

// UpdateTaskStatus updates the task status but does not delete the task (used to update the status before sending the event)
func (m *AgentTaskManager) UpdateTaskStatus(conversationID string, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	task, exists := m.tasks[conversationID]
	if !exists {
		return
	}

	if status != "" {
		task.Status = status
	}
}

// FinishTask Completes the task and removes it from the manager
func (m *AgentTaskManager) FinishTask(conversationID string, finalStatus string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	task, exists := m.tasks[conversationID]
	if !exists {
		return
	}

	if finalStatus != "" {
		task.Status = finalStatus
	}

	// Save to history
	completedTask := &CompletedTask{
		ConversationID: task.ConversationID,
		Message:        task.Message,
		StartedAt:       task.StartedAt,
		CompletedAt:     time.Now(),
		Status:          finalStatus,
	}
	
	// Add to history
	m.completedTasks = append(m.completedTasks, completedTask)
	
	// Clean up expired and excessive history
	m.cleanupHistory()

	// Remove from running tasks
	delete(m.tasks, conversationID)
}

// CleanupHistory cleans up expired history records
func (m *AgentTaskManager) cleanupHistory() {
	now := time.Now()
	cutoffTime := now.Add(-m.historyRetention)
	
	// Filter out expired records
	validTasks := make([]*CompletedTask, 0, len(m.completedTasks))
	for _, task := range m.completedTasks {
		if task.CompletedAt.After(cutoffTime) {
			validTasks = append(validTasks, task)
		}
	}
	
	// If it still exceeds the maximum number, only keep the latest
	if len(validTasks) > m.maxHistorySize {
		// Sort by completion time, keep the latest
		// Since it is appended, the latest one is at the end, so just take the last N ones.
		start := len(validTasks) - m.maxHistorySize
		validTasks = validTasks[start:]
	}
	
	m.completedTasks = validTasks
}

// GetActiveTasks returns all running tasks
func (m *AgentTaskManager) GetActiveTasks() []*AgentTask {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*AgentTask, 0, len(m.tasks))
	for _, task := range m.tasks {
		result = append(result, &AgentTask{
			ConversationID: task.ConversationID,
			Message:        task.Message,
			StartedAt:      task.StartedAt,
			Status:         task.Status,
		})
	}
	return result
}

// GetCompletedTasks returns the history of recently completed tasks
func (m *AgentTaskManager) GetCompletedTasks() []*CompletedTask {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	// Clean up expired records (read-only lock, does not affect other operations)
	// Note: cleanupHistory cannot be called directly here because a write lock is required.
	// So filter expired records when returning
	now := time.Now()
	cutoffTime := now.Add(-m.historyRetention)
	
	result := make([]*CompletedTask, 0, len(m.completedTasks))
	for _, task := range m.completedTasks {
		if task.CompletedAt.After(cutoffTime) {
			result = append(result, task)
		}
	}
	
	// Sort by completion time in descending order (newest first)
	// Since it is appended, the latest one is at the end and needs to be reversed.
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	
	// Limit the number of returns
	if len(result) > m.maxHistorySize {
		result = result[:m.maxHistorySize]
	}
	
	return result
}
