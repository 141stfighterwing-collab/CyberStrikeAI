package handler

import (
	"context"
	"net/http"
	"sync"
	"time"

	"cyberstrike-ai/internal/attackchain"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// AttackChainHandler attack chain handler
type AttackChainHandler struct {
	db           *database.DB
	logger       *zap.Logger
	openAIConfig *config.OpenAIConfig
	mu           sync.RWMutex // Protect openAIConfig from concurrent access
	// Used to prevent concurrent generation of the same conversation
	generatingLocks sync.Map // map[string]*sync.Mutex
}

// NewAttackChainHandler creates a new attack chain handler
func NewAttackChainHandler(db *database.DB, openAIConfig *config.OpenAIConfig, logger *zap.Logger) *AttackChainHandler {
	return &AttackChainHandler{
		db:           db,
		logger:       logger,
		openAIConfig: openAIConfig,
	}
}

// UpdateConfig updates OpenAI configuration
func (h *AttackChainHandler) UpdateConfig(cfg *config.OpenAIConfig) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.openAIConfig = cfg
	h.logger.Info("AttackChainHandler configuration has been updated",
		zap.String("base_url", cfg.BaseURL),
		zap.String("model", cfg.Model),
	)
}

// GetOpenAIConfig gets OpenAI configuration (thread safe)
func (h *AttackChainHandler) getOpenAIConfig() *config.OpenAIConfig {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.openAIConfig
}

// GetAttackChain Gets the attack chain (generated on demand)
// GET /api/attack-chain/:conversationId
func (h *AttackChainHandler) GetAttackChain(c *gin.Context) {
	conversationID := c.Param("conversationId")
	if conversationID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "conversationId is required"})
		return
	}

	// Check if the conversation exists
	_, err := h.db.GetConversation(conversationID)
	if err != nil {
		h.logger.Warn("Dialogue does not exist", zap.String("conversationId", conversationID), zap.Error(err))
		c.JSON(http.StatusNotFound, gin.H{"error": "Dialogue does not exist"})
		return
	}

	// Try loading from the database first (if it has been generated)
	openAIConfig := h.getOpenAIConfig()
	builder := attackchain.NewBuilder(h.db, openAIConfig, h.logger)
	chain, err := builder.LoadChainFromDatabase(conversationID)
	if err == nil && len(chain.Nodes) > 0 {
		// If it already exists, return directly
		h.logger.Info("Return the existing attack chain", zap.String("conversationId", conversationID))
		c.JSON(http.StatusOK, chain)
		return
	}

	// If it does not exist, generate a new attack chain (generated on demand)
	// Use a lock mechanism to prevent concurrent occurrences of the same conversation
	lockInterface, _ := h.generatingLocks.LoadOrStore(conversationID, &sync.Mutex{})
	lock := lockInterface.(*sync.Mutex)
	
	// Attempts to acquire the lock, returning an error if it is being generated
	acquired := lock.TryLock()
	if !acquired {
		h.logger.Info("The attack chain is being generated, please try again later.", zap.String("conversationId", conversationID))
		c.JSON(http.StatusConflict, gin.H{"error": "The attack chain is being generated, please try again later."})
		return
	}
	defer lock.Unlock()

	// Check again whether it has been generated (it may have been generated while waiting for the lock)
	chain, err = builder.LoadChainFromDatabase(conversationID)
	if err == nil && len(chain.Nodes) > 0 {
		h.logger.Info("Returns the existing attack chain (generated while waiting for the lock)", zap.String("conversationId", conversationID))
		c.JSON(http.StatusOK, chain)
		return
	}

	h.logger.Info("Start generating attack chain", zap.String("conversationId", conversationID))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	chain, err = builder.BuildChainFromConversation(ctx, conversationID)
	if err != nil {
		h.logger.Error("Failed to generate attack chain", zap.String("conversationId", conversationID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate attack chain:" + err.Error()})
		return
	}

	// After the generation is completed, delete it from the lock map (optional, retention can also be used to prevent repeated generation in a short period of time)
	// h.generatingLocks.Delete(conversationID)

	c.JSON(http.StatusOK, chain)
}

// RegenerateAttackChain regenerates the attack chain
// POST /api/attack-chain/:conversationId/regenerate
func (h *AttackChainHandler) RegenerateAttackChain(c *gin.Context) {
	conversationID := c.Param("conversationId")
	if conversationID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "conversationId is required"})
		return
	}

	// Check if the conversation exists
	_, err := h.db.GetConversation(conversationID)
	if err != nil {
		h.logger.Warn("Dialogue does not exist", zap.String("conversationId", conversationID), zap.Error(err))
		c.JSON(http.StatusNotFound, gin.H{"error": "Dialogue does not exist"})
		return
	}

	// Delete old attack chain
	if err := h.db.DeleteAttackChain(conversationID); err != nil {
		h.logger.Warn("Failed to delete old attack chain", zap.Error(err))
	}

	// Use locking mechanism to prevent concurrency
	lockInterface, _ := h.generatingLocks.LoadOrStore(conversationID, &sync.Mutex{})
	lock := lockInterface.(*sync.Mutex)
	
	acquired := lock.TryLock()
	if !acquired {
		h.logger.Info("The attack chain is being generated, please try again later.", zap.String("conversationId", conversationID))
		c.JSON(http.StatusConflict, gin.H{"error": "The attack chain is being generated, please try again later."})
		return
	}
	defer lock.Unlock()

	// Generate new attack chain
	h.logger.Info("Regenerate attack chain", zap.String("conversationId", conversationID))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	openAIConfig := h.getOpenAIConfig()
	builder := attackchain.NewBuilder(h.db, openAIConfig, h.logger)
	chain, err := builder.BuildChainFromConversation(ctx, conversationID)
	if err != nil {
		h.logger.Error("Failed to generate attack chain", zap.String("conversationId", conversationID), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate attack chain:" + err.Error()})
		return
	}

	c.JSON(http.StatusOK, chain)
}

