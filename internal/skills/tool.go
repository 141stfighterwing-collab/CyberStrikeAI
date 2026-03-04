package skills

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/mcp/builtin"

	"go.uber.org/zap"
)

// RegisterSkillsTool Register Skills tools to the MCP server
func RegisterSkillsTool(
	mcpServer *mcp.Server,
	manager *Manager,
	logger *zap.Logger,
) {
	RegisterSkillsToolWithStorage(mcpServer, manager, nil, logger)
}

// RegisterSkillsToolWithStorage registers Skills tools to the MCP server (with storage support)
func RegisterSkillsToolWithStorage(
	mcpServer *mcp.Server,
	manager *Manager,
	storage SkillStatsStorage,
	logger *zap.Logger,
) {
	// Register the first tool: Get a list of all available skills
	listSkillsTool := mcp.Tool{
		Name:             builtin.ToolListSkills,
		Description:      "Get a list of all available skills. Skills are professional knowledge documents that can be read to obtain relevant professional knowledge before performing tasks. Use this tool to view all available skills on the system, and then use the read_skill tool to read the contents of a specific skill.",
		ShortDescription: "Get a list of all available skills",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
			"required":   []string{},
		},
	}

	listSkillsHandler := func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		skills, err := manager.ListSkills()
		if err != nil {
			logger.Error("Failed to obtain skills list", zap.Error(err))
			return &mcp.ToolResult{
				Content: []mcp.Content{
					{
						Type: "text",
						Text: fmt.Sprintf("Failed to get skills list: %v", err),
					},
				},
				IsError: true,
			}, nil
		}

		if len(skills) == 0 {
			return &mcp.ToolResult{
				Content: []mcp.Content{
					{
						Type: "text",
						Text: "There are currently no skills available. \n\nSkills are professional knowledge documents that can be read to obtain relevant professional knowledge before performing tasks. You can create new skills in the skills directory.",
					},
				},
				IsError: false,
			}, nil
		}

		var result strings.Builder
		result.WriteString(fmt.Sprintf("There are %d skills available:\n\n", len(skills)))
		for i, skill := range skills {
			result.WriteString(fmt.Sprintf("%d. %s\n", i+1, skill))
		}
		result.WriteString("\nUse the read_skill tool to read the details of a specific skill. \n")
		result.WriteString("For example: read_skill(skill_name=\"sql-injection-testing\")")

		return &mcp.ToolResult{
			Content: []mcp.Content{
				{
					Type: "text",
					Text: result.String(),
				},
			},
			IsError: false,
		}, nil
	}

	mcpServer.RegisterTool(listSkillsTool, listSkillsHandler)
	logger.Info("Registered skills list tool successfully")

	// Register a second tool: read the content of a specific skill
	readSkillTool := mcp.Tool{
		Name:             builtin.ToolReadSkill,
		Description:      "Read the details of the specified skill. Skills are professional knowledge documents, including testing methods, tool usage, best practices, etc. Before performing related tasks, you can call this tool to read the content of related skills to obtain professional knowledge and guidance.",
		ShortDescription: "Read the details of the specified skill",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"skill_name": map[string]interface{}{
					"type":        "string",
					"description": "Skill name to read (required). You can use the list_skills tool to get all available skill names.",
				},
			},
			"required": []string{"skill_name"},
		},
	}

	readSkillHandler := func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		skillName, ok := args["skill_name"].(string)
		if !ok || skillName == "" {
			return &mcp.ToolResult{
				Content: []mcp.Content{
					{
						Type: "text",
						Text: "Error: The skill_name parameter is required and cannot be empty. Please use the list_skills tool to get all available skill names.",
					},
				},
				IsError: true,
			}, nil
		}

		skill, err := manager.LoadSkill(skillName)
		failed := err != nil
		now := time.Now()

		// Record call statistics
		if storage != nil {
			totalCalls := 1
			successCalls := 0
			failedCalls := 0
			if failed {
				failedCalls = 1
			} else {
				successCalls = 1
			}
			if err := storage.UpdateSkillStats(skillName, totalCalls, successCalls, failedCalls, &now); err != nil {
				logger.Warn("Failed to save Skills statistics", zap.String("skill", skillName), zap.Error(err))
			} else {
				logger.Info("Skills statistics updated",
					zap.String("skill", skillName),
					zap.Int("totalCalls", totalCalls),
					zap.Int("successCalls", successCalls),
					zap.Int("failedCalls", failedCalls))
			}
		} else {
			logger.Warn("Skills statistics storage is not configured and call statistics cannot be recorded.", zap.String("skill", skillName))
		}

		if err != nil {
			logger.Warn("Failed to read skill", zap.String("skill", skillName), zap.Error(err))
			return &mcp.ToolResult{
				Content: []mcp.Content{
					{
						Type: "text",
						Text: fmt.Sprintf("Failed to read skills: %v\n\nPlease use the list_skills tool to confirm whether the skill name is correct.", err),
					},
				},
				IsError: true,
			}, nil
		}

		var result strings.Builder
		result.WriteString(fmt.Sprintf("## Skill: %s\n\n", skill.Name))
		if skill.Description != "" {
			result.WriteString(fmt.Sprintf("**Description**: %s\n\n", skill.Description))
		}
		result.WriteString("---\n\n")
		result.WriteString(skill.Content)
		result.WriteString("\n\n---\n\n")
		result.WriteString(fmt.Sprintf("*Skill path: %s*", skill.Path))

		return &mcp.ToolResult{
			Content: []mcp.Content{
				{
					Type: "text",
					Text: result.String(),
				},
			},
			IsError: false,
		}, nil
	}

	mcpServer.RegisterTool(readSkillTool, readSkillHandler)
	logger.Info("Registered skill reading tool successfully")
}

// SkillStatsStorage Skills statistics storage interface
type SkillStatsStorage interface {
	UpdateSkillStats(skillName string, totalCalls, successCalls, failedCalls int, lastCallTime *time.Time) error
	LoadSkillStats() (map[string]*SkillStats, error)
}

// SkillStats Skills statistics
type SkillStats struct {
	SkillName    string
	TotalCalls   int
	SuccessCalls int
	FailedCalls  int
	LastCallTime *time.Time
}
