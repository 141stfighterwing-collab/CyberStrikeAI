package builtin

// Built-in tool name constants
// All code where built-in tool names are used should use these constants instead of hard-coded strings
const (
	// Vulnerability management tools
	ToolRecordVulnerability = "record_vulnerability"

	// Knowledge base tools
	ToolListKnowledgeRiskTypes = "list_knowledge_risk_types"
	ToolSearchKnowledgeBase    = "search_knowledge_base"

	// SkillsTools
	ToolListSkills    = "list_skills"
	ToolReadSkill     = "read_skill"
)

// IsBuiltinTool checks whether the tool name is a built-in tool
func IsBuiltinTool(toolName string) bool {
	switch toolName {
	case ToolRecordVulnerability,
		ToolListKnowledgeRiskTypes,
		ToolSearchKnowledgeBase,
		ToolListSkills,
		ToolReadSkill:
		return true
	default:
		return false
	}
}

// GetAllBuiltinTools returns a list of all built-in tool names
func GetAllBuiltinTools() []string {
	return []string{
		ToolRecordVulnerability,
		ToolListKnowledgeRiskTypes,
		ToolSearchKnowledgeBase,
		ToolListSkills,
		ToolReadSkill,
	}
}
