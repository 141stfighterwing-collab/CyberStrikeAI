package handler

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/mcp/builtin"
	"cyberstrike-ai/internal/skills"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// SafeTruncateString safely truncates strings to avoid truncation in the middle of UTF-8 characters
func safeTruncateString(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}

	// Convert string to rune slice to correctly count characters
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}

	// Truncate to maximum length
	truncated := string(runes[:maxLen])

	// Try truncating at punctuation or spaces to make the truncation more natural
	// Find a suitable breakpoint ahead of the truncation point (no more than 20% of the length)
	searchRange := maxLen / 5
	if searchRange > maxLen {
		searchRange = maxLen
	}
	breakChars := []rune("，。、 ,.;:!?！？/\\-_")
	bestBreakPos := len(runes[:maxLen])

	for i := bestBreakPos - 1; i >= bestBreakPos-searchRange && i >= 0; i-- {
		for _, breakChar := range breakChars {
			if runes[i] == breakChar {
				bestBreakPos = i + 1 // Break after punctuation
				goto found
			}
		}
	}

found:
	truncated = string(runes[:bestBreakPos])
	return truncated + "..."
}

// AgentHandler AgentHandler
type AgentHandler struct {
	agent            *agent.Agent
	db               *database.DB
	logger           *zap.Logger
	tasks            *AgentTaskManager
	batchTaskManager *BatchTaskManager
	config           *config.Config // Configuration reference, used to obtain role information
	knowledgeManager interface {    // Knowledge Base Manager Interface
		LogRetrieval(conversationID, messageID, query, riskType string, retrievedItems []string) error
	}
	skillsManager *skills.Manager // Skills Manager
}

// NewAgentHandler creates a new Agent handler
func NewAgentHandler(agent *agent.Agent, db *database.DB, cfg *config.Config, logger *zap.Logger) *AgentHandler {
	batchTaskManager := NewBatchTaskManager()
	batchTaskManager.SetDB(db)

	// Load all batch task queues from database
	if err := batchTaskManager.LoadFromDB(); err != nil {
		logger.Warn("Loading batch task queue from database failed", zap.Error(err))
	}

	return &AgentHandler{
		agent:            agent,
		db:               db,
		logger:           logger,
		tasks:            NewAgentTaskManager(),
		batchTaskManager: batchTaskManager,
		config:           cfg,
	}
}

// SetKnowledgeManager sets the knowledge base manager (used to record retrieval logs)
func (h *AgentHandler) SetKnowledgeManager(manager interface {
	LogRetrieval(conversationID, messageID, query, riskType string, retrievedItems []string) error
}) {
	h.knowledgeManager = manager
}

// SetSkillsManager Set Skills Manager
func (h *AgentHandler) SetSkillsManager(manager *skills.Manager) {
	h.skillsManager = manager
}

// ChatAttachment Chat attachment (file uploaded by user)
type ChatAttachment struct {
	FileName string `json:"fileName"` // File name
	Content  string `json:"content"`  // Text content or base64 (decoded or not determined by MimeType)
	MimeType string `json:"mimeType,omitempty"`
}

// ChatRequest chat request
type ChatRequest struct {
	Message        string            `json:"message" binding:"required"`
	ConversationID string            `json:"conversationId,omitempty"`
	Role           string            `json:"role,omitempty"` // Character name
	Attachments    []ChatAttachment  `json:"attachments,omitempty"`
}

const (
	maxAttachments     = 10
	chatUploadsDirName = "chat_uploads" // The root directory where conversation attachments are saved (relative to the current working directory)
)

// SaveAttachmentsToDateAndConversationDir saves attachments to chat_uploads/YYYY-MM-DD/{conversationID}/, returning the saving path of each file (in the same order as attachments)
// Use "_new" as the directory name when conversationID is empty (the new conversation does not yet have an ID)
func saveAttachmentsToDateAndConversationDir(attachments []ChatAttachment, conversationID string, logger *zap.Logger) (savedPaths []string, err error) {
	if len(attachments) == 0 {
		return nil, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("Failed to get current working directory: %w", err)
	}
	dateDir := filepath.Join(cwd, chatUploadsDirName, time.Now().Format("2006-01-02"))
	convDirName := strings.TrimSpace(conversationID)
	if convDirName == "" {
		convDirName = "_new"
	} else {
		convDirName = strings.ReplaceAll(convDirName, string(filepath.Separator), "_")
	}
	targetDir := filepath.Join(dateDir, convDirName)
	if err = os.MkdirAll(targetDir, 0755); err != nil {
		return nil, fmt.Errorf("Failed to create upload directory: %w", err)
	}
	savedPaths = make([]string, 0, len(attachments))
	for i, a := range attachments {
		raw, decErr := attachmentContentToBytes(a)
		if decErr != nil {
			return nil, fmt.Errorf("Attachment %s failed to decode: %w", a.FileName, decErr)
		}
		baseName := filepath.Base(a.FileName)
		if baseName == "" || baseName == "." {
			baseName = "file"
		}
		baseName = strings.ReplaceAll(baseName, string(filepath.Separator), "_")
		ext := filepath.Ext(baseName)
		nameNoExt := strings.TrimSuffix(baseName, ext)
		suffix := fmt.Sprintf("_%s_%s", time.Now().Format("150405"), shortRand(6))
		var unique string
		if ext != "" {
			unique = nameNoExt + suffix + ext
		} else {
			unique = baseName + suffix
		}
		fullPath := filepath.Join(targetDir, unique)
		if err = os.WriteFile(fullPath, raw, 0644); err != nil {
			return nil, fmt.Errorf("Failed to write file %s: %w", a.FileName, err)
		}
		absPath, _ := filepath.Abs(fullPath)
		savedPaths = append(savedPaths, absPath)
		if logger != nil {
			logger.Debug("Conversation attachment saved", zap.Int("index", i+1), zap.String("fileName", a.FileName), zap.String("path", absPath))
		}
	}
	return savedPaths, nil
}

func shortRand(n int) string {
	const letters = "0123456789abcdef"
	b := make([]byte, n)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = letters[int(b[i])%len(letters)]
	}
	return string(b)
}

func attachmentContentToBytes(a ChatAttachment) ([]byte, error) {
	content := a.Content
	if decoded, err := base64.StdEncoding.DecodeString(content); err == nil && len(decoded) > 0 {
		return decoded, nil
	}
	return []byte(content), nil
}

// UserMessageContentForStorage returns the user message content to be stored in the database: if there is an attachment, append the attachment name (and path) after the text. It can still be displayed after refreshing. When the conversation continues, the large model can also get the path from the history.
func userMessageContentForStorage(message string, attachments []ChatAttachment, savedPaths []string) string {
	if len(attachments) == 0 {
		return message
	}
	var b strings.Builder
	b.WriteString(message)
	for i, a := range attachments {
		b.WriteString("\n📎 ")
		b.WriteString(a.FileName)
		if i < len(savedPaths) && savedPaths[i] != "" {
			b.WriteString(": ")
			b.WriteString(savedPaths[i])
		}
	}
	return b.String()
}

// AppendAttachmentsToMessage only appends the save path of the attachment to the end of the user message, and no longer inlines the attachment content to avoid the context being too long.
func appendAttachmentsToMessage(msg string, attachments []ChatAttachment, savedPaths []string) string {
	if len(attachments) == 0 {
		return msg
	}
	var b strings.Builder
	b.WriteString(msg)
	b.WriteString("\n\n[The file uploaded by the user has been saved to the following path (please read the file content on demand instead of relying on inline content)]\n")
	for i, a := range attachments {
		if i < len(savedPaths) && savedPaths[i] != "" {
			b.WriteString(fmt.Sprintf("- %s: %s\n", a.FileName, savedPaths[i]))
		} else {
			b.WriteString(fmt.Sprintf("- %s: (The path is unknown, the save may fail)\n", a.FileName))
		}
	}
	return b.String()
}

// ChatResponse chat response
type ChatResponse struct {
	Response        string    `json:"response"`
	MCPExecutionIDs []string  `json:"mcpExecutionIds,omitempty"` // List of MCP call IDs executed in this conversation
	ConversationID  string    `json:"conversationId"`            // Conversation ID
	Time            time.Time `json:"time"`
}

// AgentLoop handles Agent Loop requests
func (h *AgentHandler) AgentLoop(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	h.logger.Info("Agent Loop request received",
		zap.String("message", req.Message),
		zap.String("conversationId", req.ConversationID),
	)

	// If there is no conversation ID, create a new conversation
	conversationID := req.ConversationID
	if conversationID == "" {
		title := safeTruncateString(req.Message, 50)
		conv, err := h.db.CreateConversation(title)
		if err != nil {
			h.logger.Error("Failed to create conversation", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		conversationID = conv.ID
	} else {
		// Verify that the conversation exists
		_, err := h.db.GetConversation(conversationID)
		if err != nil {
			h.logger.Error("Dialogue does not exist", zap.String("conversationId", conversationID), zap.Error(err))
			c.JSON(http.StatusNotFound, gin.H{"error": "Dialogue does not exist"})
			return
		}
	}

	// Prioritize attempts to restore historical context from saved ReAct data
	agentHistoryMessages, err := h.loadHistoryFromReActData(conversationID)
	if err != nil {
		h.logger.Warn("Loading historical messages from ReAct data failed, using message table", zap.Error(err))
		// Fallback to using database message tables
		historyMessages, err := h.db.GetMessages(conversationID)
		if err != nil {
			h.logger.Warn("Failed to obtain historical messages", zap.Error(err))
			agentHistoryMessages = []agent.ChatMessage{}
		} else {
			// Convert database messages to Agent message format
			agentHistoryMessages = make([]agent.ChatMessage, 0, len(historyMessages))
			for _, msg := range historyMessages {
				agentHistoryMessages = append(agentHistoryMessages, agent.ChatMessage{
					Role:    msg.Role,
					Content: msg.Content,
				})
			}
			h.logger.Info("Load historical messages from message table", zap.Int("count", len(agentHistoryMessages)))
		}
	} else {
		h.logger.Info("Recover historical context from ReAct data", zap.Int("count", len(agentHistoryMessages)))
	}

	// Verify the number of attachments (non-streaming)
	if len(req.Attachments) > maxAttachments {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("At most %d attachments", maxAttachments)})
		return
	}

	// Application role user prompt words and tool configuration
	finalMessage := req.Message
	var roleTools []string // Tool list for role configuration
	var roleSkills []string // A list of skills for character configuration (used to prompt the AI, but not hard-coded content)
	if req.Role != "" && req.Role != "Default" {
		if h.config.Roles != nil {
			if role, exists := h.config.Roles[req.Role]; exists && role.Enabled {
				// Apply user prompt words
				if role.UserPrompt != "" {
					finalMessage = role.UserPrompt + "\n\n" + req.Message
					h.logger.Info("Application role user prompt words", zap.String("role", req.Role))
				}
				// Get the tool list of role configuration (use the tools field first, backward compatible with the mcps field)
				if len(role.Tools) > 0 {
					roleTools = role.Tools
					h.logger.Info("List of tools configured using roles", zap.String("role", req.Role), zap.Int("toolCount", len(roleTools)))
				}
				// Get the list of skills configured by the character (used to prompt the AI ​​in the system prompt word, but do not hardcode the content)
				if len(role.Skills) > 0 {
					roleSkills = role.Skills
					h.logger.Info("If the character is configured with skills, the AI ​​will be prompted in the system prompt word", zap.String("role", req.Role), zap.Int("skillCount", len(roleSkills)), zap.Strings("skills", roleSkills))
				}
			}
		}
	}
	var savedPaths []string
	if len(req.Attachments) > 0 {
		savedPaths, err = saveAttachmentsToDateAndConversationDir(req.Attachments, conversationID, h.logger)
		if err != nil {
			h.logger.Error("Failed to save conversation attachment", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save uploaded file:" + err.Error()})
			return
		}
	}
	finalMessage = appendAttachmentsToMessage(finalMessage, req.Attachments, savedPaths)

	// Save user messages: When there are attachments, the name and path of the attachment are saved together. After refreshing, the large model can also get the path from the history when displaying and continuing the conversation.
	userContent := userMessageContentForStorage(req.Message, req.Attachments, savedPaths)
	_, err = h.db.AddMessage(conversationID, "user", userContent, nil)
	if err != nil {
		h.logger.Error("Failed to save user message", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save user message:" + err.Error()})
		return
	}

	// Execute Agent Loop, passing in historical messages and conversation IDs (use finalMessage containing role prompt words and role tool list)
	// Note: Skills will not be hard-coded to be injected, but the system prompt will prompt the AI ​​which skills are recommended for this role.
	result, err := h.agent.AgentLoopWithProgress(c.Request.Context(), finalMessage, agentHistoryMessages, conversationID, nil, roleTools, roleSkills)
	if err != nil {
		h.logger.Error("Agent Loop execution failed", zap.Error(err))

		// Even if the execution fails, try to save the ReAct data (if there is it in the result)
		if result != nil && (result.LastReActInput != "" || result.LastReActOutput != "") {
			if saveErr := h.db.SaveReActData(conversationID, result.LastReActInput, result.LastReActOutput); saveErr != nil {
				h.logger.Warn("Failed to save ReAct data of failed task", zap.Error(saveErr))
			} else {
				h.logger.Info("ReAct data for failed tasks saved", zap.String("conversationId", conversationID))
			}
		}

		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Save Assistant Reply
	_, err = h.db.AddMessage(conversationID, "assistant", result.Response, result.MCPExecutionIDs)
	if err != nil {
		h.logger.Error("Failed to save assistant message", zap.Error(err))
		// Even if the save fails, the response is returned but an error is logged
		// Because the AI ​​has already generated the reply, the user should be able to see it
	}

	// Save the input and output of the last round of ReAct
	if result.LastReActInput != "" || result.LastReActOutput != "" {
		if err := h.db.SaveReActData(conversationID, result.LastReActInput, result.LastReActOutput); err != nil {
			h.logger.Warn("Failed to save ReAct data", zap.Error(err))
		} else {
			h.logger.Info("ReAct data saved", zap.String("conversationId", conversationID))
		}
	}

	c.JSON(http.StatusOK, ChatResponse{
		Response:        result.Response,
		MCPExecutionIDs: result.MCPExecutionIDs,
		ConversationID:  conversationID,
		Time:            time.Now(),
	})
}

// ProcessMessageForRobot is for robots (Enterprise WeChat/DingTalk/Feishu) to call: the same execution path as /api/agent-loop/stream (including progressCallback, process details), only does not send SSE, and finally returns a complete reply
func (h *AgentHandler) ProcessMessageForRobot(ctx context.Context, conversationID, message, role string) (response string, convID string, err error) {
	if conversationID == "" {
		title := safeTruncateString(message, 50)
		conv, createErr := h.db.CreateConversation(title)
		if createErr != nil {
			return "", "", fmt.Errorf("Failed to create conversation: %w", createErr)
		}
		conversationID = conv.ID
	} else {
		if _, getErr := h.db.GetConversation(conversationID); getErr != nil {
			return "", "", fmt.Errorf("Dialogue does not exist")
		}
	}

	agentHistoryMessages, err := h.loadHistoryFromReActData(conversationID)
	if err != nil {
		historyMessages, getErr := h.db.GetMessages(conversationID)
		if getErr != nil {
			agentHistoryMessages = []agent.ChatMessage{}
		} else {
			agentHistoryMessages = make([]agent.ChatMessage, 0, len(historyMessages))
			for _, msg := range historyMessages {
				agentHistoryMessages = append(agentHistoryMessages, agent.ChatMessage{Role: msg.Role, Content: msg.Content})
			}
		}
	}

	finalMessage := message
	var roleTools, roleSkills []string
	if role != "" && role != "Default" && h.config.Roles != nil {
		if r, exists := h.config.Roles[role]; exists && r.Enabled {
			if r.UserPrompt != "" {
				finalMessage = r.UserPrompt + "\n\n" + message
			}
			roleTools = r.Tools
			roleSkills = r.Skills
		}
	}

	if _, err = h.db.AddMessage(conversationID, "user", message, nil); err != nil {
		return "", "", fmt.Errorf("Failed to save user message: %w", err)
	}

	// Consistent with agent-loop/stream: first create an assistant message placeholder and use progressCallback to write process details (without sending SSE)
	assistantMsg, err := h.db.AddMessage(conversationID, "assistant", "Processing...", nil)
	if err != nil {
		h.logger.Warn("Robot: Failed to create assistant message placeholder", zap.Error(err))
	}
	var assistantMessageID string
	if assistantMsg != nil {
		assistantMessageID = assistantMsg.ID
	}
	progressCallback := h.createProgressCallback(conversationID, assistantMessageID, nil)

	result, err := h.agent.AgentLoopWithProgress(ctx, finalMessage, agentHistoryMessages, conversationID, progressCallback, roleTools, roleSkills)
	if err != nil {
		errMsg := "Execution failed:" + err.Error()
		if assistantMessageID != "" {
			_, _ = h.db.Exec("UPDATE messages SET content = ? WHERE id = ?", errMsg, assistantMessageID)
			_ = h.db.AddProcessDetail(assistantMessageID, conversationID, "error", errMsg, nil)
		}
		return "", conversationID, err
	}

	// Update assistant message content and MCP execution ID (consistent with stream)
	if assistantMessageID != "" {
		mcpIDsJSON := ""
		if len(result.MCPExecutionIDs) > 0 {
			jsonData, _ := json.Marshal(result.MCPExecutionIDs)
			mcpIDsJSON = string(jsonData)
		}
		_, err = h.db.Exec(
			"UPDATE messages SET content = ?, mcp_execution_ids = ? WHERE id = ?",
			result.Response, mcpIDsJSON, assistantMessageID,
		)
		if err != nil {
			h.logger.Warn("Robot: Update Assistant message failed", zap.Error(err))
		}
	} else {
		if _, err = h.db.AddMessage(conversationID, "assistant", result.Response, result.MCPExecutionIDs); err != nil {
			h.logger.Warn("Robot: Failed to save assistant message", zap.Error(err))
		}
	}
	if result.LastReActInput != "" || result.LastReActOutput != "" {
		_ = h.db.SaveReActData(conversationID, result.LastReActInput, result.LastReActOutput)
	}
	return result.Response, conversationID, nil
}

// StreamEvent streaming event
type StreamEvent struct {
	Type    string      `json:"type"`    // conversation, progress, tool_call, tool_result, response, error, cancelled, done
	Message string      `json:"message"` // Show message
	Data    interface{} `json:"data,omitempty"`
}

// CreateProgressCallback creates a progress callback function to save processDetails
// SendEventFunc: Optional streaming event sending function, if nil, no streaming event will be sent
func (h *AgentHandler) createProgressCallback(conversationID, assistantMessageID string, sendEventFunc func(eventType, message string, data interface{})) agent.ProgressCallback {
	// Used to save the parameters in the tool_call event for use in tool_result
	toolCallCache := make(map[string]map[string]interface{}) // toolCallId -> arguments

	return func(eventType, message string, data interface{}) {
		// If sendEventFunc is provided, sends streaming events
		if sendEventFunc != nil {
			sendEventFunc(eventType, message, data)
		}

		// Save parameters in tool_call event
		if eventType == "tool_call" {
			if dataMap, ok := data.(map[string]interface{}); ok {
				toolName, _ := dataMap["toolName"].(string)
				if toolName == builtin.ToolSearchKnowledgeBase {
					if toolCallId, ok := dataMap["toolCallId"].(string); ok && toolCallId != "" {
						if argumentsObj, ok := dataMap["argumentsObj"].(map[string]interface{}); ok {
							toolCallCache[toolCallId] = argumentsObj
						}
					}
				}
			}
		}

		// Handle knowledge retrieval logging
		if eventType == "tool_result" && h.knowledgeManager != nil {
			if dataMap, ok := data.(map[string]interface{}); ok {
				toolName, _ := dataMap["toolName"].(string)
				if toolName == builtin.ToolSearchKnowledgeBase {
					// Extract search information
					query := ""
					riskType := ""
					var retrievedItems []string

					// First try to get parameters from tool_call cache
					if toolCallId, ok := dataMap["toolCallId"].(string); ok && toolCallId != "" {
						if cachedArgs, exists := toolCallCache[toolCallId]; exists {
							if q, ok := cachedArgs["query"].(string); ok && q != "" {
								query = q
							}
							if rt, ok := cachedArgs["risk_type"].(string); ok && rt != "" {
								riskType = rt
							}
							// Clean cache after use
							delete(toolCallCache, toolCallId)
						}
					}

					// If not in cache, try to extract from argumentsObj
					if query == "" {
						if arguments, ok := dataMap["argumentsObj"].(map[string]interface{}); ok {
							if q, ok := arguments["query"].(string); ok && q != "" {
								query = q
							}
							if rt, ok := arguments["risk_type"].(string); ok && rt != "" {
								riskType = rt
							}
						}
					}

					// If query is still empty, try to extract from result (from first line of result text)
					if query == "" {
						if result, ok := dataMap["result"].(string); ok && result != "" {
							// Try to extract the query content from the results (if the results contain "No knowledge related to query 'xxx' found")
							if strings.Contains(result, "Not found with query '") {
								start := strings.Index(result, "Not found with query '") + len("Not found with query '")
								end := strings.Index(result[start:], "'")
								if end > 0 {
									query = result[start : start+end]
								}
							}
						}
						// If still empty, use default value
						if query == "" {
							query = "Unknown query"
						}
					}

					// Extract the retrieved knowledge item ID from the tool results
					// Result format: "Found
					if result, ok := dataMap["result"].(string); ok && result != "" {
						// Try to extract knowledge item ID from metadata
						metadataMatch := strings.Index(result, "<!-- METADATA:")
						if metadataMatch > 0 {
							// Extract metadata JSON
							metadataStart := metadataMatch + len("<!-- METADATA: ")
							metadataEnd := strings.Index(result[metadataStart:], " -->")
							if metadataEnd > 0 {
								metadataJSON := result[metadataStart : metadataStart+metadataEnd]
								var metadata map[string]interface{}
								if err := json.Unmarshal([]byte(metadataJSON), &metadata); err == nil {
									if meta, ok := metadata["_metadata"].(map[string]interface{}); ok {
										if ids, ok := meta["retrievedItemIDs"].([]interface{}); ok {
											retrievedItems = make([]string, 0, len(ids))
											for _, id := range ids {
												if idStr, ok := id.(string); ok {
													retrievedItems = append(retrievedItems, idStr)
												}
											}
										}
									}
								}
							}
						}

						// If not extracted from the metadata, but the result contains "X items found", at least mark it as having results
						if len(retrievedItems) == 0 && strings.Contains(result, "Turn up") && !strings.Contains(result, "Not found") {
							// There are results, but the ID cannot be extracted accurately, use special tags
							retrievedItems = []string{"_has_results"}
						}
					}

					// Record retrieval log (asynchronous, non-blocking)
					go func() {
						if err := h.knowledgeManager.LogRetrieval(conversationID, assistantMessageID, query, riskType, retrievedItems); err != nil {
							h.logger.Warn("Record knowledge retrieval log failure", zap.Error(err))
						}
					}()

					// Add knowledge retrieval event to processDetails
					if assistantMessageID != "" {
						retrievalData := map[string]interface{}{
							"query":    query,
							"riskType": riskType,
							"toolName": toolName,
						}
						if err := h.db.AddProcessDetail(assistantMessageID, conversationID, "knowledge_retrieval", fmt.Sprintf("Retrieve knowledge: %s", query), retrievalData); err != nil {
							h.logger.Warn("Failed to save knowledge retrieval details", zap.Error(err))
						}
					}
				}
			}
		}

		// Save process details to the database (exclude response and done events, they will be processed separately later)
		if assistantMessageID != "" && eventType != "response" && eventType != "done" {
			if err := h.db.AddProcessDetail(assistantMessageID, conversationID, eventType, message, data); err != nil {
				h.logger.Warn("Failed to save process details", zap.Error(err), zap.String("eventType", eventType))
			}
		}
	}
}

// AgentLoopStream handles Agent Loop streaming requests
func (h *AgentHandler) AgentLoopStream(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// For streaming requests, errors in SSE format are also sent
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		event := StreamEvent{
			Type:    "error",
			Message: "Request parameter error:" + err.Error(),
		}
		eventJSON, _ := json.Marshal(event)
		fmt.Fprintf(c.Writer, "data: %s\n\n", eventJSON)
		c.Writer.Flush()
		return
	}

	h.logger.Info("Agent Loop streaming request received",
		zap.String("message", req.Message),
		zap.String("conversationId", req.ConversationID),
	)

	// Set SSE response headers
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no") // Disable nginx buffering

	// Send initial event
	// Used to track whether the client has disconnected
	clientDisconnected := false

	sendEvent := func(eventType, message string, data interface{}) {
		// If the client is disconnected, no more events are sent
		if clientDisconnected {
			return
		}

		// Check if the request context was canceled (client disconnected)
		select {
		case <-c.Request.Context().Done():
			clientDisconnected = true
			return
		default:
		}

		event := StreamEvent{
			Type:    eventType,
			Message: message,
			Data:    data,
		}
		eventJSON, _ := json.Marshal(event)

		// Attempts to write events and marks the client as disconnected if this fails
		if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", eventJSON); err != nil {
			clientDisconnected = true
			h.logger.Debug("Client disconnects and stops sending SSE events", zap.Error(err))
			return
		}

		// Flush the response or mark the client as disconnected if it fails
		if flusher, ok := c.Writer.(http.Flusher); ok {
			flusher.Flush()
		} else {
			c.Writer.Flush()
		}
	}

	// If there is no conversation ID, create a new conversation
	conversationID := req.ConversationID
	if conversationID == "" {
		title := safeTruncateString(req.Message, 50)
		conv, err := h.db.CreateConversation(title)
		if err != nil {
			h.logger.Error("Failed to create conversation", zap.Error(err))
			sendEvent("error", "Failed to create conversation:"+err.Error(), nil)
			return
		}
		conversationID = conv.ID
		sendEvent("conversation", "Session created", map[string]interface{}{
			"conversationId": conversationID,
		})
	} else {
		// Verify that the conversation exists
		_, err := h.db.GetConversation(conversationID)
		if err != nil {
			h.logger.Error("Dialogue does not exist", zap.String("conversationId", conversationID), zap.Error(err))
			sendEvent("error", "Dialogue does not exist", nil)
			return
		}
	}

	// Prioritize attempts to restore historical context from saved ReAct data
	agentHistoryMessages, err := h.loadHistoryFromReActData(conversationID)
	if err != nil {
		h.logger.Warn("Loading historical messages from ReAct data failed, using message table", zap.Error(err))
		// Fallback to using database message tables
		historyMessages, err := h.db.GetMessages(conversationID)
		if err != nil {
			h.logger.Warn("Failed to obtain historical messages", zap.Error(err))
			agentHistoryMessages = []agent.ChatMessage{}
		} else {
			// Convert database messages to Agent message format
			agentHistoryMessages = make([]agent.ChatMessage, 0, len(historyMessages))
			for _, msg := range historyMessages {
				agentHistoryMessages = append(agentHistoryMessages, agent.ChatMessage{
					Role:    msg.Role,
					Content: msg.Content,
				})
			}
			h.logger.Info("Load historical messages from message table", zap.Int("count", len(agentHistoryMessages)))
		}
	} else {
		h.logger.Info("Recover historical context from ReAct data", zap.Int("count", len(agentHistoryMessages)))
	}

	// Check the number of attachments
	if len(req.Attachments) > maxAttachments {
		sendEvent("error", fmt.Sprintf("At most %d attachments", maxAttachments), nil)
		return
	}

	// Application role user prompt words and tool configuration
	finalMessage := req.Message
	var roleTools []string // Tool list for role configuration
	if req.Role != "" && req.Role != "Default" {
		if h.config.Roles != nil {
			if role, exists := h.config.Roles[req.Role]; exists && role.Enabled {
				// Apply user prompt words
				if role.UserPrompt != "" {
					finalMessage = role.UserPrompt + "\n\n" + req.Message
					h.logger.Info("Application role user prompt words", zap.String("role", req.Role))
				}
				// Get the tool list of role configuration (use the tools field first, backward compatible with the mcps field)
				if len(role.Tools) > 0 {
					roleTools = role.Tools
					h.logger.Info("List of tools configured using roles", zap.String("role", req.Role), zap.Int("toolCount", len(roleTools)))
				} else if len(role.MCPs) > 0 {
					// Backward compatibility: if there is only mcps field, temporarily use the empty list (meaning to use all tools)
					// Because mcps is the MCP server name, not the tool list
					h.logger.Info("Role configuration uses old mcps fields, all tools will be used", zap.String("role", req.Role))
				}
				// Note: Character configuration skills are no longer hard-coded and injected. AI can be called on demand through the list_skills and read_skill tools.
				if len(role.Skills) > 0 {
					h.logger.Info("The role is configured with skills, and the AI ​​can be called on demand through tools", zap.String("role", req.Role), zap.Int("skillCount", len(role.Skills)), zap.Strings("skills", role.Skills))
				}
			}
		}
	}
	var savedPaths []string
	if len(req.Attachments) > 0 {
		savedPaths, err = saveAttachmentsToDateAndConversationDir(req.Attachments, conversationID, h.logger)
		if err != nil {
			h.logger.Error("Failed to save conversation attachment", zap.Error(err))
			sendEvent("error", "Failed to save uploaded file:"+err.Error(), nil)
			return
		}
	}
	// Append only attachment save path to finalMessage to avoid inlining file contents into large model context
	finalMessage = appendAttachmentsToMessage(finalMessage, req.Attachments, savedPaths)
	// If roleTools is empty, it means using all tools (default role or role without configured tools)

	// Save user messages: When there are attachments, the name and path of the attachment are saved together. After refreshing, the large model can also get the path from the history when displaying and continuing the conversation.
	userContent := userMessageContentForStorage(req.Message, req.Attachments, savedPaths)
	_, err = h.db.AddMessage(conversationID, "user", userContent, nil)
	if err != nil {
		h.logger.Error("Failed to save user message", zap.Error(err))
	}

	// Pre-create assistant messages to associate process details
	assistantMsg, err := h.db.AddMessage(conversationID, "assistant", "Processing...", nil)
	if err != nil {
		h.logger.Error("Failed to create assistant message", zap.Error(err))
// If creation fails, continue execution without saving process details
		assistantMsg = nil
	}

	// Create a progress callback function and save it to the database at the same time
	var assistantMessageID string
	if assistantMsg != nil {
		assistantMessageID = assistantMsg.ID
	}

	// Create a progress callback function and reuse unified logic
	progressCallback := h.createProgressCallback(conversationID, assistantMessageID, sendEvent)

	// Create an independent context for task execution and do not cancel it with the HTTP request
	// This way, even if the client disconnects (such as refreshing the page), the task can continue to execute.
	baseCtx, cancelWithCause := context.WithCancelCause(context.Background())
	taskCtx, timeoutCancel := context.WithTimeout(baseCtx, 600*time.Minute)
	defer timeoutCancel()
	defer cancelWithCause(nil)

	if _, err := h.tasks.StartTask(conversationID, req.Message, cancelWithCause); err != nil {
		var errorMsg string
		if errors.Is(err, ErrTaskAlreadyRunning) {
			errorMsg = "⚠️ There is already a task being executed in the current session. Please wait until the current task is completed or click the "Stop Task" button before trying again."
			sendEvent("error", errorMsg, map[string]interface{}{
				"conversationId": conversationID,
				"errorType":      "task_already_running",
			})
		} else {
			errorMsg = "❌ Unable to start task:" + err.Error()
			sendEvent("error", errorMsg, map[string]interface{}{
				"conversationId": conversationID,
				"errorType":      "task_start_failed",
			})
		}

		// Update the assistant message content and save error details to the database
		if assistantMessageID != "" {
			if _, updateErr := h.db.Exec(
				"UPDATE messages SET content = ? WHERE id = ?",
				errorMsg,
				assistantMessageID,
			); updateErr != nil {
				h.logger.Warn("Assistant message after updating error failed", zap.Error(updateErr))
			}
			// Save error details to database
			if err := h.db.AddProcessDetail(assistantMessageID, conversationID, "error", errorMsg, map[string]interface{}{
				"errorType": func() string {
					if errors.Is(err, ErrTaskAlreadyRunning) {
						return "task_already_running"
					}
					return "task_start_failed"
				}(),
			}); err != nil {
				h.logger.Warn("Failed to save error details", zap.Error(err))
			}
		}

		sendEvent("done", "", map[string]interface{}{
			"conversationId": conversationID,
		})
		return
	}

	taskStatus := "completed"
	defer h.tasks.FinishTask(conversationID, taskStatus)

	// Execute Agent Loop, passing in an independent context to ensure that the task will not be interrupted due to client disconnection (use finalMessage containing role prompt words and role tool list)
	sendEvent("progress", "Analyzing your request...", nil)
	// Note: Skills will not be hard-coded to be injected, but the system prompt will prompt the AI ​​which skills are recommended for this role.
	var roleSkills []string // A list of skills for character configuration (used to prompt the AI, but not hard-coded content)
	if req.Role != "" && req.Role != "Default" {
		if h.config.Roles != nil {
			if role, exists := h.config.Roles[req.Role]; exists && role.Enabled {
				if len(role.Skills) > 0 {
					roleSkills = role.Skills
				}
			}
		}
	}
	result, err := h.agent.AgentLoopWithProgress(taskCtx, finalMessage, agentHistoryMessages, conversationID, progressCallback, roleTools, roleSkills)
	if err != nil {
		h.logger.Error("Agent Loop execution failed", zap.Error(err))
		cause := context.Cause(baseCtx)

		// Check whether it was canceled by the user: the cause of the context is ErrTaskCancelled
		// If cause is ErrTaskCancelled, no matter what type of error it is (including context.Canceled), it is considered to be canceled by the user.
		// This correctly handles cancellations during API calls
		isCancelled := errors.Is(cause, ErrTaskCancelled)

		switch {
		case isCancelled:
			taskStatus = "cancelled"
			cancelMsg := "The task has been canceled by the user and subsequent operations have been stopped."

			// Update the task status before sending the event to ensure that the front end can see the status change in time
			h.tasks.UpdateTaskStatus(conversationID, taskStatus)

			if assistantMessageID != "" {
				if _, updateErr := h.db.Exec(
					"UPDATE messages SET content = ? WHERE id = ?",
					cancelMsg,
					assistantMessageID,
				); updateErr != nil {
					h.logger.Warn("Update Assistant message after cancellation failed", zap.Error(updateErr))
				}
				h.db.AddProcessDetail(assistantMessageID, conversationID, "cancelled", cancelMsg, nil)
			}

			// Even if the task is cancelled, try to save the ReAct data (if there is one in the result)
			if result != nil && (result.LastReActInput != "" || result.LastReActOutput != "") {
				if err := h.db.SaveReActData(conversationID, result.LastReActInput, result.LastReActOutput); err != nil {
					h.logger.Warn("Failed to save ReAct data of canceled task", zap.Error(err))
				} else {
					h.logger.Info("ReAct data for canceled tasks saved", zap.String("conversationId", conversationID))
				}
			}

			sendEvent("cancelled", cancelMsg, map[string]interface{}{
				"conversationId": conversationID,
				"messageId":      assistantMessageID,
			})
			sendEvent("done", "", map[string]interface{}{
				"conversationId": conversationID,
			})
			return
		case errors.Is(err, context.DeadlineExceeded) || errors.Is(cause, context.DeadlineExceeded):
			taskStatus = "timeout"
			timeoutMsg := "The task execution timed out and was automatically terminated."

			// Update the task status before sending the event to ensure that the front end can see the status change in time
			h.tasks.UpdateTaskStatus(conversationID, taskStatus)

			if assistantMessageID != "" {
				if _, updateErr := h.db.Exec(
					"UPDATE messages SET content = ? WHERE id = ?",
					timeoutMsg,
					assistantMessageID,
				); updateErr != nil {
					h.logger.Warn("Update assistant message failed after timeout", zap.Error(updateErr))
				}
				h.db.AddProcessDetail(assistantMessageID, conversationID, "timeout", timeoutMsg, nil)
			}

			// Even if the task times out, try to save the ReAct data (if there is one in the result)
			if result != nil && (result.LastReActInput != "" || result.LastReActOutput != "") {
				if err := h.db.SaveReActData(conversationID, result.LastReActInput, result.LastReActOutput); err != nil {
					h.logger.Warn("Failed to save ReAct data of timeout task", zap.Error(err))
				} else {
					h.logger.Info("ReAct data of timeout task saved", zap.String("conversationId", conversationID))
				}
			}

			sendEvent("error", timeoutMsg, map[string]interface{}{
				"conversationId": conversationID,
				"messageId":      assistantMessageID,
			})
			sendEvent("done", "", map[string]interface{}{
				"conversationId": conversationID,
			})
			return
		default:
			taskStatus = "failed"
			errorMsg := "Execution failed:" + err.Error()

			// Update the task status before sending the event to ensure that the front end can see the status change in time
			h.tasks.UpdateTaskStatus(conversationID, taskStatus)

			if assistantMessageID != "" {
				if _, updateErr := h.db.Exec(
					"UPDATE messages SET content = ? WHERE id = ?",
					errorMsg,
					assistantMessageID,
				); updateErr != nil {
					h.logger.Warn("Assistant message after failed update failed", zap.Error(updateErr))
				}
				h.db.AddProcessDetail(assistantMessageID, conversationID, "error", errorMsg, nil)
			}

			// Even if the task fails, try to save the ReAct data (if there is it in the result)
			if result != nil && (result.LastReActInput != "" || result.LastReActOutput != "") {
				if err := h.db.SaveReActData(conversationID, result.LastReActInput, result.LastReActOutput); err != nil {
					h.logger.Warn("Failed to save ReAct data of failed task", zap.Error(err))
				} else {
					h.logger.Info("ReAct data for failed tasks saved", zap.String("conversationId", conversationID))
				}
			}

			sendEvent("error", errorMsg, map[string]interface{}{
				"conversationId": conversationID,
				"messageId":      assistantMessageID,
			})
			sendEvent("done", "", map[string]interface{}{
				"conversationId": conversationID,
			})
		}
		return
	}

	// Update assistant message content
	if assistantMsg != nil {
		_, err = h.db.Exec(
			"UPDATE messages SET content = ?, mcp_execution_ids = ? WHERE id = ?",
			result.Response,
			func() string {
				if len(result.MCPExecutionIDs) > 0 {
					jsonData, _ := json.Marshal(result.MCPExecutionIDs)
					return string(jsonData)
				}
				return ""
			}(),
			assistantMessageID,
		)
		if err != nil {
			h.logger.Error("Update assistant message failed", zap.Error(err))
		}
	} else {
		// If creation failed before, create it now
		_, err = h.db.AddMessage(conversationID, "assistant", result.Response, result.MCPExecutionIDs)
		if err != nil {
			h.logger.Error("Failed to save assistant message", zap.Error(err))
		}
	}

	// Save the input and output of the last round of ReAct
	if result.LastReActInput != "" || result.LastReActOutput != "" {
		if err := h.db.SaveReActData(conversationID, result.LastReActInput, result.LastReActOutput); err != nil {
			h.logger.Warn("Failed to save ReAct data", zap.Error(err))
		} else {
			h.logger.Info("ReAct data saved", zap.String("conversationId", conversationID))
		}
	}

	// Send final response
	sendEvent("response", result.Response, map[string]interface{}{
		"mcpExecutionIds": result.MCPExecutionIDs,
		"conversationId":  conversationID,
		"messageId":       assistantMessageID, // Contains the message ID so that the front end can correlate process details
	})
	sendEvent("done", "", map[string]interface{}{
		"conversationId": conversationID,
	})
}

// CancelAgentLoop cancels the task being executed
func (h *AgentHandler) CancelAgentLoop(c *gin.Context) {
	var req struct {
		ConversationID string `json:"conversationId" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ok, err := h.tasks.CancelTask(req.ConversationID, ErrTaskCancelled)
	if err != nil {
		h.logger.Error("Failed to cancel task", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Executing task not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":         "cancelling",
		"conversationId": req.ConversationID,
		"message":        "A cancellation request has been submitted and the task will be stopped after the current step is completed.",
	})
}

// ListAgentTasks lists all running tasks
func (h *AgentHandler) ListAgentTasks(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"tasks": h.tasks.GetActiveTasks(),
	})
}

// ListCompletedTasks lists the history of recently completed tasks
func (h *AgentHandler) ListCompletedTasks(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"tasks": h.tasks.GetCompletedTasks(),
	})
}

// BatchTaskRequest batch task request
type BatchTaskRequest struct {
	Title string   `json:"title"`                    // Task title (optional)
	Tasks []string `json:"tasks" binding:"required"` // Task list, one task per line
	Role  string   `json:"role,omitempty"`           // Role name (optional, empty string indicates default role)
}

// CreateBatchQueue creates a batch task queue
func (h *AgentHandler) CreateBatchQueue(c *gin.Context) {
	var req BatchTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if len(req.Tasks) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Task list cannot be empty"})
		return
	}

	// Filter empty tasks
	validTasks := make([]string, 0, len(req.Tasks))
	for _, task := range req.Tasks {
		if task != "" {
			validTasks = append(validTasks, task)
		}
	}

	if len(validTasks) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No valid tasks"})
		return
	}

	queue := h.batchTaskManager.CreateBatchQueue(req.Title, req.Role, validTasks)
	c.JSON(http.StatusOK, gin.H{
		"queueId": queue.ID,
		"queue":   queue,
	})
}

// GetBatchQueue Gets the batch task queue
func (h *AgentHandler) GetBatchQueue(c *gin.Context) {
	queueID := c.Param("queueId")
	queue, exists := h.batchTaskManager.GetBatchQueue(queueID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Queue does not exist"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"queue": queue})
}

// ListBatchQueuesResponse batch task queue list response
type ListBatchQueuesResponse struct {
	Queues     []*BatchTaskQueue `json:"queues"`
	Total      int               `json:"total"`
	Page       int               `json:"page"`
	PageSize   int               `json:"page_size"`
	TotalPages int               `json:"total_pages"`
}

// ListBatchQueues lists all batch task queues (supports filtering and paging)
func (h *AgentHandler) ListBatchQueues(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "10")
	offsetStr := c.DefaultQuery("offset", "0")
	pageStr := c.Query("page")
	status := c.Query("status")
	keyword := c.Query("keyword")

	limit, _ := strconv.Atoi(limitStr)
	offset, _ := strconv.Atoi(offsetStr)
	page := 1

	// If the page parameter is provided, the page is used first to calculate the offset.
	if pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
			offset = (page - 1) * limit
		}
	}

	// Limit pageSize range
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	if offset < 0 {
		offset = 0
	}

	// The default status is "all"
	if status == "" {
		status = "all"
	}

	// Get queue list and total number
	queues, total, err := h.batchTaskManager.ListQueues(limit, offset, status, keyword)
	if err != nil {
		h.logger.Error("Failed to get batch task queue list", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Calculate total number of pages
	totalPages := (total + limit - 1) / limit
	if totalPages == 0 {
		totalPages = 1
	}

	// If you use offset to calculate the page, you need to recalculate it.
	if pageStr == "" {
		page = (offset / limit) + 1
	}

	response := ListBatchQueuesResponse{
		Queues:     queues,
		Total:      total,
		Page:       page,
		PageSize:   limit,
		TotalPages: totalPages,
	}

	c.JSON(http.StatusOK, response)
}

// StartBatchQueue starts executing the batch task queue
func (h *AgentHandler) StartBatchQueue(c *gin.Context) {
	queueID := c.Param("queueId")
	queue, exists := h.batchTaskManager.GetBatchQueue(queueID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Queue does not exist"})
		return
	}

	if queue.Status != "pending" && queue.Status != "paused" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Queue status does not allow startup"})
		return
	}

	// Execute batch tasks in the background
	go h.executeBatchQueue(queueID)

	h.batchTaskManager.UpdateQueueStatus(queueID, "running")
	c.JSON(http.StatusOK, gin.H{"message": "Batch task has started execution", "queueId": queueID})
}

// PauseBatchQueue Pauses the batch task queue
func (h *AgentHandler) PauseBatchQueue(c *gin.Context) {
	queueID := c.Param("queueId")
	success := h.batchTaskManager.PauseQueue(queueID)
	if !success {
		c.JSON(http.StatusNotFound, gin.H{"error": "Queue does not exist or cannot be paused"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Batch task is paused"})
}

// DeleteBatchQueue deletes the batch task queue
func (h *AgentHandler) DeleteBatchQueue(c *gin.Context) {
	queueID := c.Param("queueId")
	success := h.batchTaskManager.DeleteQueue(queueID)
	if !success {
		c.JSON(http.StatusNotFound, gin.H{"error": "Queue does not exist"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "The batch task queue has been deleted"})
}

// UpdateBatchTask updates batch task messages
func (h *AgentHandler) UpdateBatchTask(c *gin.Context) {
	queueID := c.Param("queueId")
	taskID := c.Param("taskId")

	var req struct {
		Message string `json:"message" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request parameter:" + err.Error()})
		return
	}

	if req.Message == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Task message cannot be empty"})
		return
	}

	err := h.batchTaskManager.UpdateTaskMessage(queueID, taskID, req.Message)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Return updated queue information
	queue, exists := h.batchTaskManager.GetBatchQueue(queueID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Queue does not exist"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Task updated", "queue": queue})
}

// AddBatchTask adds a task to the batch task queue
func (h *AgentHandler) AddBatchTask(c *gin.Context) {
	queueID := c.Param("queueId")

	var req struct {
		Message string `json:"message" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request parameter:" + err.Error()})
		return
	}

	if req.Message == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Task message cannot be empty"})
		return
	}

	task, err := h.batchTaskManager.AddTaskToQueue(queueID, req.Message)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Return updated queue information
	queue, exists := h.batchTaskManager.GetBatchQueue(queueID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Queue does not exist"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Task has been added", "task": task, "queue": queue})
}

// DeleteBatchTask Delete batch task
func (h *AgentHandler) DeleteBatchTask(c *gin.Context) {
	queueID := c.Param("queueId")
	taskID := c.Param("taskId")

	err := h.batchTaskManager.DeleteTask(queueID, taskID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Return updated queue information
	queue, exists := h.batchTaskManager.GetBatchQueue(queueID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Queue does not exist"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Task deleted", "queue": queue})
}

// ExecuteBatchQueue executes batch task queue
func (h *AgentHandler) executeBatchQueue(queueID string) {
	h.logger.Info("Start executing batch task queue", zap.String("queueId", queueID))

	for {
		// Check queue status
		queue, exists := h.batchTaskManager.GetBatchQueue(queueID)
		if !exists || queue.Status == "cancelled" || queue.Status == "completed" || queue.Status == "paused" {
			break
		}

		// Get next task
		task, hasNext := h.batchTaskManager.GetNextTask(queueID)
		if !hasNext {
			// All tasks completed
			h.batchTaskManager.UpdateQueueStatus(queueID, "completed")
			h.logger.Info("Batch task queue execution completed", zap.String("queueId", queueID))
			break
		}

		// Update task status to running
		h.batchTaskManager.UpdateTaskStatus(queueID, task.ID, "running", "", "")

		// Create new conversation
		title := safeTruncateString(task.Message, 50)
		conv, err := h.db.CreateConversation(title)
		var conversationID string
		if err != nil {
			h.logger.Error("Failed to create conversation", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.Error(err))
			h.batchTaskManager.UpdateTaskStatus(queueID, task.ID, "failed", "", "Failed to create conversation:"+err.Error())
			h.batchTaskManager.MoveToNextTask(queueID)
			continue
		}
		conversationID = conv.ID

		// Save the conversationId to the task (save it even in the running state to view the conversation)
		h.batchTaskManager.UpdateTaskStatusWithConversationID(queueID, task.ID, "running", "", "", conversationID)

		// Application role user prompt words and tool configuration
		finalMessage := task.Message
		var roleTools []string // Tool list for role configuration
		var roleSkills []string // A list of skills for character configuration (used to prompt the AI, but not hard-coded content)
		if queue.Role != "" && queue.Role != "Default" {
			if h.config.Roles != nil {
				if role, exists := h.config.Roles[queue.Role]; exists && role.Enabled {
					// Apply user prompt words
					if role.UserPrompt != "" {
						finalMessage = role.UserPrompt + "\n\n" + task.Message
						h.logger.Info("Application role user prompt words", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.String("role", queue.Role))
					}
					// Get the tool list of role configuration (use the tools field first, backward compatible with the mcps field)
					if len(role.Tools) > 0 {
						roleTools = role.Tools
						h.logger.Info("List of tools configured using roles", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.String("role", queue.Role), zap.Int("toolCount", len(roleTools)))
					}
					// Get the list of skills configured by the character (used to prompt the AI ​​in the system prompt word, but do not hardcode the content)
					if len(role.Skills) > 0 {
						roleSkills = role.Skills
						h.logger.Info("If the character is configured with skills, the AI ​​will be prompted in the system prompt word", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.String("role", queue.Role), zap.Int("skillCount", len(roleSkills)), zap.Strings("skills", roleSkills))
					}
				}
			}
		}

		// Save user message (save the original message, excluding role prompt words)
		_, err = h.db.AddMessage(conversationID, "user", task.Message, nil)
		if err != nil {
			h.logger.Error("Failed to save user message", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.String("conversationId", conversationID), zap.Error(err))
		}

		// Pre-create assistant messages to associate process details
		assistantMsg, err := h.db.AddMessage(conversationID, "assistant", "Processing...", nil)
		if err != nil {
			h.logger.Error("Failed to create assistant message", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.String("conversationId", conversationID), zap.Error(err))
			// If creation fails, execution continues without saving process details
			assistantMsg = nil
		}

		// Create a progress callback function and reuse unified logic (batch tasks do not require streaming events, so pass in nil)
		var assistantMessageID string
		if assistantMsg != nil {
			assistantMessageID = assistantMsg.ID
		}
		progressCallback := h.createProgressCallback(conversationID, assistantMessageID, nil)

		// Execute the task (using finalMessage containing the role prompt word and the role tool list)
		h.logger.Info("Execute batch tasks", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.String("message", task.Message), zap.String("role", queue.Role), zap.String("conversationId", conversationID))

		// Single subtask timeout: adjusted from 30 minutes to 6 hours to adapt to long-term penetration/scanning tasks
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
		// Store the cancellation function so that the current task can be canceled when the queue is canceled
		h.batchTaskManager.SetTaskCancel(queueID, cancel)
		// Use the role tool list configured by the queue (if empty, means use all tools)
		// Note: Skills will not be hard-coded to be injected, but the system prompt will prompt the AI ​​which skills are recommended for this role.
		result, err := h.agent.AgentLoopWithProgress(ctx, finalMessage, []agent.ChatMessage{}, conversationID, progressCallback, roleTools, roleSkills)
		// Task execution is completed, clean up the cancellation function
		h.batchTaskManager.SetTaskCancel(queueID, nil)
		cancel()

		if err != nil {
			// Check if it is a cancellation error
			// 1. Directly check whether it is context.Canceled (including wrapped errors)
			// 2. Check whether the error message contains the "context canceled" or "cancelled" keywords
			// 3. Check whether result.Response contains cancellation-related messages
			errStr := err.Error()
			isCancelled := errors.Is(err, context.Canceled) ||
				strings.Contains(strings.ToLower(errStr), "context canceled") ||
				strings.Contains(strings.ToLower(errStr), "context cancelled") ||
				(result != nil && result.Response != "" && (strings.Contains(result.Response, "Task has been canceled") || strings.Contains(result.Response, "Task execution interrupted")))

			if isCancelled {
				h.logger.Info("Batch task canceled", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.String("conversationId", conversationID))
				cancelMsg := "The task has been canceled by the user and subsequent operations have been stopped."
				// If there is a more specific cancellation message in the result, use it
				if result != nil && result.Response != "" && (strings.Contains(result.Response, "Task has been canceled") || strings.Contains(result.Response, "Task execution interrupted")) {
					cancelMsg = result.Response
				}
				// Update assistant message content
				if assistantMessageID != "" {
					if _, updateErr := h.db.Exec(
						"UPDATE messages SET content = ? WHERE id = ?",
						cancelMsg,
						assistantMessageID,
					); updateErr != nil {
						h.logger.Warn("Update Assistant message after cancellation failed", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.Error(updateErr))
					}
					// Save cancellation details to database
					if err := h.db.AddProcessDetail(assistantMessageID, conversationID, "cancelled", cancelMsg, nil); err != nil {
						h.logger.Warn("Failed to save cancellation details", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.Error(err))
					}
				} else {
					// If there is no pre-created helper message, create a new one
					_, errMsg := h.db.AddMessage(conversationID, "assistant", cancelMsg, nil)
					if errMsg != nil {
						h.logger.Warn("Failed to save cancellation message", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.Error(errMsg))
					}
				}
				// Save ReAct data if present
				if result != nil && (result.LastReActInput != "" || result.LastReActOutput != "") {
					if err := h.db.SaveReActData(conversationID, result.LastReActInput, result.LastReActOutput); err != nil {
						h.logger.Warn("Failed to save ReAct data of canceled task", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.Error(err))
					}
				}
				h.batchTaskManager.UpdateTaskStatusWithConversationID(queueID, task.ID, "cancelled", cancelMsg, "", conversationID)
			} else {
				h.logger.Error("Batch task execution failed", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.String("conversationId", conversationID), zap.Error(err))
				errorMsg := "Execution failed:" + err.Error()
				// Update assistant message content
				if assistantMessageID != "" {
					if _, updateErr := h.db.Exec(
						"UPDATE messages SET content = ? WHERE id = ?",
						errorMsg,
						assistantMessageID,
					); updateErr != nil {
						h.logger.Warn("Assistant message after failed update failed", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.Error(updateErr))
					}
					// Save error details to database
					if err := h.db.AddProcessDetail(assistantMessageID, conversationID, "error", errorMsg, nil); err != nil {
						h.logger.Warn("Failed to save error details", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.Error(err))
					}
				}
				h.batchTaskManager.UpdateTaskStatus(queueID, task.ID, "failed", "", err.Error())
			}
		} else {
			h.logger.Info("Batch task executed successfully", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.String("conversationId", conversationID))

			// Update assistant message content
			if assistantMessageID != "" {
				mcpIDsJSON := ""
				if len(result.MCPExecutionIDs) > 0 {
					jsonData, _ := json.Marshal(result.MCPExecutionIDs)
					mcpIDsJSON = string(jsonData)
				}
				if _, updateErr := h.db.Exec(
					"UPDATE messages SET content = ?, mcp_execution_ids = ? WHERE id = ?",
					result.Response,
					mcpIDsJSON,
					assistantMessageID,
				); updateErr != nil {
					h.logger.Warn("Update assistant message failed", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.Error(updateErr))
					// If the update fails, try creating a new message
					_, err = h.db.AddMessage(conversationID, "assistant", result.Response, result.MCPExecutionIDs)
					if err != nil {
						h.logger.Error("Failed to save assistant message", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.String("conversationId", conversationID), zap.Error(err))
					}
				}
			} else {
				// If there is no pre-created helper message, create a new one
				_, err = h.db.AddMessage(conversationID, "assistant", result.Response, result.MCPExecutionIDs)
				if err != nil {
					h.logger.Error("Failed to save assistant message", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.String("conversationId", conversationID), zap.Error(err))
				}
			}

			// Save ReAct data
			if result.LastReActInput != "" || result.LastReActOutput != "" {
				if err := h.db.SaveReActData(conversationID, result.LastReActInput, result.LastReActOutput); err != nil {
					h.logger.Warn("Failed to save ReAct data", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.Error(err))
				} else {
					h.logger.Info("ReAct data saved", zap.String("queueId", queueID), zap.String("taskId", task.ID), zap.String("conversationId", conversationID))
				}
			}

			// Save results
			h.batchTaskManager.UpdateTaskStatusWithConversationID(queueID, task.ID, "completed", result.Response, "", conversationID)
		}

		// Move to next task
		h.batchTaskManager.MoveToNextTask(queueID)

		// Check if canceled or suspended
		queue, _ = h.batchTaskManager.GetBatchQueue(queueID)
		if queue.Status == "cancelled" || queue.Status == "paused" {
			break
		}
	}
}

// LoadHistoryFromReActData Restore historical message context from saved ReAct data
// Adopt splicing logic similar to attack chain generation: use the saved last_react_input and last_react_output first, and fall back to the message table if they do not exist.
func (h *AgentHandler) loadHistoryFromReActData(conversationID string) ([]agent.ChatMessage, error) {
	// Get saved ReAct input and output
	reactInputJSON, reactOutput, err := h.db.GetReActData(conversationID)
	if err != nil {
		return nil, fmt.Errorf("Failed to obtain ReAct data: %w", err)
	}

	// If last_react_input is empty, fall back to using the message table (consistent with the attack chain generation logic)
	if reactInputJSON == "" {
		return nil, fmt.Errorf("ReAct data is empty, message table will be used")
	}

	dataSource := "database_last_react_input"

	// Parse messages array in JSON format
	var messagesArray []map[string]interface{}
	if err := json.Unmarshal([]byte(reactInputJSON), &messagesArray); err != nil {
		return nil, fmt.Errorf("Failed to parse ReAct input JSON: %w", err)
	}

	messageCount := len(messagesArray)

	h.logger.Info("Restore historical context using saved ReAct data",
		zap.String("conversationId", conversationID),
		zap.String("dataSource", dataSource),
		zap.Int("reactInputSize", len(reactInputJSON)),
		zap.Int("messageCount", messageCount),
		zap.Int("reactOutputSize", len(reactOutput)),
	)
	// fmt.Println("messagesArray:", messagesArray)//debug

	// Convert to Agent message format
	agentMessages := make([]agent.ChatMessage, 0, len(messagesArray))
	for _, msgMap := range messagesArray {
		msg := agent.ChatMessage{}

		// Parse role
		if role, ok := msgMap["role"].(string); ok {
			msg.Role = role
		} else {
			continue // Skip invalid messages
		}

		// Skip system messages (AgentLoop will add them again)
		if msg.Role == "system" {
			continue
		}

		// Parse content
		if content, ok := msgMap["content"].(string); ok {
			msg.Content = content
		}

		// Parse tool_calls if present
		if toolCallsRaw, ok := msgMap["tool_calls"]; ok && toolCallsRaw != nil {
			if toolCallsArray, ok := toolCallsRaw.([]interface{}); ok {
				msg.ToolCalls = make([]agent.ToolCall, 0, len(toolCallsArray))
				for _, tcRaw := range toolCallsArray {
					if tcMap, ok := tcRaw.(map[string]interface{}); ok {
						toolCall := agent.ToolCall{}

						// Parse ID
						if id, ok := tcMap["id"].(string); ok {
							toolCall.ID = id
						}

						// Parse Type
						if toolType, ok := tcMap["type"].(string); ok {
							toolCall.Type = toolType
						}

						// Parse Function
						if funcMap, ok := tcMap["function"].(map[string]interface{}); ok {
							toolCall.Function = agent.FunctionCall{}

							// Parse function name
							if name, ok := funcMap["name"].(string); ok {
								toolCall.Function.Name = name
							}

							// Parse arguments (may be strings or objects)
							if argsRaw, ok := funcMap["arguments"]; ok {
								if argsStr, ok := argsRaw.(string); ok {
									// If it is a string, parse it as JSON
									var argsMap map[string]interface{}
									if err := json.Unmarshal([]byte(argsStr), &argsMap); err == nil {
										toolCall.Function.Arguments = argsMap
									}
								} else if argsMap, ok := argsRaw.(map[string]interface{}); ok {
									// If it is already an object, use it directly
									toolCall.Function.Arguments = argsMap
								}
							}
						}

						if toolCall.ID != "" {
							msg.ToolCalls = append(msg.ToolCalls, toolCall)
						}
					}
				}
			}
		}

		// Parse tool_call_id (tool role message)
		if toolCallID, ok := msgMap["tool_call_id"].(string); ok {
			msg.ToolCallID = toolCallID
		}

		agentMessages = append(agentMessages, msg)
	}

	// If last_react_output exists, it needs to be the last assistant message
	// Because last_react_input is saved before the iteration starts and does not contain the final output of the last round
	if reactOutput != "" {
		// Check if the last message is an assistant message and has no tool_calls
		// If there are tool_calls, it means that there should be tool messages and the final assistant reply later.
		if len(agentMessages) > 0 {
			lastMsg := &agentMessages[len(agentMessages)-1]
			if strings.EqualFold(lastMsg.Role, "assistant") && len(lastMsg.ToolCalls) == 0 {
				// The last one is an assistant message without tool_calls, and its content is updated with the final output.
				lastMsg.Content = reactOutput
			} else {
				// The last one is not an assistant message, or there are tool_calls, add the final output as a new assistant message
				agentMessages = append(agentMessages, agent.ChatMessage{
					Role:    "assistant",
					Content: reactOutput,
				})
			}
		} else {
			// If there is no message, add the final output directly
			agentMessages = append(agentMessages, agent.ChatMessage{
				Role:    "assistant",
				Content: reactOutput,
			})
		}
	}

	if len(agentMessages) == 0 {
		return nil, fmt.Errorf("Message parsed from ReAct data is empty")
	}

	// Fix possible mismatch tool messages to avoid OpenAI errors
	// This prevents "messages with role 'tool' must be a response to a preceeding message with 'tool_calls'" errors
	if h.agent != nil {
		if fixed := h.agent.RepairOrphanToolMessages(&agentMessages); fixed {
			h.logger.Info("Fixed mismatch tool messages in history messages recovered from ReAct data",
				zap.String("conversationId", conversationID),
			)
		}
	}

	h.logger.Info("Recovery of historical messages from ReAct data completed",
		zap.String("conversationId", conversationID),
		zap.String("dataSource", dataSource),
		zap.Int("originalMessageCount", messageCount),
		zap.Int("finalMessageCount", len(agentMessages)),
		zap.Bool("hasReactOutput", reactOutput != ""),
	)
	fmt.Println("agentMessages:", agentMessages) //debug
	return agentMessages, nil
}
