package security

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/storage"

	"go.uber.org/zap"
)

// Executor Security Tool Executor
type Executor struct {
	config        *config.SecurityConfig
	toolIndex     map[string]*config.ToolConfig // Tool index for O(1) lookups
	mcpServer     *mcp.Server
	logger        *zap.Logger
	resultStorage ResultStorage // Result storage (for query tools)
}

// ResultStorage result storage interface (use the type of storage package directly)
type ResultStorage interface {
	SaveResult(executionID string, toolName string, result string) error
	GetResult(executionID string) (string, error)
	GetResultPage(executionID string, page int, limit int) (*storage.ResultPage, error)
	SearchResult(executionID string, keyword string, useRegex bool) ([]string, error)
	FilterResult(executionID string, filter string, useRegex bool) ([]string, error)
	GetResultMetadata(executionID string) (*storage.ResultMetadata, error)
	GetResultPath(executionID string) string
	DeleteResult(executionID string) error
}

// NewExecutor creates a new executor
func NewExecutor(cfg *config.SecurityConfig, mcpServer *mcp.Server, logger *zap.Logger) *Executor {
	executor := &Executor{
		config:        cfg,
		toolIndex:     make(map[string]*config.ToolConfig),
		mcpServer:     mcpServer,
		logger:        logger,
		resultStorage: nil, // Set later via SetResultStorage
	}
	// Build tool index
	executor.buildToolIndex()
	return executor
}

// SetResultStorage sets the result storage
func (e *Executor) SetResultStorage(storage ResultStorage) {
	e.resultStorage = storage
}

// BuildToolIndex builds tool index, optimizing O(n) lookup to O(1)
func (e *Executor) buildToolIndex() {
	e.toolIndex = make(map[string]*config.ToolConfig)
	for i := range e.config.Tools {
		if e.config.Tools[i].Enabled {
			e.toolIndex[e.config.Tools[i].Name] = &e.config.Tools[i]
		}
	}
	e.logger.Info("Tool index construction completed",
		zap.Int("totalTools", len(e.config.Tools)),
		zap.Int("enabledTools", len(e.toolIndex)),
	)
}

// ExecuteTool execution security tool
func (e *Executor) ExecuteTool(ctx context.Context, toolName string, args map[string]interface{}) (*mcp.ToolResult, error) {
	e.logger.Info("ExecuteTool is called",
		zap.String("toolName", toolName),
		zap.Any("args", args),
	)

	// Special treatment: the exec tool directly executes system commands
	if toolName == "exec" {
		e.logger.Info("Execute exec tool")
		return e.executeSystemCommand(ctx, args)
	}

	// Using index lookup tool configuration (O(1) lookup)
	toolConfig, exists := e.toolIndex[toolName]
	if !exists {
		e.logger.Error("Tool not found or not enabled",
			zap.String("toolName", toolName),
			zap.Int("totalTools", len(e.config.Tools)),
			zap.Int("enabledTools", len(e.toolIndex)),
		)
		return nil, fmt.Errorf("Tool %s not found or not enabled", toolName)
	}

	e.logger.Info("Find tool configuration",
		zap.String("toolName", toolName),
		zap.String("command", toolConfig.Command),
		zap.Strings("args", toolConfig.Args),
	)

	// Special handling: internal tools (command starts with "internal:")
	if strings.HasPrefix(toolConfig.Command, "internal:") {
		e.logger.Info("Execute internal tools",
			zap.String("toolName", toolName),
			zap.String("command", toolConfig.Command),
		)
		return e.executeInternalTool(ctx, toolName, toolConfig.Command, args)
	}

	// Build command - uses different argument formats depending on tool type
	cmdArgs := e.buildCommandArgs(toolName, toolConfig, args)

	e.logger.Info("Build command parameters completed",
		zap.String("toolName", toolName),
		zap.Strings("cmdArgs", cmdArgs),
		zap.Int("argsCount", len(cmdArgs)),
	)

	// Verify command parameters
	if len(cmdArgs) == 0 {
		e.logger.Warn("Command parameters are empty",
			zap.String("toolName", toolName),
			zap.Any("inputArgs", args),
		)
		return &mcp.ToolResult{
			Content: []mcp.Content{
				{
					Type: "text",
					Text: fmt.Sprintf("Error: Tool %s is missing a required parameter. Parameters received: %v", toolName, args),
				},
			},
			IsError: true,
		}, nil
	}

	// Execute command
	cmd := exec.CommandContext(ctx, toolConfig.Command, cmdArgs...)

	e.logger.Info("Execute security tools",
		zap.String("tool", toolName),
		zap.Strings("args", cmdArgs),
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if the exit code is in the allowed list
		exitCode := getExitCode(err)
		if exitCode != nil && toolConfig.AllowedExitCodes != nil {
			for _, allowedCode := range toolConfig.AllowedExitCodes {
				if *exitCode == allowedCode {
					e.logger.Info("Tool execution completed (exit code is in allowed list)",
						zap.String("tool", toolName),
						zap.Int("exitCode", *exitCode),
						zap.String("output", string(output)),
					)
					return &mcp.ToolResult{
						Content: []mcp.Content{
							{
								Type: "text",
								Text: string(output),
							},
						},
						IsError: false,
					}, nil
				}
			}
		}

		e.logger.Error("Tool execution failed",
			zap.String("tool", toolName),
			zap.Error(err),
			zap.Int("exitCode", getExitCodeValue(err)),
			zap.String("output", string(output)),
		)
		return &mcp.ToolResult{
			Content: []mcp.Content{
				{
					Type: "text",
					Text: fmt.Sprintf("Tool execution failed: %v\nOutput: %s", err, string(output)),
				},
			},
			IsError: true,
		}, nil
	}

	e.logger.Info("Tool execution successful",
		zap.String("tool", toolName),
		zap.String("output", string(output)),
	)

	return &mcp.ToolResult{
		Content: []mcp.Content{
			{
				Type: "text",
				Text: string(output),
			},
		},
		IsError: false,
	}, nil
}

// RegisterTools registers tools to the MCP server
func (e *Executor) RegisterTools(mcpServer *mcp.Server) {
	e.logger.Info("Start registration tool",
		zap.Int("totalTools", len(e.config.Tools)),
		zap.Int("enabledTools", len(e.toolIndex)),
	)

	// Rebuild the index (in case of configuration updates)
	e.buildToolIndex()

	for i, toolConfig := range e.config.Tools {
		if !toolConfig.Enabled {
			e.logger.Debug("Skip unenabled tools",
				zap.String("tool", toolConfig.Name),
			)
			continue
		}

		// Create a copy of tool configuration to avoid closure issues
		toolName := toolConfig.Name
		toolConfigCopy := toolConfig

		// The description exposed to the AI/API depends on the configuration: short_description or description
		useFullDescription := strings.TrimSpace(strings.ToLower(e.config.ToolDescriptionMode)) == "full"
		shortDesc := toolConfigCopy.ShortDescription
		if shortDesc == "" {
			// If there is no short description, extract the first line or first 10000 characters from the long description
			desc := toolConfigCopy.Description
			if len(desc) > 10000 {
				if idx := strings.Index(desc, "\n"); idx > 0 && idx < 10000 {
					shortDesc = strings.TrimSpace(desc[:idx])
				} else {
					shortDesc = desc[:10000] + "..."
				}
			} else {
				shortDesc = desc
			}
		}
		if useFullDescription {
			shortDesc = "" // Clear ShortDescription when using description, and the downstream will fall back to Description
		}

		tool := mcp.Tool{
			Name:             toolConfigCopy.Name,
			Description:      toolConfigCopy.Description,
			ShortDescription: shortDesc,
			InputSchema:      e.buildInputSchema(&toolConfigCopy),
		}

		handler := func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
			e.logger.Info("Tool handler is called",
				zap.String("toolName", toolName),
				zap.Any("args", args),
			)
			return e.ExecuteTool(ctx, toolName, args)
		}

		mcpServer.RegisterTool(tool, handler)
		e.logger.Info("Security tool registration successful",
			zap.String("tool", toolConfigCopy.Name),
			zap.String("command", toolConfigCopy.Command),
			zap.Int("index", i),
		)
	}

	e.logger.Info("Tool registration completed",
		zap.Int("registeredCount", len(e.config.Tools)),
	)
}

// BuildCommandArgs build command parameters
func (e *Executor) buildCommandArgs(toolName string, toolConfig *config.ToolConfig, args map[string]interface{}) []string {
	cmdArgs := make([]string, 0)

	// If parameter mapping is defined in the configuration, use the mapping rules in the configuration
	if len(toolConfig.Parameters) > 0 {
		// Check if there is a scan_type parameter, if so replace the default scan type parameter
		hasScanType := false
		var scanTypeValue string
		if scanType, ok := args["scan_type"].(string); ok && scanType != "" {
			hasScanType = true
			scanTypeValue = scanType
		}

		// Add fixed parameters (if scan_type is specified, you may need to filter out the default scan type parameters)
		if hasScanType && toolName == "nmap" {
			// For nmap, if scan_type is specified, skip the default -sT -sV -sC
			// These parameters will be replaced by the scan_type parameter
		} else {
			cmdArgs = append(cmdArgs, toolConfig.Args...)
		}

		// Sort by positional parameters
		positionalParams := make([]config.ParameterConfig, 0)
		flagParams := make([]config.ParameterConfig, 0)

		for _, param := range toolConfig.Parameters {
			if param.Position != nil {
				positionalParams = append(positionalParams, param)
			} else {
				flagParams = append(flagParams, param)
			}
		}

		// For tools that require subcommands (such as gobuster dir), position 0 must immediately follow the command name and before any flags
		for _, param := range positionalParams {
			if param.Name == "additional_args" || param.Name == "scan_type" || param.Name == "action" {
				continue
			}
			if param.Position != nil && *param.Position == 0 {
				value := e.getParamValue(args, param)
				if value == nil && param.Default != nil {
					value = param.Default
				}
				if value != nil {
					cmdArgs = append(cmdArgs, e.formatParamValue(param, value))
				}
				break
			}
		}

		// Handling flag parameters
		for _, param := range flagParams {
			// Skip special parameters, they will be processed separately later
			// The action parameter is only used for the internal logic of the tool and is not passed to the command.
			if param.Name == "additional_args" || param.Name == "scan_type" || param.Name == "action" {
				continue
			}

			value := e.getParamValue(args, param)
			if value == nil {
				if param.Required {
					// Required parameters are missing, and an empty array is returned to allow the upper layer to handle the error.
					e.logger.Warn("Missing required flag parameter",
						zap.String("tool", toolName),
						zap.String("param", param.Name),
					)
					return []string{}
				}
				continue
			}

			// Special handling of boolean values: if false, skip; if true, only add flag
			if param.Type == "bool" {
				var boolVal bool
				var ok bool

				// Try multiple type conversions
				if boolVal, ok = value.(bool); ok {
					// Already a Boolean value
				} else if numVal, ok := value.(float64); ok {
					// JSON number type (float64)
					boolVal = numVal != 0
					ok = true
				} else if numVal, ok := value.(int); ok {
					// Int type
					boolVal = numVal != 0
					ok = true
				} else if strVal, ok := value.(string); ok {
					// String type
					boolVal = strVal == "true" || strVal == "1" || strVal == "yes"
					ok = true
				}

				if ok {
					if !boolVal {
						continue // No parameters are added when false
					}
					// When true, only the flag is added, not the value
					if param.Flag != "" {
						cmdArgs = append(cmdArgs, param.Flag)
					}
					continue
				}
			}

			format := param.Format
			if format == "" {
				format = "flag" // Default format
			}

			switch format {
			case "flag":
				// --flag value or -f value
				if param.Flag != "" {
					cmdArgs = append(cmdArgs, param.Flag)
				}
				formattedValue := e.formatParamValue(param, value)
				if formattedValue != "" {
					cmdArgs = append(cmdArgs, formattedValue)
				}
			case "combined":
				// --flag=value or -f=value
				if param.Flag != "" {
					cmdArgs = append(cmdArgs, fmt.Sprintf("%s=%s", param.Flag, e.formatParamValue(param, value)))
				} else {
					cmdArgs = append(cmdArgs, e.formatParamValue(param, value))
				}
			case "template":
				// Use template strings
				if param.Template != "" {
					template := param.Template
					template = strings.ReplaceAll(template, "{flag}", param.Flag)
					template = strings.ReplaceAll(template, "{value}", e.formatParamValue(param, value))
					template = strings.ReplaceAll(template, "{name}", param.Name)
					cmdArgs = append(cmdArgs, strings.Fields(template)...)
				} else {
					// If there is no template, use the default format
					if param.Flag != "" {
						cmdArgs = append(cmdArgs, param.Flag)
					}
					cmdArgs = append(cmdArgs, e.formatParamValue(param, value))
				}
			case "positional":
				// Positional parameters (already handled above)
				cmdArgs = append(cmdArgs, e.formatParamValue(param, value))
			default:
				// Default: add value directly
				cmdArgs = append(cmdArgs, e.formatParamValue(param, value))
			}
		}

		// Then handle the positional parameters (positional parameters usually come after the flag parameters)
		// Sort positional parameters by position
		// First find the largest position value and determine how many positions need to be processed
		maxPosition := -1
		for _, param := range positionalParams {
			if param.Position != nil && *param.Position > maxPosition {
				maxPosition = *param.Position
			}
		}

		// Process parameters in positional order to ensure that even if some positions have no parameters or use default values, they are passed correctly
		// Position 0 has been inserted at the front (subcommands take precedence), starting from 1 here
		for i := 0; i <= maxPosition; i++ {
			if i == 0 {
				continue
			}
			for _, param := range positionalParams {
				// Skip special parameters, they will be processed separately later
				// The action parameter is only used for the internal logic of the tool and is not passed to the command.
				if param.Name == "additional_args" || param.Name == "scan_type" || param.Name == "action" {
					continue
				}

				if param.Position != nil && *param.Position == i {
					value := e.getParamValue(args, param)
					if value == nil {
						if param.Required {
							// Required parameters are missing, and an empty array is returned to allow the upper layer to handle the error.
							e.logger.Warn("Missing required positional argument",
								zap.String("tool", toolName),
								zap.String("param", param.Name),
								zap.Int("position", *param.Position),
							)
							return []string{}
						}
						// For non-required parameters, if the value is nil, try to use the default value
						if param.Default != nil {
							value = param.Default
						} else {
							// If there is no default value, skip this position and continue processing the next position
							break
						}
					}
					// Add to command parameters only if value is not nil
					if value != nil {
						cmdArgs = append(cmdArgs, e.formatParamValue(param, value))
					}
					break
				}
			}
			// If the corresponding parameter is not found at a certain position, continue processing the next position.
			// This ensures that the positional parameters are in the correct order
		}

		// Special processing: additional_args parameter (needs to be split into multiple parameters by spaces)
		if additionalArgs, ok := args["additional_args"].(string); ok && additionalArgs != "" {
			// Split by spaces, but keep content within quotes
			additionalArgsList := e.parseAdditionalArgs(additionalArgs)
			cmdArgs = append(cmdArgs, additionalArgsList...)
		}

		// Special processing: scan_type parameter (needs to be split by spaces and inserted into the appropriate position)
		if hasScanType {
			scanTypeArgs := e.parseAdditionalArgs(scanTypeValue)
			if len(scanTypeArgs) > 0 {
				// For nmap, scan_type should replace the default scan type parameter
				// Since we've skipped the default args, we now need to insert scan_type in place
				// Find the position of the target parameter (usually the last positional parameter)
				insertPos := len(cmdArgs)
				for i := len(cmdArgs) - 1; i >= 0; i-- {
					// Target is usually the last non-flag argument
					if !strings.HasPrefix(cmdArgs[i], "-") {
						insertPos = i
						break
					}
				}
				// Insert scan_type parameter before target
				newArgs := make([]string, 0, len(cmdArgs)+len(scanTypeArgs))
				newArgs = append(newArgs, cmdArgs[:insertPos]...)
				newArgs = append(newArgs, scanTypeArgs...)
				newArgs = append(newArgs, cmdArgs[insertPos:]...)
				cmdArgs = newArgs
			}
		}

		return cmdArgs
	}

	// If no parameter configuration is defined, fixed parameters and general processing are used
	// Add fixed parameters
	cmdArgs = append(cmdArgs, toolConfig.Args...)

	// General processing: convert parameters into command line arguments
	for key, value := range args {
		if key == "_tool_name" {
			continue
		}
		// Use --key value format
		cmdArgs = append(cmdArgs, fmt.Sprintf("--%s", key))
		if strValue, ok := value.(string); ok {
			cmdArgs = append(cmdArgs, strValue)
		} else {
			cmdArgs = append(cmdArgs, fmt.Sprintf("%v", value))
		}
	}

	return cmdArgs
}

// ParseAdditionalArgs parses the additional_args string, splitting it on spaces but retaining the content within quotes
func (e *Executor) parseAdditionalArgs(argsStr string) []string {
	if argsStr == "" {
		return []string{}
	}

	result := make([]string, 0)
	var current strings.Builder
	inQuotes := false
	var quoteChar rune
	escapeNext := false

	runes := []rune(argsStr)
	for i := 0; i < len(runes); i++ {
		r := runes[i]

		if escapeNext {
			current.WriteRune(r)
			escapeNext = false
			continue
		}

		if r == '\\' {
			// Check if next character is a quote
			if i+1 < len(runes) && (runes[i+1] == '"' || runes[i+1] == '\'') {
				// Escaped quotes: backslashes are skipped and quotes are written as normal characters
				i++
				current.WriteRune(runes[i])
			} else {
				// Other escape characters: write backslash, the next character will be processed in the next iteration
				escapeNext = true
				current.WriteRune(r)
			}
			continue
		}

		if !inQuotes && (r == '"' || r == '\'') {
			inQuotes = true
			quoteChar = r
			continue
		}

		if inQuotes && r == quoteChar {
			inQuotes = false
			quoteChar = 0
			continue
		}

		if !inQuotes && (r == ' ' || r == '\t' || r == '\n') {
			if current.Len() > 0 {
				result = append(result, current.String())
				current.Reset()
			}
			continue
		}

		current.WriteRune(r)
	}

	// Process the last parameter if present
	if current.Len() > 0 {
		result = append(result, current.String())
	}

	// If the parsed result is empty, use simple space splitting as a fallback solution
	if len(result) == 0 {
		result = strings.Fields(argsStr)
	}

	return result
}

// GetParamValue gets parameter value, supports default value
func (e *Executor) getParamValue(args map[string]interface{}, param config.ParameterConfig) interface{} {
	// Get value from parameter
	if value, ok := args[param.Name]; ok && value != nil {
		return value
	}

	// If the parameter is required but not provided, return nil (let the upper layer handle the error)
	if param.Required {
		return nil
	}

	// Return to default value
	return param.Default
}

// FormatParamValue format parameter value
func (e *Executor) formatParamValue(param config.ParameterConfig, value interface{}) string {
	switch param.Type {
	case "bool":
		// Boolean values ​​should be handled in the upper layer and should not be called here
		if boolVal, ok := value.(bool); ok {
			return fmt.Sprintf("%v", boolVal)
		}
		return "false"
	case "array":
		// Array: Convert to comma separated string
		if arr, ok := value.([]interface{}); ok {
			strs := make([]string, 0, len(arr))
			for _, item := range arr {
				strs = append(strs, fmt.Sprintf("%v", item))
			}
			return strings.Join(strs, ",")
		}
		return fmt.Sprintf("%v", value)
	case "object":
		// Object/Dictionary: serialized to JSON string
		if jsonBytes, err := json.Marshal(value); err == nil {
			return string(jsonBytes)
		}
		// If JSON serialization fails, fall back to default formatting
		return fmt.Sprintf("%v", value)
	default:
		formattedValue := fmt.Sprintf("%v", value)
		// Special handling: For the ports parameter (usually the port parameter of tools such as nmap), clean up the spaces
		// Nmap does not accept spaces in the port list, for example "80,443, 22" should become "80,443,22"
		if param.Name == "ports" {
			// Remove all spaces but keep commas and other characters
			formattedValue = strings.ReplaceAll(formattedValue, " ", "")
		}
		return formattedValue
	}
}

// IsBackgroundCommand detects whether the command is a fully background command (with an & symbol at the end, but not within quotes)
// Note: This situation of command1 & command2 is not completely background, because command2 will be executed in the foreground.
func (e *Executor) isBackgroundCommand(command string) bool {
	// Remove leading and trailing spaces
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}

	// Check for all ampersands in the command that are not inside quotes
	// Find the last & symbol and check if it is at the end of the command
	inSingleQuote := false
	inDoubleQuote := false
	escaped := false
	lastAmpersandPos := -1

	for i, r := range command {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '\'' && !inDoubleQuote {
			inSingleQuote = !inSingleQuote
			continue
		}
		if r == '"' && !inSingleQuote {
			inDoubleQuote = !inDoubleQuote
			continue
		}
		if r == '&' && !inSingleQuote && !inDoubleQuote {
			// Check if there are any spaces or newlines before and after & (make sure it is a standalone & and not part of the variable name)
			isStandalone := false

			// Check for: spaces, tabs, newlines, or the beginning of a command
			if i == 0 {
				isStandalone = true
			} else {
				prev := command[i-1]
				if prev == ' ' || prev == '\t' || prev == '\n' || prev == '\r' {
					isStandalone = true
				}
			}

			// Check for following: spaces, tabs, newlines, or the end of the command
			if isStandalone {
				if i == len(command)-1 {
					// At the end, it is definitely independent &
					lastAmpersandPos = i
				} else {
					next := command[i+1]
					if next == ' ' || next == '\t' || next == '\n' || next == '\r' {
						// If there is a space after it, it is independent &
						lastAmpersandPos = i
					}
				}
			}
		}
	}

	// If the & symbol is not found, it is not a background command
	if lastAmpersandPos == -1 {
		return false
	}

	// Check if there is non-empty content after the last &
	afterAmpersand := strings.TrimSpace(command[lastAmpersandPos+1:])
	if afterAmpersand == "" {
		// & with only whitespace characters at the end or after, this is a completely background command
		// Check if there is content before &
		beforeAmpersand := strings.TrimSpace(command[:lastAmpersandPos])
		return beforeAmpersand != ""
	}

	// If there is non-empty content after &, it means command1 & command2.
	// In this case, command2 will be executed in the foreground, so it is not considered a complete background command.
	return false
}

// ExecuteSystemCommand executes system commands
func (e *Executor) executeSystemCommand(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
	// Get command
	command, ok := args["command"].(string)
	if !ok {
		return &mcp.ToolResult{
			Content: []mcp.Content{
				{
					Type: "text",
					Text: "Error: command parameter missing",
				},
			},
			IsError: true,
		}, nil
	}

	if command == "" {
		return &mcp.ToolResult{
			Content: []mcp.Content{
				{
					Type: "text",
					Text: "Error: command parameter cannot be empty",
				},
			},
			IsError: true,
		}, nil
	}

	// Security check: log executed commands
	e.logger.Warn("Execute system commands",
		zap.String("command", command),
	)

	// Get the shell type (optional, default is sh)
	shell := "sh"
	if s, ok := args["shell"].(string); ok && s != "" {
		shell = s
	}

	// Get working directory (optional)
	workDir := ""
	if wd, ok := args["workdir"].(string); ok && wd != "" {
		workDir = wd
	}

	// Detect whether it is a background command (contains the & symbol, but not within quotes)
	isBackground := e.isBackgroundCommand(command)

	// Build command
	var cmd *exec.Cmd
	if workDir != "" {
		cmd = exec.CommandContext(ctx, shell, "-c", command)
		cmd.Dir = workDir
	} else {
		cmd = exec.CommandContext(ctx, shell, "-c", command)
	}

	// Execute command
	e.logger.Info("Execute system commands",
		zap.String("command", command),
		zap.String("shell", shell),
		zap.String("workdir", workDir),
		zap.Bool("isBackground", isBackground),
	)

	// If it is a background command, use special processing to get the actual background process PID
	if isBackground {
		// Remove the ampersand at the end of the command
		commandWithoutAmpersand := strings.TrimSuffix(strings.TrimSpace(command), "&")
		commandWithoutAmpersand = strings.TrimSpace(commandWithoutAmpersand)

		// Build a new command: command & pid=$!; echo $pid
		// Use variables to save the PID to ensure that the correct background process PID can be obtained
		pidCommand := fmt.Sprintf("%s & pid=$!; echo $pid", commandWithoutAmpersand)

		// Create new command to get PID
		var pidCmd *exec.Cmd
		if workDir != "" {
			pidCmd = exec.CommandContext(ctx, shell, "-c", pidCommand)
			pidCmd.Dir = workDir
		} else {
			pidCmd = exec.CommandContext(ctx, shell, "-c", pidCommand)
		}

		// Get stdout pipe
		stdout, err := pidCmd.StdoutPipe()
		if err != nil {
			e.logger.Error("Failed to create stdout pipe",
				zap.String("command", command),
				zap.Error(err),
			)
			// If creating the pipe fails, use the PID of the shell process as fallback
			if err := pidCmd.Start(); err != nil {
				return &mcp.ToolResult{
					Content: []mcp.Content{
						{
							Type: "text",
							Text: fmt.Sprintf("Failed to start background command: %v", err),
						},
					},
					IsError: true,
				}, nil
			}
			pid := pidCmd.Process.Pid
			go pidCmd.Wait() // Wait in the background to avoid zombie processes
			return &mcp.ToolResult{
				Content: []mcp.Content{
					{
						Type: "text",
						Text: fmt.Sprintf("Background command started\nCommand: %s\nProcess ID: %d (may be inaccurate, failed to get PID)\n\nNote: The background process will continue to run and will not wait for its completion.", command, pid),
					},
				},
				IsError: false,
			}, nil
		}

		// Start command
		if err := pidCmd.Start(); err != nil {
			stdout.Close()
			e.logger.Error("Background command failed to start",
				zap.String("command", command),
				zap.Error(err),
			)
			return &mcp.ToolResult{
				Content: []mcp.Content{
					{
						Type: "text",
						Text: fmt.Sprintf("Failed to start background command: %v", err),
					},
				},
				IsError: true,
			}, nil
		}

		// Read the first line of output (PID)
		reader := bufio.NewReader(stdout)
		pidLine, err := reader.ReadString('\n')
		stdout.Close()

		var actualPid int
		if err != nil && err != io.EOF {
			e.logger.Warn("Failed to read background process PID",
				zap.String("command", command),
				zap.Error(err),
			)
			// If the read fails, use the PID of the shell process
			actualPid = pidCmd.Process.Pid
		} else {
			// Parse PID
			pidStr := strings.TrimSpace(pidLine)
			if parsedPid, err := strconv.Atoi(pidStr); err == nil {
				actualPid = parsedPid
			} else {
				e.logger.Warn("Failed to resolve background process PID",
					zap.String("command", command),
					zap.String("pidLine", pidStr),
					zap.Error(err),
				)
				// If parsing fails, use the PID of the shell process
				actualPid = pidCmd.Process.Pid
			}
		}

		// Wait for shell processes in goroutine to avoid zombie processes
		go func() {
			if err := pidCmd.Wait(); err != nil {
				e.logger.Debug("The background command shell process execution is completed",
					zap.String("command", command),
					zap.Error(err),
				)
			}
		}()

		e.logger.Info("Background command started",
			zap.String("command", command),
			zap.Int("actualPid", actualPid),
		)

		return &mcp.ToolResult{
			Content: []mcp.Content{
				{
					Type: "text",
					Text: fmt.Sprintf("Background command started\nCommand: %s\nProcess ID: %d\n\nNote: The background process will continue to run and will not wait for its completion.", command, actualPid),
				},
			},
			IsError: false,
		}, nil
	}

	// Non-background command: wait for output
	output, err := cmd.CombinedOutput()
	if err != nil {
		e.logger.Error("System command execution failed",
			zap.String("command", command),
			zap.Error(err),
			zap.String("output", string(output)),
		)
		return &mcp.ToolResult{
			Content: []mcp.Content{
				{
					Type: "text",
					Text: fmt.Sprintf("Command execution failed: %v\nOutput: %s", err, string(output)),
				},
			},
			IsError: true,
		}, nil
	}

	e.logger.Info("System command executed successfully",
		zap.String("command", command),
		zap.String("output_length", fmt.Sprintf("%d", len(output))),
	)

	return &mcp.ToolResult{
		Content: []mcp.Content{
			{
				Type: "text",
				Text: string(output),
			},
		},
		IsError: false,
	}, nil
}

// ExecuteInternalTool executes the internal tool (does not execute external commands)
func (e *Executor) executeInternalTool(ctx context.Context, toolName string, command string, args map[string]interface{}) (*mcp.ToolResult, error) {
	// Extract internal tool types (remove "internal:" prefix)
	internalToolType := strings.TrimPrefix(command, "internal:")

	e.logger.Info("Execute internal tools",
		zap.String("toolName", toolName),
		zap.String("internalToolType", internalToolType),
		zap.Any("args", args),
	)

	// Distribute processing based on internal tool type
	switch internalToolType {
	case "query_execution_result":
		return e.executeQueryExecutionResult(ctx, args)
	default:
		return &mcp.ToolResult{
			Content: []mcp.Content{
				{
					Type: "text",
					Text: fmt.Sprintf("Error: Unknown internal tool type: %s", internalToolType),
				},
			},
			IsError: true,
		}, nil
	}
}

// ExecuteQueryExecutionResult execute query execution result tool
func (e *Executor) executeQueryExecutionResult(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
	// Get execution_id parameter
	executionID, ok := args["execution_id"].(string)
	if !ok || executionID == "" {
		return &mcp.ToolResult{
			Content: []mcp.Content{
				{
					Type: "text",
					Text: "Error: execution_id parameter is required and cannot be empty",
				},
			},
			IsError: true,
		}, nil
	}

	// Get optional parameters
	page := 1
	if p, ok := args["page"].(float64); ok {
		page = int(p)
	}
	if page < 1 {
		page = 1
	}

	limit := 100
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}
	if limit < 1 {
		limit = 100
	}
	if limit > 500 {
		limit = 500 // Limit the maximum number of lines per page
	}

	search := ""
	if s, ok := args["search"].(string); ok {
		search = s
	}

	filter := ""
	if f, ok := args["filter"].(string); ok {
		filter = f
	}

	useRegex := false
	if r, ok := args["use_regex"].(bool); ok {
		useRegex = r
	}

	// Check if result storage is available
	if e.resultStorage == nil {
		return &mcp.ToolResult{
			Content: []mcp.Content{
				{
					Type: "text",
					Text: "Error: result store not initialized",
				},
			},
			IsError: true,
		}, nil
	}

	// Execute query
	var resultPage *storage.ResultPage
	var err error

	if search != "" {
		// Search mode
		matchedLines, err := e.resultStorage.SearchResult(executionID, search, useRegex)
		if err != nil {
			return &mcp.ToolResult{
				Content: []mcp.Content{
					{
						Type: "text",
						Text: fmt.Sprintf("Search failed: %v", err),
					},
				},
				IsError: true,
			}, nil
		}
		// Paginate search results
		resultPage = paginateLines(matchedLines, page, limit)
	} else if filter != "" {
		// Filter mode
		filteredLines, err := e.resultStorage.FilterResult(executionID, filter, useRegex)
		if err != nil {
			return &mcp.ToolResult{
				Content: []mcp.Content{
					{
						Type: "text",
						Text: fmt.Sprintf("Filtering failed: %v", err),
					},
				},
				IsError: true,
			}, nil
		}
		// Paging filter results
		resultPage = paginateLines(filteredLines, page, limit)
	} else {
		// Ordinary paging query
		resultPage, err = e.resultStorage.GetResultPage(executionID, page, limit)
		if err != nil {
			return &mcp.ToolResult{
				Content: []mcp.Content{
					{
						Type: "text",
						Text: fmt.Sprintf("Query failed: %v", err),
					},
				},
				IsError: true,
			}, nil
		}
	}

	// Get meta information
	metadata, err := e.resultStorage.GetResultMetadata(executionID)
	if err != nil {
		// Failure to obtain meta-information does not affect query results
		e.logger.Warn("Failed to obtain result meta information", zap.Error(err))
	}

	// Format return results
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Query results (Execution ID: %s)\n", executionID))

	if metadata != nil {
		sb.WriteString(fmt.Sprintf("Tool: %s | Size: %d bytes (%.2f KB) | Total lines: %d\n",
			metadata.ToolName, metadata.TotalSize, float64(metadata.TotalSize)/1024, metadata.TotalLines))
	}

	sb.WriteString(fmt.Sprintf("Page %d/%d, %d lines per page, %d lines total\n\n",
		resultPage.Page, resultPage.TotalPages, resultPage.Limit, resultPage.TotalLines))

	if len(resultPage.Lines) == 0 {
		sb.WriteString("No matching results found. \n")
	} else {
		for i, line := range resultPage.Lines {
			lineNum := (resultPage.Page-1)*resultPage.Limit + i + 1
			sb.WriteString(fmt.Sprintf("%d: %s\n", lineNum, line))
		}
	}

	sb.WriteString("\n")
	if resultPage.Page < resultPage.TotalPages {
		sb.WriteString(fmt.Sprintf("Tip: Use page=%d to view the next page", resultPage.Page+1))
		if search != "" {
			sb.WriteString(fmt.Sprintf(", or use search=\"%s\"Continue searching", search))
			if useRegex {
				sb.WriteString("(regular mode)")
			}
		}
		if filter != "" {
			sb.WriteString(fmt.Sprintf(", or use filter=\"%s\"Continue filtering", filter))
			if useRegex {
				sb.WriteString("(regular mode)")
			}
		}
		sb.WriteString("\n")
	}

	return &mcp.ToolResult{
		Content: []mcp.Content{
			{
				Type: "text",
				Text: sb.String(),
			},
		},
		IsError: false,
	}, nil
}

// PaginateLines Paginates a list of lines
func paginateLines(lines []string, page int, limit int) *storage.ResultPage {
	totalLines := len(lines)
	totalPages := (totalLines + limit - 1) / limit
	if page < 1 {
		page = 1
	}
	if page > totalPages && totalPages > 0 {
		page = totalPages
	}

	start := (page - 1) * limit
	end := start + limit
	if end > totalLines {
		end = totalLines
	}

	var pageLines []string
	if start < totalLines {
		pageLines = lines[start:end]
	} else {
		pageLines = []string{}
	}

	return &storage.ResultPage{
		Lines:      pageLines,
		Page:       page,
		Limit:      limit,
		TotalLines: totalLines,
		TotalPages: totalPages,
	}
}

// BuildInputSchema build input schema
func (e *Executor) buildInputSchema(toolConfig *config.ToolConfig) map[string]interface{} {
	schema := map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
		"required":   []string{},
	}

	// If parameters are defined in the configuration, the parameter definitions in the configuration will be used first.
	if len(toolConfig.Parameters) > 0 {
		properties := make(map[string]interface{})
		required := []string{}

		for _, param := range toolConfig.Parameters {
			// Skip parameters with empty names (to avoid illegal schema caused by name: null or empty in YAML)
			if strings.TrimSpace(param.Name) == "" {
				e.logger.Debug("Skip unnamed parameters",
					zap.String("tool", toolConfig.Name),
					zap.String("type", param.Type),
				)
				continue
			}
			// The conversion type is OpenAI/JSON Schema standard type (empty type defaults to string)
			openAIType := e.convertToOpenAIType(param.Type)

			prop := map[string]interface{}{
				"type":        openAIType,
				"description": param.Description,
			}

			// Add default value
			if param.Default != nil {
				prop["default"] = param.Default
			}

			// Add enum options
			if len(param.Options) > 0 {
				prop["enum"] = param.Options
			}

			properties[param.Name] = prop

			// Add to required parameter list
			if param.Required {
				required = append(required, param.Name)
			}
		}

		schema["properties"] = properties
		schema["required"] = required
		return schema
	}

	// If no parameter configuration is defined, an empty schema is returned.
	// In this case the tool may only use fixed parameters (args field)
	// Or you need to define parameters through YAML configuration file
	e.logger.Warn("The tool does not define parameter configuration and returns an empty schema.",
		zap.String("tool", toolConfig.Name),
	)
	return schema
}

// ConvertToOpenAIType converts the type in the configuration to the OpenAI/JSON Schema standard type
func (e *Executor) convertToOpenAIType(configType string) string {
	// Empty or null types are uniformly treated as strings to avoid illegal schema causing tool call failure.
	if strings.TrimSpace(configType) == "" {
		return "string"
	}
	switch configType {
	case "bool":
		return "boolean"
	case "int", "integer":
		return "number"
	case "float", "double":
		return "number"
	case "string", "array", "object":
		return configType
	default:
		// By default, the original type is returned, but a warning is logged.
		e.logger.Warn("Unknown parameter type, use original type",
			zap.String("type", configType),
		)
		return configType
	}
}

// GetExitCode extracts the exit code from the error, or returns nil if it is not an ExitError
func getExitCode(err error) *int {
	if err == nil {
		return nil
	}
	if exitError, ok := err.(*exec.ExitError); ok {
		if exitError.ProcessState != nil {
			exitCode := exitError.ExitCode()
			return &exitCode
		}
	}
	return nil
}

// GetExitCodeValue Extracts the exit code value from the error, returning -1 if it is not an ExitError
func getExitCodeValue(err error) int {
	if code := getExitCode(err); code != nil {
		return *code
	}
	return -1
}
