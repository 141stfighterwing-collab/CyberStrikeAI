package config

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Version     string                `yaml:"version,omitempty" json:"version,omitempty"` // The version number displayed on the front end, such as v1.3.3
	Server      ServerConfig          `yaml:"server"`
	Log         LogConfig             `yaml:"log"`
	MCP         MCPConfig             `yaml:"mcp"`
	OpenAI      OpenAIConfig          `yaml:"openai"`
	FOFA        FofaConfig            `yaml:"fofa,omitempty" json:"fofa,omitempty"`
	Agent       AgentConfig           `yaml:"agent"`
	Security    SecurityConfig        `yaml:"security"`
	Database    DatabaseConfig        `yaml:"database"`
	Auth        AuthConfig            `yaml:"auth"`
	ExternalMCP ExternalMCPConfig     `yaml:"external_mcp,omitempty"`
	Knowledge   KnowledgeConfig       `yaml:"knowledge,omitempty"`
	Robots      RobotsConfig          `yaml:"robots,omitempty" json:"robots,omitempty"`         // Enterprise WeChat/DingTalk/Feishu and other robot configurations
	RolesDir    string                `yaml:"roles_dir,omitempty" json:"roles_dir,omitempty"`   // Role profile directory (new way)
	Roles       map[string]RoleConfig `yaml:"roles,omitempty" json:"roles,omitempty"`           // Backward compatibility: supports defining roles in the main configuration file
	SkillsDir   string                `yaml:"skills_dir,omitempty" json:"skills_dir,omitempty"` // Skills configuration file directory
}

// RobotsConfig robot configuration (Enterprise WeChat, DingTalk, Feishu, etc.)
type RobotsConfig struct {
	Wecom   RobotWecomConfig   `yaml:"wecom,omitempty" json:"wecom,omitempty"`     // Enterprise WeChat
	Dingtalk RobotDingtalkConfig `yaml:"dingtalk,omitempty" json:"dingtalk,omitempty"` // DingTalk
	Lark    RobotLarkConfig    `yaml:"lark,omitempty" json:"lark,omitempty"`     // Feishu
}

// RobotWecomConfig Enterprise WeChat robot configuration
type RobotWecomConfig struct {
	Enabled       bool   `yaml:"enabled" json:"enabled"`
	Token         string `yaml:"token" json:"token"`                     // Callback URL verification token
	EncodingAESKey string `yaml:"encoding_aes_key" json:"encoding_aes_key"` // EncodingAESKey
	CorpID        string `yaml:"corp_id" json:"corp_id"`               // Enterprise ID
	Secret        string `yaml:"secret" json:"secret"`                  // Apply Secret
	AgentID       int64  `yaml:"agent_id" json:"agent_id"`              // ApplicationAgentId
}

// RobotDingtalkConfig DingTalk robot configuration
type RobotDingtalkConfig struct {
	Enabled      bool   `yaml:"enabled" json:"enabled"`
	ClientID     string `yaml:"client_id" json:"client_id"`         // Application Key (AppKey)
	ClientSecret string `yaml:"client_secret" json:"client_secret"` // Apply Secret
}

// RobotLarkConfig Feishu robot configuration
type RobotLarkConfig struct {
	Enabled   bool   `yaml:"enabled" json:"enabled"`
	AppID     string `yaml:"app_id" json:"app_id"`         // App App ID
	AppSecret string `yaml:"app_secret" json:"app_secret"` // App App Secret
	VerifyToken string `yaml:"verify_token" json:"verify_token"` // Event subscription Verification Token (optional)
}

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type LogConfig struct {
	Level  string `yaml:"level"`
	Output string `yaml:"output"`
}

type MCPConfig struct {
	Enabled bool   `yaml:"enabled"`
	Host    string `yaml:"host"`
	Port    int    `yaml:"port"`
}

type OpenAIConfig struct {
	APIKey         string `yaml:"api_key" json:"api_key"`
	BaseURL        string `yaml:"base_url" json:"base_url"`
	Model          string `yaml:"model" json:"model"`
	MaxTotalTokens int    `yaml:"max_total_tokens,omitempty" json:"max_total_tokens,omitempty"`
}

type FofaConfig struct {
	// Email is the FOFA account email; APIKey is the FOFA API Key (it is recommended to use a Key with read-only permission)
	Email   string `yaml:"email,omitempty" json:"email,omitempty"`
	APIKey  string `yaml:"api_key,omitempty" json:"api_key,omitempty"`
BaseURL string `yaml:"base_url,omitempty" json:"base_url,omitempty"` // Default https://fofa.info/api/v1/search/all
}

type SecurityConfig struct {
	Tools               []ToolConfig `yaml:"tools,omitempty"`                 // Backward compatibility: supports defining tools in the main configuration file
	ToolsDir            string       `yaml:"tools_dir,omitempty"`             // Tool configuration file directory (new way)
	ToolDescriptionMode string       `yaml:"tool_description_mode,omitempty"` // Tool description mode: "short" | "full", default short
}

type DatabaseConfig struct {
	Path            string `yaml:"path"`                        // Session database path
	KnowledgeDBPath string `yaml:"knowledge_db_path,omitempty"` // Knowledge base database path (optional, if empty, the session database will be used)
}

type AgentConfig struct {
	MaxIterations        int    `yaml:"max_iterations" json:"max_iterations"`
	LargeResultThreshold int    `yaml:"large_result_threshold" json:"large_result_threshold"` // Large result threshold (bytes), default 50KB
	ResultStorageDir     string `yaml:"result_storage_dir" json:"result_storage_dir"`         // Result storage directory, default tmp
}

type AuthConfig struct {
	Password                    string `yaml:"password" json:"password"`
	SessionDurationHours        int    `yaml:"session_duration_hours" json:"session_duration_hours"`
	GeneratedPassword           string `yaml:"-" json:"-"`
	GeneratedPasswordPersisted  bool   `yaml:"-" json:"-"`
	GeneratedPasswordPersistErr string `yaml:"-" json:"-"`
}

// ExternalMCPConfig External MCP configuration
type ExternalMCPConfig struct {
	Servers map[string]ExternalMCPServerConfig `yaml:"servers,omitempty" json:"servers,omitempty"`
}

// ExternalMCPServerConfig external MCP server configuration
type ExternalMCPServerConfig struct {
	// Stdio mode configuration
	Command string            `yaml:"command,omitempty" json:"command,omitempty"`
	Args    []string          `yaml:"args,omitempty" json:"args,omitempty"`
	Env     map[string]string `yaml:"env,omitempty" json:"env,omitempty"` // Environment variables (for stdio mode)

	// HTTP mode configuration
Transport string `yaml:"transport,omitempty" json:"transport,omitempty"` // "stdio" | "sse" | "http"(Streamable) | "simple_http" (self-built/simple POST endpoint, such as this machine http://127.0.0.1:8081/mcp)
	URL       string            `yaml:"url,omitempty" json:"url,omitempty"`
	Headers   map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"` // HTTP/SSE request headers (such as x-api-key)

	// Common configuration
	Description       string          `yaml:"description,omitempty" json:"description,omitempty"`
	Timeout           int             `yaml:"timeout,omitempty" json:"timeout,omitempty"`                         // Timeout (seconds)
	ExternalMCPEnable bool            `yaml:"external_mcp_enable,omitempty" json:"external_mcp_enable,omitempty"` // Whether to enable external MCP
	ToolEnabled       map[string]bool `yaml:"tool_enabled,omitempty" json:"tool_enabled,omitempty"`               // The activation status of each tool (tool name -> whether it is enabled)

	// Backward compatibility field (deprecated, reserved for reading old configurations)
	Enabled  bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`   // Deprecated, use external_mcp_enable
	Disabled bool `yaml:"disabled,omitempty" json:"disabled,omitempty"` // Deprecated, use external_mcp_enable
}
type ToolConfig struct {
	Name             string            `yaml:"name"`
	Command          string            `yaml:"command"`
	Args             []string          `yaml:"args,omitempty"`              // Fixed parameters (optional)
	ShortDescription string            `yaml:"short_description,omitempty"` // Short description (used for tool list, reducing token consumption)
	Description      string            `yaml:"description"`                 // Detailed description (for tool documentation)
	Enabled          bool              `yaml:"enabled"`
	Parameters       []ParameterConfig `yaml:"parameters,omitempty"`         // Parameter definition (optional)
	ArgMapping       string            `yaml:"arg_mapping,omitempty"`        // Parameter mapping method: "auto", "manual", "template" (optional)
	AllowedExitCodes []int             `yaml:"allowed_exit_codes,omitempty"` // List of allowed exit codes (some tools also return non-zero exit codes on success)
}

// ParameterConfig parameter configuration
type ParameterConfig struct {
	Name        string      `yaml:"name"`               // Parameter name
	Type        string      `yaml:"type"`               // Parameter type: string, int, bool, array
	Description string      `yaml:"description"`        // Parameter description
	Required    bool        `yaml:"required,omitempty"` // Is it necessary
	Default     interface{} `yaml:"default,omitempty"`  // Default value
	Flag        string      `yaml:"flag,omitempty"`     // Command line flags such as "-u", "--url", "-p"
	Position    *int        `yaml:"position,omitempty"` // The position of the positional parameter (starting from 0)
	Format      string      `yaml:"format,omitempty"`   // Parameter format: "flag", "positional", "combined" (flag=value), "template"
	Template    string      `yaml:"template,omitempty"` // Template string, such as "{flag} {value}" or "{value}"
	Options     []string    `yaml:"options,omitempty"`  // List of optional values ​​(for enumerations)
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("Failed to read configuration file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("Failed to parse configuration file: %w", err)
	}

	if cfg.Auth.SessionDurationHours <= 0 {
		cfg.Auth.SessionDurationHours = 12
	}

	if strings.TrimSpace(cfg.Auth.Password) == "" {
		password, err := generateStrongPassword(24)
		if err != nil {
			return nil, fmt.Errorf("Failed to generate default password: %w", err)
		}

		cfg.Auth.Password = password
		cfg.Auth.GeneratedPassword = password

		if err := PersistAuthPassword(path, password); err != nil {
			cfg.Auth.GeneratedPasswordPersisted = false
			cfg.Auth.GeneratedPasswordPersistErr = err.Error()
		} else {
			cfg.Auth.GeneratedPasswordPersisted = true
		}
	}

	// If a tool directory is configured, load the tool configuration from the directory
	if cfg.Security.ToolsDir != "" {
		configDir := filepath.Dir(path)
		toolsDir := cfg.Security.ToolsDir

		// If it is a relative path, it is relative to the directory where the configuration file is located.
		if !filepath.IsAbs(toolsDir) {
			toolsDir = filepath.Join(configDir, toolsDir)
		}

		tools, err := LoadToolsFromDir(toolsDir)
		if err != nil {
			return nil, fmt.Errorf("Failed to load tool configuration from tools directory: %w", err)
		}

		// Merge tool configuration: tools in the directory take precedence, and tools in the main configuration are supplemented
		existingTools := make(map[string]bool)
		for _, tool := range tools {
			existingTools[tool.Name] = true
		}

		// Add tools in the main configuration that are not present in the directory (backward compatibility)
		for _, tool := range cfg.Security.Tools {
			if !existingTools[tool.Name] {
				tools = append(tools, tool)
			}
		}

		cfg.Security.Tools = tools
	}

	// Migrate external MCP configuration: migrate old enabled/disabled fields to external_mcp_enable
	if cfg.ExternalMCP.Servers != nil {
		for name, serverCfg := range cfg.ExternalMCP.Servers {
			// If external_mcp_enable is already set, skip migration
			// Otherwise migrate from enabled/disabled fields
			// Note: Since ExternalMCPEnable is of bool type and the zero value is false, you need to check whether it is really set.
			// Here we determine whether migration is needed by checking the old enabled/disabled fields
			if serverCfg.Disabled {
				// The old configuration uses disabled and migrates to external_mcp_enable
				serverCfg.ExternalMCPEnable = false
			} else if serverCfg.Enabled {
				// The old configuration uses enabled and is migrated to external_mcp_enable
				serverCfg.ExternalMCPEnable = true
			} else {
				// None are set, the default is enabled
				serverCfg.ExternalMCPEnable = true
			}
			cfg.ExternalMCP.Servers[name] = serverCfg
		}
	}

	// Load role configuration from role directory
	if cfg.RolesDir != "" {
		configDir := filepath.Dir(path)
		rolesDir := cfg.RolesDir

		// If it is a relative path, it is relative to the directory where the configuration file is located.
		if !filepath.IsAbs(rolesDir) {
			rolesDir = filepath.Join(configDir, rolesDir)
		}

		roles, err := LoadRolesFromDir(rolesDir)
		if err != nil {
			return nil, fmt.Errorf("Failed to load role configuration from role directory: %w", err)
		}

		cfg.Roles = roles
	} else {
		// If roles_dir is not configured, initializes to an empty map
		if cfg.Roles == nil {
			cfg.Roles = make(map[string]RoleConfig)
		}
	}

	return &cfg, nil
}

func generateStrongPassword(length int) (string, error) {
	if length <= 0 {
		length = 24
	}

	bytesLen := length
	randomBytes := make([]byte, bytesLen)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}

	password := base64.RawURLEncoding.EncodeToString(randomBytes)
	if len(password) > length {
		password = password[:length]
	}
	return password, nil
}

func PersistAuthPassword(path, password string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	inAuthBlock := false
	authIndent := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inAuthBlock {
			if strings.HasPrefix(trimmed, "auth:") {
				inAuthBlock = true
				authIndent = len(line) - len(strings.TrimLeft(line, " "))
			}
			continue
		}

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		leadingSpaces := len(line) - len(strings.TrimLeft(line, " "))
		if leadingSpaces <= authIndent {
			// Leave the auth block
			inAuthBlock = false
			authIndent = -1
			// Keep looking for other auth blocks (theoretically there are none)
			if strings.HasPrefix(trimmed, "auth:") {
				inAuthBlock = true
				authIndent = leadingSpaces
			}
			continue
		}

		if strings.HasPrefix(strings.TrimSpace(line), "password:") {
			prefix := line[:len(line)-len(strings.TrimLeft(line, " "))]
			comment := ""
			if idx := strings.Index(line, "#"); idx >= 0 {
				comment = strings.TrimRight(line[idx:], " ")
			}

			newLine := fmt.Sprintf("%spassword: %s", prefix, password)
			if comment != "" {
				if !strings.HasPrefix(comment, " ") {
					newLine += " "
				}
				newLine += comment
			}
			lines[i] = newLine
			break
		}
	}

	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
}

func PrintGeneratedPasswordWarning(password string, persisted bool, persistErr string) {
	if strings.TrimSpace(password) == "" {
		return
	}

	if persisted {
		fmt.Println("[CyberStrikeAI] ✅ The web login password has been automatically generated and written for you.")
	} else {
		if persistErr != "" {
			fmt.Printf("[CyberStrikeAI] ⚠️ Unable to automatically write the password in the configuration file: %s\n", persistErr)
		} else {
			fmt.Println("[CyberStrikeAI] ⚠️ The password in the configuration file cannot be automatically written.")
		}
		fmt.Println("Please manually write the following random password into auth.password of config.yaml:")
	}

	fmt.Println("----------------------------------------------------------------")
	fmt.Println("CyberStrikeAI Auto-Generated Web Password")
	fmt.Printf("Password: %s\n", password)
	fmt.Println("WARNING: Anyone with this password can fully control CyberStrikeAI.")
	fmt.Println("Please store it securely and change it in config.yaml as soon as possible.")
	fmt.Println("WARNING: Anyone holding this password will have full control of CyberStrikeAI.")
	fmt.Println("Please keep it safe and modify auth.password in config.yaml as soon as possible!")
	fmt.Println("----------------------------------------------------------------")
}

// LoadToolsFromDir loads all tool configuration files from the directory
func LoadToolsFromDir(dir string) ([]ToolConfig, error) {
	var tools []ToolConfig

	// Check if directory exists
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return tools, nil // If the directory does not exist, an empty list will be returned and no error will be reported.
	}

	// Read all .yaml and .yml files in the directory
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("Failed to read tools directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		filePath := filepath.Join(dir, name)
		tool, err := LoadToolFromFile(filePath)
		if err != nil {
			// Log errors but continue loading other files
			fmt.Printf("Warning: Failed to load tool configuration file %s: %v\n", filePath, err)
			continue
		}

		tools = append(tools, *tool)
	}

	return tools, nil
}

// LoadToolFromFile loads tool configuration from a single file
func LoadToolFromFile(path string) (*ToolConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("Failed to read file: %w", err)
	}

	var tool ToolConfig
	if err := yaml.Unmarshal(data, &tool); err != nil {
		return nil, fmt.Errorf("Parsing tool configuration failed: %w", err)
	}

	// Validate required fields
	if tool.Name == "" {
		return nil, fmt.Errorf("Tool name cannot be empty")
	}
	if tool.Command == "" {
		return nil, fmt.Errorf("Tool command cannot be empty")
	}

	return &tool, nil
}

// LoadRolesFromDir loads all role profiles from a directory
func LoadRolesFromDir(dir string) (map[string]RoleConfig, error) {
	roles := make(map[string]RoleConfig)

	// Check if directory exists
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return roles, nil // If the directory does not exist, an empty map will be returned and no error will be reported.
	}

	// Read all .yaml and .yml files in the directory
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("Failed to read role directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		filePath := filepath.Join(dir, name)
		role, err := LoadRoleFromFile(filePath)
		if err != nil {
			// Log errors but continue loading other files
			fmt.Printf("Warning: Failed to load role profile %s: %v\n", filePath, err)
			continue
		}

		// Use role name as key
		roleName := role.Name
		if roleName == "" {
			// If the role name is empty, use the file name (minus the extension) as the name
			roleName = strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".yml")
			role.Name = roleName
		}

		roles[roleName] = *role
	}

	return roles, nil
}

// LoadRoleFromFile loads role configuration from a single file
func LoadRoleFromFile(path string) (*RoleConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("Failed to read file: %w", err)
	}

	var role RoleConfig
	if err := yaml.Unmarshal(data, &role); err != nil {
		return nil, fmt.Errorf("Failed to parse role configuration: %w", err)
	}

	// Handle icon field: if it contains Unicode escape format (\U0001F3C6), convert to actual Unicode character
	// Go's yaml library may not automatically parse \U escape sequences and needs to be converted manually
	if role.Icon != "" {
		icon := role.Icon
		// Remove possible quotes
		icon = strings.Trim(icon, `"`)

		// Check if the Unicode escape format is \U0001F3C6 (8-digit hexadecimal) or \uXXXX (4-digit hexadecimal)
		if len(icon) >= 3 && icon[0] == '\\' {
			if icon[1] == 'U' && len(icon) >= 10 {
				// \U0001F3C6 format (8-digit hexadecimal)
				if codePoint, err := strconv.ParseInt(icon[2:10], 16, 32); err == nil {
					role.Icon = string(rune(codePoint))
				}
			} else if icon[1] == 'u' && len(icon) >= 6 {
				// \uXXXX format (4-digit hexadecimal)
				if codePoint, err := strconv.ParseInt(icon[2:6], 16, 32); err == nil {
					role.Icon = string(rune(codePoint))
				}
			}
		}
	}

	// Validate required fields
	if role.Name == "" {
		// If name is empty, try to get from filename
		baseName := filepath.Base(path)
		role.Name = strings.TrimSuffix(strings.TrimSuffix(baseName, ".yaml"), ".yml")
	}

	return &role, nil
}

func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 8080,
		},
		Log: LogConfig{
			Level:  "info",
			Output: "stdout",
		},
		MCP: MCPConfig{
			Enabled: true,
			Host:    "0.0.0.0",
			Port:    8081,
		},
		OpenAI: OpenAIConfig{
			BaseURL:        "https://api.openai.com/v1",
			Model:          "gpt-4",
			MaxTotalTokens: 120000,
		},
		Agent: AgentConfig{
			MaxIterations: 30, // Default maximum number of iterations
		},
		Security: SecurityConfig{
			Tools:    []ToolConfig{}, // Tool configuration should be loaded from config.yaml or tools/ directory
			ToolsDir: "tools",        // Default tool directory
		},
		Database: DatabaseConfig{
			Path:            "data/conversations.db",
			KnowledgeDBPath: "data/knowledge.db", // Default knowledge base database path
		},
		Auth: AuthConfig{
			SessionDurationHours: 12,
		},
		Knowledge: KnowledgeConfig{
			Enabled:  true,
			BasePath: "knowledge_base",
			Embedding: EmbeddingConfig{
				Provider: "openai",
				Model:    "text-embedding-3-small",
				BaseURL:  "https://api.openai.com/v1",
			},
			Retrieval: RetrievalConfig{
				TopK:                5,
				SimilarityThreshold: 0.7,
				HybridWeight:        0.7,
			},
		},
	}
}

// KnowledgeConfig knowledge base configuration
type KnowledgeConfig struct {
	Enabled   bool            `yaml:"enabled" json:"enabled"`     // Whether to enable knowledge retrieval
	BasePath  string          `yaml:"base_path" json:"base_path"` // Knowledge base path
	Embedding EmbeddingConfig `yaml:"embedding" json:"embedding"`
	Retrieval RetrievalConfig `yaml:"retrieval" json:"retrieval"`
}

// EmbeddingConfig embedding configuration
type EmbeddingConfig struct {
	Provider string `yaml:"provider" json:"provider"` // Embed model provider
	Model    string `yaml:"model" json:"model"`       // Model name
	BaseURL  string `yaml:"base_url" json:"base_url"` // API Base URL
	APIKey   string `yaml:"api_key" json:"api_key"`   // API Key (inherited from OpenAI configuration)
}

// RetrievalConfig Retrieve configuration
type RetrievalConfig struct {
	TopK                int     `yaml:"top_k" json:"top_k"`                               // SearchTop-K
	SimilarityThreshold float64 `yaml:"similarity_threshold" json:"similarity_threshold"` // Similarity threshold
	HybridWeight        float64 `yaml:"hybrid_weight" json:"hybrid_weight"`               // Vector search weight (0-1)
}

// RolesConfig role configuration (deprecated, use map[string]RoleConfig instead)
// This type is retained for compatibility with older code, but it is recommended to use map[string]RoleConfig directly
type RolesConfig struct {
	Roles map[string]RoleConfig `yaml:"roles,omitempty" json:"roles,omitempty"`
}

// RoleConfig single role configuration
type RoleConfig struct {
	Name        string   `yaml:"name" json:"name"`                         // Character name
	Description string   `yaml:"description" json:"description"`           // Role description
	UserPrompt  string   `yaml:"user_prompt" json:"user_prompt"`           // User prompt word (appended to the front of user message)
	Icon        string   `yaml:"icon,omitempty" json:"icon,omitempty"`     // Character icon (optional)
	Tools       []string `yaml:"tools,omitempty" json:"tools,omitempty"`   // List of associated tools (toolKey format, such as "toolName" or "mcpName::toolName")
	MCPs        []string `yaml:"mcps,omitempty" json:"mcps,omitempty"`     // Backward compatibility: list of associated MCP servers (deprecated, use tools instead)
	Skills      []string `yaml:"skills,omitempty" json:"skills,omitempty"` // Associated skills list (skill name list, the contents of these skills will be read before executing the task)
	Enabled     bool     `yaml:"enabled" json:"enabled"`                   // Whether to enable
}
