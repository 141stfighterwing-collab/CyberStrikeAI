package handler

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const (
	robotCmdHelp        = "Help"
	robotCmdList        = "List"
	robotCmdListAlt     = "Conversation list"
	robotCmdSwitch      = "Switch"
	robotCmdContinue    = "Continue"
	robotCmdNew         = "New conversation"
	robotCmdClear       = "Clear"
	robotCmdCurrent     = "Current"
	robotCmdStop        = "Stop"
	robotCmdRoles       = "Role"
	robotCmdRolesList   = "Role list"
	robotCmdSwitchRole  = "Switch roles"
	robotCmdDelete      = "Delete"
	robotCmdVersion     = "Version"
)

// RobotHandler Enterprise WeChat/DingTalk/Feishu and other robot callback processing
type RobotHandler struct {
	config         *config.Config
	db             *database.DB
	agentHandler   *AgentHandler
	logger         *zap.Logger
	mu             sync.RWMutex
	sessions       map[string]string             // key: "platform_userID", value: conversationID
	sessionRoles   map[string]string             // Key: "platform_userID", value: roleName (default "default")
	cancelMu       sync.Mutex                    // Protect runningCancels
	runningCancels map[string]context.CancelFunc // Key: "platform_userID", used to stop command interruption tasks
}

// NewRobotHandler creates a robot handler
func NewRobotHandler(cfg *config.Config, db *database.DB, agentHandler *AgentHandler, logger *zap.Logger) *RobotHandler {
	return &RobotHandler{
		config:         cfg,
		db:             db,
		agentHandler:   agentHandler,
		logger:         logger,
		sessions:       make(map[string]string),
		sessionRoles:   make(map[string]string),
		runningCancels: make(map[string]context.CancelFunc),
	}
}

// SessionKey generates session key
func (h *RobotHandler) sessionKey(platform, userID string) string {
	return platform + "_" + userID
}

// GetOrCreateConversation gets or creates the current conversation, title is used for the title of the new conversation (take the first 50 words of the user's first message)
func (h *RobotHandler) getOrCreateConversation(platform, userID, title string) (convID string, isNew bool) {
	h.mu.RLock()
	convID = h.sessions[h.sessionKey(platform, userID)]
	h.mu.RUnlock()
	if convID != "" {
		return convID, false
	}
	t := strings.TrimSpace(title)
	if t == "" {
		t = "New conversation" + time.Now().Format("01-02 15:04")
	} else {
		t = safeTruncateString(t, 50)
	}
	conv, err := h.db.CreateConversation(t)
	if err != nil {
		h.logger.Warn("Failed to create bot session", zap.Error(err))
		return "", false
	}
	convID = conv.ID
	h.mu.Lock()
	h.sessions[h.sessionKey(platform, userID)] = convID
	h.mu.Unlock()
	return convID, true
}

// SetConversation switches the current session
func (h *RobotHandler) setConversation(platform, userID, convID string) {
	h.mu.Lock()
	h.sessions[h.sessionKey(platform, userID)] = convID
	h.mu.Unlock()
}

// GetRole Gets the role used by the current user, returns "default" if not set
func (h *RobotHandler) getRole(platform, userID string) string {
	h.mu.RLock()
	role := h.sessionRoles[h.sessionKey(platform, userID)]
	h.mu.RUnlock()
	if role == "" {
		return "Default"
	}
	return role
}

// SetRole sets the role used by the current user
func (h *RobotHandler) setRole(platform, userID, roleName string) {
	h.mu.Lock()
	h.sessionRoles[h.sessionKey(platform, userID)] = roleName
	h.mu.Unlock()
}

// ClearConversation clears the current conversation (switch to a new conversation)
func (h *RobotHandler) clearConversation(platform, userID string) (newConvID string) {
	title := "New conversation" + time.Now().Format("01-02 15:04")
	conv, err := h.db.CreateConversation(title)
	if err != nil {
		h.logger.Warn("Failed to create new conversation", zap.Error(err))
		return ""
	}
	h.setConversation(platform, userID, conv.ID)
	return conv.ID
}

// HandleMessage processes user input and returns reply text (for webhook calls on each platform)
func (h *RobotHandler) HandleMessage(platform, userID, text string) (reply string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "Please enter content or send "help" / help view command."
	}

	// Command distribution (supports Chinese and English)
	switch {
	case text == robotCmdHelp || text == "help" || text == "？" || text == "?":
		return h.cmdHelp()
	case text == robotCmdList || text == robotCmdListAlt || text == "list":
		return h.cmdList()
	case strings.HasPrefix(text, robotCmdSwitch+" ") || strings.HasPrefix(text, robotCmdContinue+" ") || strings.HasPrefix(text, "switch ") || strings.HasPrefix(text, "continue "):
		var id string
		switch {
		case strings.HasPrefix(text, robotCmdSwitch+" "):
			id = strings.TrimSpace(text[len(robotCmdSwitch)+1:])
		case strings.HasPrefix(text, robotCmdContinue+" "):
			id = strings.TrimSpace(text[len(robotCmdContinue)+1:])
		case strings.HasPrefix(text, "switch "):
			id = strings.TrimSpace(text[7:])
		default:
			id = strings.TrimSpace(text[9:])
		}
		return h.cmdSwitch(platform, userID, id)
	case text == robotCmdNew || text == "new":
		return h.cmdNew(platform, userID)
	case text == robotCmdClear || text == "clear":
		return h.cmdClear(platform, userID)
	case text == robotCmdCurrent || text == "current":
		return h.cmdCurrent(platform, userID)
	case text == robotCmdStop || text == "stop":
		return h.cmdStop(platform, userID)
	case text == robotCmdRoles || text == robotCmdRolesList || text == "roles":
		return h.cmdRoles()
	case strings.HasPrefix(text, robotCmdRoles+" ") || strings.HasPrefix(text, robotCmdSwitchRole+" ") || strings.HasPrefix(text, "role "):
		var roleName string
		switch {
		case strings.HasPrefix(text, robotCmdRoles+" "):
			roleName = strings.TrimSpace(text[len(robotCmdRoles)+1:])
		case strings.HasPrefix(text, robotCmdSwitchRole+" "):
			roleName = strings.TrimSpace(text[len(robotCmdSwitchRole)+1:])
		default:
			roleName = strings.TrimSpace(text[5:])
		}
		return h.cmdSwitchRole(platform, userID, roleName)
	case strings.HasPrefix(text, robotCmdDelete+" ") || strings.HasPrefix(text, "delete "):
		var convID string
		if strings.HasPrefix(text, robotCmdDelete+" ") {
			convID = strings.TrimSpace(text[len(robotCmdDelete)+1:])
		} else {
			convID = strings.TrimSpace(text[7:])
		}
		return h.cmdDelete(platform, userID, convID)
	case text == robotCmdVersion || text == "version":
		return h.cmdVersion()
	}

	// General news: Go to Agent
	convID, _ := h.getOrCreateConversation(platform, userID, text)
	if convID == "" {
		return "Unable to create or get the conversation, please try again later."
	}
	// If the conversation title is in the format of "New Conversation xx:xx" (created by the "New Conversation" command), update the title to the content of the first message, consistent with the web experience
	if conv, err := h.db.GetConversation(convID); err == nil && strings.HasPrefix(conv.Title, "New conversation") {
		newTitle := safeTruncateString(text, 50)
		if newTitle != "" {
			_ = h.db.UpdateConversationTitle(convID, newTitle)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	sk := h.sessionKey(platform, userID)
	h.cancelMu.Lock()
	h.runningCancels[sk] = cancel
	h.cancelMu.Unlock()
	defer func() {
		cancel()
		h.cancelMu.Lock()
		delete(h.runningCancels, sk)
		h.cancelMu.Unlock()
	}()
	role := h.getRole(platform, userID)
	resp, newConvID, err := h.agentHandler.ProcessMessageForRobot(ctx, convID, text, role)
	if err != nil {
		h.logger.Warn("Robot Agent execution failed", zap.String("platform", platform), zap.String("userID", userID), zap.Error(err))
		if errors.Is(err, context.Canceled) {
			return "The task has been cancelled."
		}
		return "Processing failed:" + err.Error()
	}
	if newConvID != convID {
		h.setConversation(platform, userID, newConvID)
	}
	return resp
}

func (h *RobotHandler) cmdHelp() string {
	return "**【CyberStrikeAI robot command】**\n\n" +
		"- `help` `help` — Show this help | Show this help\n" +
		"- `list` `list` — List all conversation titles and IDs | List conversations\n" +
		"- `switch <ID>` `switch <ID>` — Specifies the conversation to continue | Switch to conversation\n" +
		"- `new` `new` — Start a new conversation | Start new conversation\n" +
		"- `clear` `clear` — Clear the current context | Clear context\n" +
		"- `current` `current` — Show current conversation ID and title | Show current conversation\n" +
		"- `stop` `stop` — interrupt the current task | Stop running task\n" +
		"- `roles` `roles` — List all available roles | List roles\n" +
		"- `role <name>` `role <name>` — Switch the current role | Switch role\n" +
		"- `delete <ID>` `delete <ID>` — delete the specified conversation | Delete conversation\n" +
		"- `version` `version` — displays the current version number | Show version\n\n" +
		"---\n" +
		"In addition to the above commands, direct input will be sent to AI for penetration testing/security analysis. \n" +
		"Otherwise, send any text for AI penetration testing / security analysis."
}

func (h *RobotHandler) cmdList() string {
	convs, err := h.db.ListConversations(50, 0, "")
	if err != nil {
		return "Failed to get conversation list:" + err.Error()
	}
	if len(convs) == 0 {
		return "There are no conversations yet. Sending anything will automatically create a new conversation."
	}
	var b strings.Builder
	b.WriteString("【Conversation List】\n")
	for i, c := range convs {
		if i >= 20 {
			b.WriteString("…only show first 20 items\n")
			break
		}
		b.WriteString(fmt.Sprintf("· %s\n  ID: %s\n", c.Title, c.ID))
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func (h *RobotHandler) cmdSwitch(platform, userID, convID string) string {
	if convID == "" {
		return "Please specify the conversation ID, for example: switch xxx-xxx-xxx"
	}
	conv, err := h.db.GetConversation(convID)
	if err != nil {
		return "Conversation does not exist or has wrong ID."
	}
	h.setConversation(platform, userID, conv.ID)
	return fmt.Sprintf("Switched to conversation: "%s"\nID: %s", conv.Title, conv.ID)
}

func (h *RobotHandler) cmdNew(platform, userID string) string {
	newID := h.clearConversation(platform, userID)
	if newID == "" {
		return "Failed to create new conversation, please try again."
	}
	return "A new conversation has been opened and content can be sent directly."
}

func (h *RobotHandler) cmdClear(platform, userID string) string {
	return h.cmdNew(platform, userID)
}

func (h *RobotHandler) cmdStop(platform, userID string) string {
	sk := h.sessionKey(platform, userID)
	h.cancelMu.Lock()
	cancel, ok := h.runningCancels[sk]
	if ok {
		delete(h.runningCancels, sk)
		cancel()
	}
	h.cancelMu.Unlock()
	if !ok {
		return "There are no tasks currently executing."
	}
	return "The current task has been stopped."
}

func (h *RobotHandler) cmdCurrent(platform, userID string) string {
	h.mu.RLock()
	convID := h.sessions[h.sessionKey(platform, userID)]
	h.mu.RUnlock()
	if convID == "" {
		return "There are currently no ongoing conversations. Sending anything will create a new conversation."
	}
	conv, err := h.db.GetConversation(convID)
	if err != nil {
		return "Current conversation ID:" + convID + "(Failed to get title)"
	}
	role := h.getRole(platform, userID)
	return fmt.Sprintf("Current conversation: "%s"\nID: %s\nCurrent character: %s", conv.Title, conv.ID, role)
}

func (h *RobotHandler) cmdRoles() string {
	if h.config.Roles == nil || len(h.config.Roles) == 0 {
		return "There are no available roles yet."
	}
	names := make([]string, 0, len(h.config.Roles))
	for name, role := range h.config.Roles {
		if role.Enabled {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "There are no available roles yet."
	}
	sort.Slice(names, func(i, j int) bool {
		if names[i] == "Default" {
			return true
		}
		if names[j] == "Default" {
			return false
		}
		return names[i] < names[j]
	})
	var b strings.Builder
	b.WriteString("【Character List】\n")
	for _, name := range names {
		role := h.config.Roles[name]
		desc := role.Description
		if desc == "" {
			desc = "No description"
		}
		b.WriteString(fmt.Sprintf("· %s — %s\n", name, desc))
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func (h *RobotHandler) cmdSwitchRole(platform, userID, roleName string) string {
	if roleName == "" {
		return "Please specify a role name, for example: Role Penetration Testing"
	}
	if h.config.Roles == nil {
		return "There are no available roles yet."
	}
	role, exists := h.config.Roles[roleName]
	if !exists {
		return fmt.Sprintf("The character "%s" does not exist. Send "role" to see available roles.", roleName)
	}
	if !role.Enabled {
		return fmt.Sprintf("Character "%s" is disabled.", roleName)
	}
	h.setRole(platform, userID, roleName)
	return fmt.Sprintf("Switched to character: "%s"\n%s", roleName, role.Description)
}

func (h *RobotHandler) cmdDelete(platform, userID, convID string) string {
	if convID == "" {
		return "Please specify the conversation ID, for example: delete xxx-xxx-xxx"
	}
	sk := h.sessionKey(platform, userID)
	h.mu.RLock()
	currentConvID := h.sessions[sk]
	h.mu.RUnlock()
	if convID == currentConvID {
		// When deleting the current conversation, clear the session binding first
		h.mu.Lock()
		delete(h.sessions, sk)
		h.mu.Unlock()
	}
	if err := h.db.DeleteConversation(convID); err != nil {
		return "Delete failed:" + err.Error()
	}
	return fmt.Sprintf("Deleted conversation ID: %s", convID)
}

func (h *RobotHandler) cmdVersion() string {
	v := h.config.Version
	if v == "" {
		v = "Unknown"
	}
	return "CyberStrikeAI " + v
}

// —————— Enterprise WeChat ——————

// WecomXML Enterprise WeChat callback XML (simplified structure in plain text mode; encryption mode needs to be decrypted first and then parsed)
type wecomXML struct {
	ToUserName   string `xml:"ToUserName"`
	FromUserName string `xml:"FromUserName"`
	CreateTime   int64  `xml:"CreateTime"`
	MsgType      string `xml:"MsgType"`
	Content      string `xml:"Content"`
	MsgID        string `xml:"MsgId"`
	AgentID      int64  `xml:"AgentID"`
	Encrypt      string `xml:"Encrypt"` // The message in encrypted mode is here
}

// WecomReplyXML passive reply XML
type wecomReplyXML struct {
	XMLName      xml.Name `xml:"xml"`
	ToUserName   string   `xml:"ToUserName"`
	FromUserName string  `xml:"FromUserName"`
	CreateTime   int64   `xml:"CreateTime"`
	MsgType      string  `xml:"MsgType"`
	Content      string  `xml:"Content"`
}

// HandleWecomGET Enterprise WeChat URL verification (GET)
func (h *RobotHandler) HandleWecomGET(c *gin.Context) {
	if !h.config.Robots.Wecom.Enabled {
		c.String(http.StatusNotFound, "")
		return
	}
	echostr := c.Query("echostr")
	if echostr == "" {
		c.String(http.StatusBadRequest, "missing echostr")
		return
	}
	// In plain text mode, Enterprise WeChat may directly transmit echostr and return it directly first to pass the verification.
	c.String(http.StatusOK, echostr)
}

// WecomDecrypt Enterprise WeChat message decryption (AES-256-CBC, PKCS7, plain text format: 16 bytes random + 4 bytes length + message + corpID)
func wecomDecrypt(encodingAESKey, encryptedB64 string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(encodingAESKey + "=")
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("Encoding_aes_key should be 32 bytes after decoding")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(encryptedB64)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	iv := key[:16]
	mode := cipher.NewCBCDecrypter(block, iv)
	if len(ciphertext)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("The ciphertext length is not a multiple of the block size")
	}
	plain := make([]byte, len(ciphertext))
	mode.CryptBlocks(plain, ciphertext)
	// Remove PKCS7 padding
	n := int(plain[len(plain)-1])
	if n < 1 || n > 32 {
		return nil, fmt.Errorf("Invalid PKCS7 padding")
	}
	plain = plain[:len(plain)-n]
	// Enterprise WeChat format: 16 bytes random + 4 bytes length (big endian) + message + corpID
	if len(plain) < 20 {
		return nil, fmt.Errorf("Plaintext too short")
	}
	msgLen := binary.BigEndian.Uint32(plain[16:20])
	if int(20+msgLen) > len(plain) {
		return nil, fmt.Errorf("Message length out of bounds")
	}
	return plain[20 : 20+msgLen], nil
}

// HandleWecomPOST Enterprise WeChat message callback (POST), supports plain text and encrypted modes
func (h *RobotHandler) HandleWecomPOST(c *gin.Context) {
	if !h.config.Robots.Wecom.Enabled {
		c.String(http.StatusOK, "")
		return
	}
	bodyRaw, _ := io.ReadAll(c.Request.Body)
	var body wecomXML
	if err := xml.Unmarshal(bodyRaw, &body); err != nil {
		h.logger.Debug("Enterprise WeChat POST fails to parse XML", zap.Error(err))
		c.String(http.StatusOK, "")
		return
	}
	// Encryption mode: decrypt first and then parse the inner XML
	if body.Encrypt != "" && h.config.Robots.Wecom.EncodingAESKey != "" {
		decrypted, err := wecomDecrypt(h.config.Robots.Wecom.EncodingAESKey, body.Encrypt)
		if err != nil {
			h.logger.Warn("Enterprise WeChat message decryption failed", zap.Error(err))
			c.String(http.StatusOK, "")
			return
		}
		if err := xml.Unmarshal(decrypted, &body); err != nil {
			h.logger.Warn("XML parsing fails after enterprise WeChat decryption", zap.Error(err))
			c.String(http.StatusOK, "")
			return
		}
	}
	if body.MsgType != "text" {
		c.XML(http.StatusOK, wecomReplyXML{
			ToUserName:   body.FromUserName,
			FromUserName: body.ToUserName,
			CreateTime:  time.Now().Unix(),
			MsgType:     "text",
			Content:     "Currently only text messages are supported, please send text.",
		})
		return
	}
	userID := body.FromUserName
	text := strings.TrimSpace(body.Content)
	reply := h.HandleMessage("wecom", userID, text)
	// The encryption mode requires an encrypted reply (here it is simplified to a plain text reply; if the enterprise requires encryption, encryption must be implemented)
	c.XML(http.StatusOK, wecomReplyXML{
		ToUserName:   body.FromUserName,
		FromUserName: body.ToUserName,
		CreateTime:  time.Now().Unix(),
		MsgType:     "text",
		Content:     reply,
	})
}

// —————— Test interface (requires login, used to verify robot logic, no DingTalk/Feishu client required) ——————

// RobotTestRequest simulates robot message request
type RobotTestRequest struct {
	Platform string `json:"platform"` // Such as "dingtalk", "lark", "wecom"
	UserID   string `json:"user_id"`
	Text     string `json:"text"`
}

// HandleRobotTest for local verification: POST JSON { "platform", "user_id", "text" }, return { "reply": "..." }
func (h *RobotHandler) HandleRobotTest(c *gin.Context) {
	var req RobotTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "The request body must be JSON, including platform, user_id, text"})
		return
	}
	platform := strings.TrimSpace(req.Platform)
	if platform == "" {
		platform = "test"
	}
	userID := strings.TrimSpace(req.UserID)
	if userID == "" {
		userID = "test_user"
	}
	reply := h.HandleMessage(platform, userID, req.Text)
	c.JSON(http.StatusOK, gin.H{"reply": reply})
}

// —————— DingTalk ——————

// HandleDingtalkPOST DingTalk event callback (streaming access, etc.); currently a placeholder, returns 200
func (h *RobotHandler) HandleDingtalkPOST(c *gin.Context) {
	if !h.config.Robots.Dingtalk.Enabled {
		c.JSON(http.StatusOK, gin.H{})
		return
	}
	// DingTalk streaming/event callback format needs to be parsed and responded asynchronously according to official documents. Only 200 is returned here.
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}

// ———— Feishu ——————

// HandleLarkPOST Feishu event callback; currently a placeholder, returns 200; challenge needs to be returned during verification
func (h *RobotHandler) HandleLarkPOST(c *gin.Context) {
	if !h.config.Robots.Lark.Enabled {
		c.JSON(http.StatusOK, gin.H{})
		return
	}
	var body struct {
		Challenge string `json:"challenge"`
	}
	if err := c.ShouldBindJSON(&body); err == nil && body.Challenge != "" {
		c.JSON(http.StatusOK, gin.H{"challenge": body.Challenge})
		return
	}
	c.JSON(http.StatusOK, gin.H{})
}
