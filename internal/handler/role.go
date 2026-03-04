package handler

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"cyberstrike-ai/internal/config"

	"gopkg.in/yaml.v3"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// RoleHandler role handler
type RoleHandler struct {
	config        *config.Config
	configPath    string
	logger        *zap.Logger
	skillsManager SkillsManager // Skills manager interface (optional)
}

// SkillsManager Skills Manager Interface
type SkillsManager interface {
	ListSkills() ([]string, error)
}

// NewRoleHandler creates a new role handler
func NewRoleHandler(cfg *config.Config, configPath string, logger *zap.Logger) *RoleHandler {
	return &RoleHandler{
		config:     cfg,
		configPath: configPath,
		logger:     logger,
	}
}

// SetSkillsManager Set Skills Manager
func (h *RoleHandler) SetSkillsManager(manager SkillsManager) {
	h.skillsManager = manager
}

// GetSkills Gets a list of all available skills
func (h *RoleHandler) GetSkills(c *gin.Context) {
	if h.skillsManager == nil {
		c.JSON(http.StatusOK, gin.H{
			"skills": []string{},
		})
		return
	}

	skills, err := h.skillsManager.ListSkills()
	if err != nil {
		h.logger.Warn("Failed to obtain skills list", zap.Error(err))
		c.JSON(http.StatusOK, gin.H{
			"skills": []string{},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"skills": skills,
	})
}

// GetRoles Gets all roles
func (h *RoleHandler) GetRoles(c *gin.Context) {
	if h.config.Roles == nil {
		h.config.Roles = make(map[string]config.RoleConfig)
	}

	roles := make([]config.RoleConfig, 0, len(h.config.Roles))
	for key, role := range h.config.Roles {
		// Make sure the role's key and name are consistent
		if role.Name == "" {
			role.Name = key
		}
		roles = append(roles, role)
	}

	c.JSON(http.StatusOK, gin.H{
		"roles": roles,
	})
}

// GetRole Gets a single role
func (h *RoleHandler) GetRole(c *gin.Context) {
	roleName := c.Param("name")
	if roleName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Role name cannot be empty"})
		return
	}

	if h.config.Roles == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Role does not exist"})
		return
	}

	role, exists := h.config.Roles[roleName]
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Role does not exist"})
		return
	}

	// Make sure the role's name and key are consistent
	if role.Name == "" {
		role.Name = roleName
	}

	c.JSON(http.StatusOK, gin.H{
		"role": role,
	})
}

// UpdateRole update role
func (h *RoleHandler) UpdateRole(c *gin.Context) {
	roleName := c.Param("name")
	if roleName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Role name cannot be empty"})
		return
	}

	var req config.RoleConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request parameter:" + err.Error()})
		return
	}

	// Make sure the role name is consistent with the name in the request
	if req.Name == "" {
		req.Name = roleName
	}

	// Initialize Roles map
	if h.config.Roles == nil {
		h.config.Roles = make(map[string]config.RoleConfig)
	}

	// Delete all old roles with the same name but different keys (to avoid duplication)
	// Use role name as key to ensure uniqueness
	finalKey := req.Name
	keysToDelete := make([]string, 0)
	for key := range h.config.Roles {
		// If the key is different from the final key but the name is the same, mark it for deletion
		if key != finalKey {
			role := h.config.Roles[key]
			// Make sure the role's name field is set correctly
			if role.Name == "" {
				role.Name = key
			}
			if role.Name == req.Name {
				keysToDelete = append(keysToDelete, key)
			}
		}
	}
	// Delete old role
	for _, key := range keysToDelete {
		delete(h.config.Roles, key)
		h.logger.Info("Remove duplicate roles", zap.String("oldKey", key), zap.String("name", req.Name))
	}

	// If the currently updated key is different from the final key, the old one also needs to be deleted.
	if roleName != finalKey {
		delete(h.config.Roles, roleName)
	}

	// If the character name changes, old files need to be deleted
	if roleName != finalKey {
		configDir := filepath.Dir(h.configPath)
		rolesDir := h.config.RolesDir
		if rolesDir == "" {
			rolesDir = "roles" // Default directory
		}

		// If it is a relative path, it is relative to the directory where the configuration file is located.
		if !filepath.IsAbs(rolesDir) {
			rolesDir = filepath.Join(configDir, rolesDir)
		}

		// Delete old character files
		oldSafeFileName := sanitizeFileName(roleName)
		oldRoleFileYaml := filepath.Join(rolesDir, oldSafeFileName+".yaml")
		oldRoleFileYml := filepath.Join(rolesDir, oldSafeFileName+".yml")

		if _, err := os.Stat(oldRoleFileYaml); err == nil {
			if err := os.Remove(oldRoleFileYaml); err != nil {
				h.logger.Warn("Deletion of old role profile failed", zap.String("file", oldRoleFileYaml), zap.Error(err))
			}
		}
		if _, err := os.Stat(oldRoleFileYml); err == nil {
			if err := os.Remove(oldRoleFileYml); err != nil {
				h.logger.Warn("Deletion of old role profile failed", zap.String("file", oldRoleFileYml), zap.Error(err))
			}
		}
	}

	// Use role name as key to save (ensure uniqueness)
	h.config.Roles[finalKey] = req

	// Save configuration to file
	if err := h.saveConfig(); err != nil {
		h.logger.Error("Failed to save configuration", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save configuration:" + err.Error()})
		return
	}

	h.logger.Info("Update role", zap.String("oldKey", roleName), zap.String("newKey", finalKey), zap.String("name", req.Name))
	c.JSON(http.StatusOK, gin.H{
		"message": "Role updated",
		"role":    req,
	})
}

// CreateRole creates a new role
func (h *RoleHandler) CreateRole(c *gin.Context) {
	var req config.RoleConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request parameter:" + err.Error()})
		return
	}

	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Role name cannot be empty"})
		return
	}

	// Initialize Roles map
	if h.config.Roles == nil {
		h.config.Roles = make(map[string]config.RoleConfig)
	}

	// Check if the role already exists
	if _, exists := h.config.Roles[req.Name]; exists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Role already exists"})
		return
	}

	// Create role (enabled by default)
	if !req.Enabled {
		req.Enabled = true
	}

	h.config.Roles[req.Name] = req

	// Save configuration to file
	if err := h.saveConfig(); err != nil {
		h.logger.Error("Failed to save configuration", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save configuration:" + err.Error()})
		return
	}

	h.logger.Info("Create a role", zap.String("roleName", req.Name))
	c.JSON(http.StatusOK, gin.H{
		"message": "Role created",
		"role":    req,
	})
}

// DeleteRole Delete role
func (h *RoleHandler) DeleteRole(c *gin.Context) {
	roleName := c.Param("name")
	if roleName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Role name cannot be empty"})
		return
	}

	if h.config.Roles == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Role does not exist"})
		return
	}

	if _, exists := h.config.Roles[roleName]; !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Role does not exist"})
		return
	}

	// Not allowed to delete "default" role
	if roleName == "Default" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cannot delete default role"})
		return
	}

	delete(h.config.Roles, roleName)

	// Delete the corresponding role file
	configDir := filepath.Dir(h.configPath)
	rolesDir := h.config.RolesDir
	if rolesDir == "" {
		rolesDir = "roles" // Default directory
	}

	// If it is a relative path, it is relative to the directory where the configuration file is located.
	if !filepath.IsAbs(rolesDir) {
		rolesDir = filepath.Join(configDir, rolesDir)
	}

	// Try deleting role files (.yaml and .yml)
	safeFileName := sanitizeFileName(roleName)
	roleFileYaml := filepath.Join(rolesDir, safeFileName+".yaml")
	roleFileYml := filepath.Join(rolesDir, safeFileName+".yml")

	// Delete the .yaml file if it exists
	if _, err := os.Stat(roleFileYaml); err == nil {
		if err := os.Remove(roleFileYaml); err != nil {
			h.logger.Warn("Failed to delete role profile", zap.String("file", roleFileYaml), zap.Error(err))
		} else {
			h.logger.Info("Role profile deleted", zap.String("file", roleFileYaml))
		}
	}

	// Delete the .yml file if it exists
	if _, err := os.Stat(roleFileYml); err == nil {
		if err := os.Remove(roleFileYml); err != nil {
			h.logger.Warn("Failed to delete role profile", zap.String("file", roleFileYml), zap.Error(err))
		} else {
			h.logger.Info("Role profile deleted", zap.String("file", roleFileYml))
		}
	}

	h.logger.Info("Delete role", zap.String("roleName", roleName))
	c.JSON(http.StatusOK, gin.H{
		"message": "Role deleted",
	})
}

// SaveConfig saves the configuration to a file in the directory
func (h *RoleHandler) saveConfig() error {
	configDir := filepath.Dir(h.configPath)
	rolesDir := h.config.RolesDir
	if rolesDir == "" {
		rolesDir = "roles" // Default directory
	}

	// If it is a relative path, it is relative to the directory where the configuration file is located.
	if !filepath.IsAbs(rolesDir) {
		rolesDir = filepath.Join(configDir, rolesDir)
	}

	// Make sure the directory exists
	if err := os.MkdirAll(rolesDir, 0755); err != nil {
		return fmt.Errorf("Failed to create role directory: %w", err)
	}

	// Save each character to a separate file
	if h.config.Roles != nil {
		for roleName, role := range h.config.Roles {
			// Make sure the role name is set correctly
			if role.Name == "" {
				role.Name = roleName
			}

			// Use character name as filename (safely filename to avoid special characters)
			safeFileName := sanitizeFileName(role.Name)
			roleFile := filepath.Join(rolesDir, safeFileName+".yaml")

			// Serialize role configuration to YAML
			roleData, err := yaml.Marshal(&role)
			if err != nil {
				h.logger.Error("Failed to serialize role configuration", zap.String("role", roleName), zap.Error(err))
				continue
			}

			// Handling icon fields: Make sure icon values ​​containing \U are surrounded by quotes (YAML requires quotes to correctly parse Unicode escapes)
			roleDataStr := string(roleData)
			if role.Icon != "" && strings.HasPrefix(role.Icon, "\\U") {
				// Match icon: \UXXXXXXXX format (without quotes), excluding the situation where there are already quotes
				// Use a negative lookahead to ensure there are no quotes following, or match directly without quotes
				re := regexp.MustCompile(`(?m)^(icon:\s+)(\\U[0-9A-F]{8})(\s*)$`)
				roleDataStr = re.ReplaceAllString(roleDataStr, `${1}"${2}"${3}`)
				roleData = []byte(roleDataStr)
			}

			// Write file
			if err := os.WriteFile(roleFile, roleData, 0644); err != nil {
				h.logger.Error("Failed to save role profile", zap.String("role", roleName), zap.String("file", roleFile), zap.Error(err))
				continue
			}

			h.logger.Info("Role configuration saved to file", zap.String("role", roleName), zap.String("file", roleFile))
		}
	}

	return nil
}

// SanitizeFileName Converts a role name to a safe file name
func sanitizeFileName(name string) string {
	// Replace potentially unsafe characters
	replacer := map[rune]string{
		'/':  "_",
		'\\': "_",
		':':  "_",
		'*':  "_",
		'?':  "_",
		'"':  "_",
		'<':  "_",
		'>':  "_",
		'|':  "_",
		' ':  "_",
	}

	var result []rune
	for _, r := range name {
		if replacement, ok := replacer[r]; ok {
			result = append(result, []rune(replacement)...)
		} else {
			result = append(result, r)
		}
	}

	fileName := string(result)
	// If the file name is empty, the default name is used
	if fileName == "" {
		fileName = "role"
	}

	return fileName
}

// UpdateRolesConfig updates role configuration
func updateRolesConfig(doc *yaml.Node, cfg config.RolesConfig) {
	root := doc.Content[0]
	rolesNode := ensureMap(root, "roles")

	// Clear existing roles
	if rolesNode.Kind == yaml.MappingNode {
		rolesNode.Content = nil
	}

	// Add a new role (use name as key to ensure uniqueness)
	if cfg.Roles != nil {
		// First create a map with name as key and remove duplicates (keep the last one)
		rolesByName := make(map[string]config.RoleConfig)
		for roleKey, role := range cfg.Roles {
			// Make sure the role's name field is set correctly
			if role.Name == "" {
				role.Name = roleKey
			}
			// Use name as the final key. If there are multiple keys corresponding to the same name, only the last one is retained.
			rolesByName[role.Name] = role
		}

		// Write the deduplicated role into YAML
		for roleName, role := range rolesByName {
			roleNode := ensureMap(rolesNode, roleName)
			setStringInMap(roleNode, "name", role.Name)
			setStringInMap(roleNode, "description", role.Description)
			setStringInMap(roleNode, "user_prompt", role.UserPrompt)
			if role.Icon != "" {
				setStringInMap(roleNode, "icon", role.Icon)
			}
			setBoolInMap(roleNode, "enabled", role.Enabled)

			// Add a tool list (use the tools field first)
			if len(role.Tools) > 0 {
				toolsNode := ensureArray(roleNode, "tools")
				toolsNode.Content = nil
				for _, toolKey := range role.Tools {
					toolNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: toolKey}
					toolsNode.Content = append(toolsNode.Content, toolNode)
				}
			} else if len(role.MCPs) > 0 {
				// Backward compatibility: if there are no tools but mcps, save mcps
				mcpsNode := ensureArray(roleNode, "mcps")
				mcpsNode.Content = nil
				for _, mcpName := range role.MCPs {
					mcpNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: mcpName}
					mcpsNode.Content = append(mcpsNode.Content, mcpNode)
				}
			}
		}
	}
}

// EnsureArray ensures that the array node with the specified key exists in the array
func ensureArray(parent *yaml.Node, key string) *yaml.Node {
	_, valueNode := ensureKeyValue(parent, key)
	if valueNode.Kind != yaml.SequenceNode {
		valueNode.Kind = yaml.SequenceNode
		valueNode.Tag = "!!seq"
		valueNode.Content = nil
	}
	return valueNode
}
