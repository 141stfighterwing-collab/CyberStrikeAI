/**
* Built-in tool name constants
* All front-end code where built-in tool names are used should use these constants instead of hard-coded strings
 * 
* Note: These constants must be consistent with the constants in the backend's internal/mcp/builtin/constants.go
 */

// Built-in tool name constants
const BuiltinTools = {
    // Vulnerability management tools
    RECORD_VULNERABILITY: 'record_vulnerability',
    
    // Knowledge base tools
    LIST_KNOWLEDGE_RISK_TYPES: 'list_knowledge_risk_types',
    SEARCH_KNOWLEDGE_BASE: 'search_knowledge_base'
};

// Check if it is a built-in tool
function isBuiltinTool(toolName) {
    return Object.values(BuiltinTools).includes(toolName);
}

// Get a list of all built-in tool names
function getAllBuiltinTools() {
    return Object.values(BuiltinTools);
}

