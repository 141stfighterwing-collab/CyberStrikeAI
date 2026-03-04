package attackchain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"cyberstrike-ai/internal/agent"
	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/database"
	"cyberstrike-ai/internal/openai"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Builder attack chain builder
type Builder struct {
	db           *database.DB
	logger       *zap.Logger
	openAIClient *openai.Client
	openAIConfig *config.OpenAIConfig
	tokenCounter agent.TokenCounter
	maxTokens    int // Maximum tokens limit, default 100000
}

// Node attack chain node (type using database package)
type Node = database.AttackChainNode

// Edge attacks the edge of the chain (using the type of database package)
type Edge = database.AttackChainEdge

// Chain complete attack chain
type Chain struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// NewBuilder creates a new attack chain builder
func NewBuilder(db *database.DB, openAIConfig *config.OpenAIConfig, logger *zap.Logger) *Builder {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}
	httpClient := &http.Client{Timeout: 5 * time.Minute, Transport: transport}

	// Prioritize using the unified Token upper limit in the configuration file (config.yaml -> openai.max_total_tokens)
	maxTokens := 0
	if openAIConfig != nil && openAIConfig.MaxTotalTokens > 0 {
		maxTokens = openAIConfig.MaxTotalTokens
	} else if openAIConfig != nil {
		// If max_total_tokens is not configured explicitly, set a reasonable default value based on the model
		model := strings.ToLower(openAIConfig.Model)
		if strings.Contains(model, "gpt-4") {
			maxTokens = 128000 // Gpt-4 usually supports 128k
		} else if strings.Contains(model, "gpt-3.5") {
			maxTokens = 16000 // Gpt-3.5-turbo usually supports 16k
		} else if strings.Contains(model, "deepseek") {
			maxTokens = 131072 // Deepseek-chat usually supports 131k
		} else {
			maxTokens = 100000 // Pocket default value
		}
	} else {
		// Use the bottom value when there is no OpenAI configuration, avoid 0
		maxTokens = 100000
	}

	return &Builder{
		db:           db,
		logger:       logger,
		openAIClient: openai.NewClient(openAIConfig, httpClient, logger),
		openAIConfig: openAIConfig,
		tokenCounter: agent.NewTikTokenCounter(),
		maxTokens:    maxTokens,
	}
}

// BuildChainFromConversation Build an attack chain from a conversation (simplified version: user input + last round of ReAct input + large model output)
func (b *Builder) BuildChainFromConversation(ctx context.Context, conversationID string) (*Chain, error) {
	b.logger.Info("Start building the attack chain (simplified version)", zap.String("conversationId", conversationID))

	// 0. First check if there is an actual tool execution record
	messages, err := b.db.GetMessages(conversationID)
	if err != nil {
		return nil, fmt.Errorf("Failed to get conversation message: %w", err)
	}

	if len(messages) == 0 {
		b.logger.Info("There is no data in the conversation", zap.String("conversationId", conversationID))
		return &Chain{Nodes: []Node{}, Edges: []Edge{}}, nil
	}

	// Check if there is an actual tool execution (by checking the mcp_execution_ids of the assistant message)
	hasToolExecutions := false
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(messages[i].Role, "assistant") {
			if len(messages[i].MCPExecutionIDs) > 0 {
				hasToolExecutions = true
				break
			}
		}
	}

	// Check if the task was canceled (by checking the last assistant message content or process_details)
	taskCancelled := false
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(messages[i].Role, "assistant") {
			content := strings.ToLower(messages[i].Content)
			if strings.Contains(content, "Cancel") || strings.Contains(content, "cancelled") {
				taskCancelled = true
			}
			break
		}
	}

	// If the task is canceled and no actual tool is executed, an empty attack chain is returned
	if taskCancelled && !hasToolExecutions {
		b.logger.Info("The task was canceled and no actual tool was executed, returning an empty attack chain.",
			zap.String("conversationId", conversationID),
			zap.Bool("taskCancelled", taskCancelled),
			zap.Bool("hasToolExecutions", hasToolExecutions))
		return &Chain{Nodes: []Node{}, Edges: []Edge{}}, nil
	}

	// If no actual tool is executed, an empty attack chain is also returned (to avoid AI fabrication)
	if !hasToolExecutions {
		b.logger.Info("There is no actual tool execution record and an empty attack chain is returned.",
			zap.String("conversationId", conversationID))
		return &Chain{Nodes: []Node{}, Edges: []Edge{}}, nil
	}

	// 1. Prioritize trying to obtain the last round of saved ReAct input and output from the database
	reactInputJSON, modelOutput, err := b.db.GetReActData(conversationID)
	if err != nil {
		b.logger.Warn("Failed to get saved ReAct data, will be built using message history", zap.Error(err))
		// Continue to use the original logic
		reactInputJSON = ""
		modelOutput = ""
	}

	// var userInput string
	var reactInputFinal string
	var dataSource string // Record data sources

	// If the saved ReAct data is successfully obtained, use it directly
	if reactInputJSON != "" && modelOutput != "" {
		// Calculate the hash value of the ReAct input for tracking
		hash := sha256.Sum256([]byte(reactInputJSON))
		reactInputHash := hex.EncodeToString(hash[:])[:16] // Use first 16 characters as short identifier

		// Number of statistical messages
		var messageCount int
		var tempMessages []interface{}
		if json.Unmarshal([]byte(reactInputJSON), &tempMessages) == nil {
			messageCount = len(tempMessages)
		}

		dataSource = "database_last_react_input"
		b.logger.Info("Build an attack chain using saved ReAct data",
			zap.String("conversationId", conversationID),
			zap.String("dataSource", dataSource),
			zap.Int("reactInputSize", len(reactInputJSON)),
			zap.Int("messageCount", messageCount),
			zap.String("reactInputHash", reactInputHash),
			zap.Int("modelOutputSize", len(modelOutput)))

		// Extract user input from saved ReAct input (JSON format)
		// userInput = b.extractUserInputFromReActInput(reactInputJSON)

		// Convert JSON formatted messages into readable format
		reactInputFinal = b.formatReActInputFromJSON(reactInputJSON)
	} else {
		// 2. If there is no saved ReAct data, build it from the conversation message
		dataSource = "messages_table"
		b.logger.Info("Building ReAct data from message history",
			zap.String("conversationId", conversationID),
			zap.String("dataSource", dataSource),
			zap.Int("messageCount", len(messages)))

		// Extract user input (last user message)
		for i := len(messages) - 1; i >= 0; i-- {
			if strings.EqualFold(messages[i].Role, "user") {
				// userInput = messages[i].Content
				break
			}
		}

		// Extract the input of the last round of ReAct (historical messages + current user input)
		reactInputFinal = b.buildReActInput(messages)

		// Extract the last output of the large model (the last assistant message)
		for i := len(messages) - 1; i >= 0; i-- {
			if strings.EqualFold(messages[i].Role, "assistant") {
				modelOutput = messages[i].Content
				break
			}
		}
	}

	// 3. Build a simplified prompt and pass it to the large model at once
	prompt := b.buildSimplePrompt(reactInputFinal, modelOutput)
	// fmt.Println(prompt)
	// 6. Call AI to generate an attack chain (one-time, no processing)
	chainJSON, err := b.callAIForChainGeneration(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("AI generation failed: %w", err)
	}

	// 7. Parse JSON and generate node/edge ID (frontend requires valid ID)
	chainData, err := b.parseChainJSON(chainJSON)
	if err != nil {
		// If the parsing fails, return an empty link and let the front end handle the error.
		b.logger.Warn("Failed to parse attack chain JSON", zap.Error(err), zap.String("raw_json", chainJSON))
		return &Chain{
			Nodes: []Node{},
			Edges: []Edge{},
		}, nil
	}

	b.logger.Info("Attack chain construction completed",
		zap.String("conversationId", conversationID),
		zap.String("dataSource", dataSource),
		zap.Int("nodes", len(chainData.Nodes)),
		zap.Int("edges", len(chainData.Edges)))

	// Save to database (for subsequent loading)
	if err := b.saveChain(conversationID, chainData.Nodes, chainData.Edges); err != nil {
		b.logger.Warn("Failed to save attack chain to database", zap.Error(err))
		// Even if the save fails, the data will be returned to the front end.
	}

	// Return directly without any processing or verification
	return chainData, nil
}

// BuildReActInput builds the input of the last round of ReAct (historical messages + current user input)
func (b *Builder) buildReActInput(messages []database.Message) string {
	var builder strings.Builder
	for _, msg := range messages {
		builder.WriteString(fmt.Sprintf("[%s]: %s\n\n", msg.Role, msg.Content))
	}
	return builder.String()
}

// ExtractUserInputFromReActInput Extracts the last user input from the saved ReAct input (messages array in JSON format)
// func (b *Builder) extractUserInputFromReActInput(reactInputJSON string) string {
// // reactInputJSON is a ChatMessage array in JSON format and needs to be parsed
// 	var messages []map[string]interface{}
// 	if err := json.Unmarshal([]byte(reactInputJSON), &messages); err != nil {
// B.logger.Warn("Failed to parse ReAct input JSON", zap.Error(err))
// 		return ""
// 	}

// // Find the last user message from back to front
// 	for i := len(messages) - 1; i >= 0; i-- {
// 		if role, ok := messages[i]["role"].(string); ok && strings.EqualFold(role, "user") {
// 			if content, ok := messages[i]["content"].(string); ok {
// 				return content
// 			}
// 		}
// 	}

// 	return ""
// }

// FormatReActInputFromJSON Converts the messages array in JSON format into a readable string format
func (b *Builder) formatReActInputFromJSON(reactInputJSON string) string {
	var messages []map[string]interface{}
	if err := json.Unmarshal([]byte(reactInputJSON), &messages); err != nil {
		b.logger.Warn("Parsing ReAct input JSON failed", zap.Error(err))
		return reactInputJSON // If parsing fails, return the original JSON
	}

	var builder strings.Builder
	for _, msg := range messages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)

		// Process assistant messages: extract tool_calls information
		if role == "assistant" {
			if toolCalls, ok := msg["tool_calls"].([]interface{}); ok && len(toolCalls) > 0 {
				// If there is text content, it is displayed first
				if content != "" {
					builder.WriteString(fmt.Sprintf("[%s]: %s\n", role, content))
				}
				// Show each tool call in detail
				builder.WriteString(fmt.Sprintf("[%s] Tool calls (%d):\n", role, len(toolCalls)))
				for i, toolCall := range toolCalls {
					if tc, ok := toolCall.(map[string]interface{}); ok {
						toolCallID, _ := tc["id"].(string)
						if funcData, ok := tc["function"].(map[string]interface{}); ok {
							toolName, _ := funcData["name"].(string)
							arguments, _ := funcData["arguments"].(string)
							builder.WriteString(fmt.Sprintf("[Tool call %d]\n", i+1))
							builder.WriteString(fmt.Sprintf("    ID: %s\n", toolCallID))
							builder.WriteString(fmt.Sprintf("Tool name: %s\n", toolName))
							builder.WriteString(fmt.Sprintf("Parameters: %s\n", arguments))
						}
					}
				}
				builder.WriteString("\n")
				continue
			}
		}

		// Handling tool messages: display tool_call_id and complete content
		if role == "tool" {
			toolCallID, _ := msg["tool_call_id"].(string)
			if toolCallID != "" {
				builder.WriteString(fmt.Sprintf("[%s] (tool_call_id: %s):\n%s\n\n", role, toolCallID, content))
			} else {
				builder.WriteString(fmt.Sprintf("[%s]: %s\n\n", role, content))
			}
			continue
		}

		// Other message types (system, user, etc.) are displayed normally
		builder.WriteString(fmt.Sprintf("[%s]: %s\n\n", role, content))
	}

	return builder.String()
}

// BuildSimplePrompt builds a simplified prompt
func (b *Builder) buildSimplePrompt(reactInput, modelOutput string) string {
Return fmt.Sprintf(`You are a professional security test analyst and attack chain construction expert. Your task is to construct a logical and educational attack chain diagram based on the conversation records and tool execution results, fully demonstrating the thinking process and execution path of the penetration test.

## Core Objectives

Building an attack chain that tells the complete attack story allows learners to:
1. Understand the complete process and thinking logic of penetration testing (every step from target identification to vulnerability discovery)
2. Learn how to take cues from failures and adjust your strategy
3. Understand the actual effects and limitations of using tools
4. Understand the cause-and-effect relationship between vulnerability discovery and exploitation

**Key Principle**: Integrity first. All meaningful tool executions and critical steps must be included, and important information should not be left out in order to control the number of nodes.

## Build process (think in this order)

### Step 1: Understand the context
Carefully analyze tool call sequences in ReAct input and large model output to identify:
- Test target (IP, domain name, URL, etc.)
- Tools and parameters for actual execution
- Key information returned by the tool (successful results, error messages, timeouts, etc.)
- AI analysis and decision-making process

### Step 2: Extract key nodes
Extract meaningful nodes from tool execution records, ensuring no critical steps are missed:
- **target node**: Create a target node for each independent test target
- **action node**: Create an action node for each meaningful tool execution (including failure to provide clues, successful information collection, vulnerability verification, etc.)
- **vulnerability node**: Create a vulnerability node for each truly confirmed vulnerability
- **Sanity Check**: Check the tool call sequence in the ReAct input to ensure that every meaningful tool execution is included in the attack chain

### Step 3: Build logical relationships (tree structure)
**IMPORTANT: A tree structure must be built, not a simple linear chain. **
Connect the nodes according to the causal relationship to form a tree diagram (because it is executed by a single agent, it does not need to be in chronological order):
- **Branch structure**: A node can have multiple subsequent nodes (for example: after a port scan finds multiple ports, multiple different tests can be performed at the same time)
- **aggregation structure**: multiple nodes can point to the same node (for example: multiple different tests have discovered the same vulnerability)
- Identify which actions are executed based on the results of previous actions
- Identify which vulnerabilities were discovered by which actions
- Identify how failed nodes provide clues to subsequent success
- **Avoid linear chains**: Do not connect all nodes in a line, build a tree structure based on actual parallel testing and branch exploration

### Step 4: Optimize and streamline
- **Sanity Check**: Make sure all meaningful tool executions are included, don't miss critical steps
- **Merge Rules**: Only merge truly similar or repeated action nodes (such as multiple similar calls of the same tool)
- **Deletion Rules**: Only delete failed nodes that are completely worthless (completely no output, pure system errors, repeated identical failures)
- **Important reminder**: It is better to keep more nodes than to miss key steps. The attack chain must fully demonstrate the penetration testing process
- Ensure the attack chain is logically coherent and able to tell a complete story

## Detailed explanation of node types

### target (target node)
- **Purpose**: Identify test targets
- **Creation Rules**: Create a target node for each independent target (different IP/domain name)
- **Multi-target processing**: Nodes of different targets are not connected to each other and form independent subgraphs.
- **metadata.target**: Accurately record target identification (IP address, domain name, URL, etc.)

### action (action node)
- **Purpose**: Record tool execution and AI analysis results
- **Tag Rules**:
* 15-25 Chinese characters, verb-object structure
* Successful node: describes the execution result (such as "Scan port found 80/443/8080", "Directory scan found /admin path")
* Failure node: describe the reason for the failure (such as "Attempt SQL injection (intercepted by WAF)", "Port scan timeout (destination unreachable)")
- **ai_analysis requirements**:
* Success node: summarizes key findings from tool execution and explains the significance of these findings
* Failure node: The reason for the failure, the clues obtained, and how these clues guide subsequent actions must be explained
* No more than 150 words, be specific and informative
- **Findings Requirements**:
* The key information points in the results returned by the extraction tool
* Each finding should be an independent, valuable piece of information
* Success node: List key findings (such as ["Port 80 is open", "Port 443 is open", "HTTP service is Apache 2.4"])
* Failed node: List failure clues (such as ["WAF interception", "Return 403", "Cloudflare detected"])
- **status tag**:
* Success node: not set or set to "success"
* Failed node that provides clues: must be set to "failed_insight"
- **risk_score**: always 0 (action node does not evaluate risk)

### vulnerability (vulnerability node)
- **Purpose**: Record real confirmed security vulnerabilities
- **Create Rules**:
* Must be a real confirmed vulnerability, not all discovered are vulnerabilities
* Clear evidence of vulnerability is required (such as SQL injection returning database error, XSS successfully executed, etc.)
- **risk_score rules**:
* critical (90-100): can cause the system to completely collapse (RCE, SQL injection leading to data leakage, etc.)
* high (80-89): can lead to leakage of sensitive information or elevation of privileges
* medium (60-79): There is a security risk but the impact is limited
* low (40-59): minor security issues
- **metadata requirements**:
* vulnerability_type: vulnerability type (SQL injection, XSS, RCE, etc.)
* description: Detailed description of the location, principle, and impact of the vulnerability
  * severity：critical/high/medium/low
* location: precise vulnerability location (URL, parameters, file path, etc.)

## Node filtering and merging rules

### Failed nodes that must be retained
Nodes must be created for the following failure cases because they provide valuable clues:
- The tool returns clear error messages (permission errors, connection refused, authentication failure, etc.)
- Timeout or connection failure (may indicate firewall, network isolation, etc.)
- WAF/firewall interception (returning 403, 406, etc., indicating the existence of a protection mechanism)
- The tool is not installed or misconfigured (but the call was executed)
- The target is unreachable (DNS resolution failure, network failure, etc.)

### Failed nodes that should be deleted
Nodes should not be created in the following situations:
- Completely outputless tool calls
- Pure system errors (not related to the target, such as local environment problems)
- Repeated same failures (only the first time of the same error is retained)

### Node merging rules
Nodes should be merged in the following situations:
- Multiple similar calls to the same tool (such as multiple nmap scans of different port ranges, merged into one "Port scan" node)
- Multiple similar detections for the same target (such as multiple directory scanning tools, merged into one "Directory scan" node)

### Node number control
- **Completeness First**: All meaningful tool executions and critical steps must be included, do not delete important nodes to control the number
- **Recommended range**: A single target usually has 8-15 nodes, but if there are more actual execution steps, it can be increased appropriately (up to 20 nodes)
- **PRIORITY RETENTION**: Critical success steps, failures in providing clues, discovered vulnerabilities, important information gathering steps
- **can be merged**: multiple similar calls of the same tool (such as multiple nmap scans of different port ranges, merged into one "Port scan" node)
- **can be deleted**: tool calls with no output at all, pure system errors, repeated identical failures (only the first time of the same error is retained)
- **Important Principle**: It is better to have more nodes than to miss key steps. The attack chain must be able to fully demonstrate the complete process of penetration testing

## Edge type and weight

### Edge type
- **leads_to**: means "Lead to" or "Boot to", used for action→action, target→action
* For example: port scan → directory scan (because port 80 was found, directory scan was performed)
- **discovers**: means "Discover", **specifically used for action→vulnerability**
* For example: SQL injection testing → SQL injection vulnerability
* **Important**: All action→vulnerability edges must use the discovers type. Even if multiple actions point to the same vulnerability, discovers should be used uniformly.
- **enables**: means "Enable" or "Contribute to", **only used for vulnerability→vulnerability, action→action (when the subsequent action depends on the previous result)**
* For example: Information leakage vulnerability → Privilege escalation vulnerability (information obtained through information leakage facilitates privilege escalation)
* **Important**: enables cannot be used for action→vulnerability, action→vulnerability must use discovers

### Edge weight
- **Weight 1-2**: Weak association (such as initial detection and further detection)
- **Weight 3-4**: Medium correlation (e.g. discovery port to service identification)
- **Weight 5-7**: Strong correlation (such as vulnerability discovery, key information leakage)
- **Weight 8-10**: extremely strong correlation (such as successful vulnerability exploitation, privilege escalation)

### DAG structure requirements (directed acyclic graph)
**Key: You must ensure that the generated DAG (directed acyclic graph) is a true DAG without any cycles. **

- **Node numbering rule**: Node id increases from "node_1" (node_1, node_2, node_3...)
- **边的方向规则**：所有边的source节点id必须严格小于target节点id（source < target），这是确保无环的关键
* For example: node_1 → node_2 ✓ (correct)
* For example: node_2 → node_1 ✗ (error, a loop will be formed)
* For example: node_3 → node_5 ✓ (correct)
- **Loop-free verification**: Before outputting JSON, all edges must be checked to ensure that no edge has source >= target
- **No Orphaned Nodes**: Make sure every node is connected by at least one edge (except possibly the root node)
- **DAG structural features**:
* A node can have multiple subsequent nodes (branch), for example: node_2 (port scan) can be connected to multiple nodes such as node_3, node_4, node_5 at the same time
* Multiple nodes can be aggregated into one node (aggregation), for example: node_3, node_4, node_5 all point to node_6 (vulnerable node)
* Avoid connecting all nodes in a line and build a DAG structure based on actual parallel testing and branch exploration.
- **Topological sorting verification**: If you sort by node ID from small to large, all edges should point from left to right (from top to bottom), thus ensuring no loops

## Requirements for logical coherence of attack chain

The attack chain constructed should be able to answer the following questions:
1. **Starting point**: Where does testing start? (target node)
2. **Exploration Process**: How to collect information step by step? (action node sequence)
3. **Failure and Adjustment**: How to adjust your strategy when you encounter obstacles? (failed_insight node)
4. **Key Findings**: What important information was discovered? (findings of action)
5. **Vulnerability Confirmation**: How to confirm the existence of the vulnerability? (action→vulnerability)
6. **Attack Path**: What is the complete attack path? (path from target to vulnerability)

## The last round of ReAct input

%s

## Large model output

%s

## Output format

Output strictly in the following JSON format, do not add any other text:

**Important: The example shows a tree structure. Note that node_2 (port scan) is connected to multiple subsequent nodes (node_3, node_4) at the same time, forming a branch structure. **

{
   "nodes": [
     {
       "id": "node_1",
       "type": "target",
       "label": "Test target: example.com",
       "risk_score": 40,
       "metadata": {
         "target": "example.com"
       }
     },
     {
       "id": "node_2",
       "type": "action",
       "label": "Scan port found 80/443/8080",
       "risk_score": 0,
       "metadata": {
         "tool_name": "nmap",
         "tool_intent": "Port scan",
         "ai_analysis": "Use nmap to perform port scanning on the target and find that ports 80, 443, and 8080 are open. Port 80 runs the HTTP service, port 443 runs the HTTPS service, and port 8080 may be the management backend. These open ports provide entrances for subsequent web application testing.",
         "findings": ["Port 80 is open", "Port 443 is open", "Port 8080 is open", "HTTP service is Apache 2.4"]
       }
     },
     {
       "id": "node_3",
       "type": "action",
       "label": "Directory scan found /admin background",
       "risk_score": 0,
       "metadata": {
         "tool_name": "dirsearch",
         "tool_intent": "Directory scan",
         "ai_analysis": "Use dirsearch to scan the target directory and find that the /admin directory exists and is accessible. This directory may be the management backend and is an important test target.",
         "findings": ["/admin directory exists", "Return 200 status code", "Suspected management background"]
       }
     },
     {
       "id": "node_4",
       "type": "action",
       "label": "Identify the web service as Apache 2.4",
       "risk_score": 0,
       "metadata": {
         "tool_name": "whatweb",
         "tool_intent": "Web service identification",
         "ai_analysis": "It was identified that the target was running an Apache 2.4 server, which provided important information for subsequent vulnerability testing.",
         "findings": ["Apache 2.4", "PHP version information"]
       }
     },
     {
       "id": "node_5",
       "type": "action",
       "label": "Attempt SQL injection (intercepted by WAF)",
       "risk_score": 0,
       "metadata": {
         "tool_name": "sqlmap",
         "tool_intent": "SQL injection detection",
         "ai_analysis": "When performing SQL injection testing on /login.php, it was intercepted by WAF and returned a 403 error. The error message says Cloudflare protection is detected. This indicates that the target has a WAF deployed and the testing strategy needs to be adjusted.",
         "findings": ["WAF interception", "Return 403", "Cloudflare detected", "Target Deployment WAF"],
         "status": "failed_insight"
       }
     },
     {
       "id": "node_6",
       "type": "vulnerability",
       "label": "SQL injection vulnerability",
       "risk_score": 85,
       "metadata": {
         "vulnerability_type": "SQL injection",
         "description": "A SQL injection vulnerability was found in the username parameter of /admin/login.php. You can bypass login verification by injecting the payload and directly obtain administrator privileges. The vulnerability returns a database error message, confirming the existence of an injection point.",
         "severity": "high",
         "location": "/admin/login.php?username="
       }
     }
   ],
   "edges": [
     {
       "source": "node_1",
       "target": "node_2",
       "type": "leads_to",
       "weight": 3
     },
     {
       "source": "node_2",
       "target": "node_3",
       "type": "leads_to",
       "weight": 4
     },
     {
       "source": "node_2",
       "target": "node_4",
       "type": "leads_to",
       "weight": 3
     },
     {
       "source": "node_3",
       "target": "node_5",
       "type": "leads_to",
       "weight": 4
     },
     {
       "source": "node_5",
       "target": "node_6",
       "type": "discovers",
       "weight": 7
     }
   ]
}

## Important reminder

1. **No fabrication**: Only use the tools actually executed and the results actually returned in the ReAct input. If there is no actual data, empty nodes and edges arrays are returned.
2. **DAG结构必须**：必须构建真正的DAG（有向无环图），不能有任何循环。所有边的source节点id必须严格小于target节点id（source < target）。
3. **Topological order**: Nodes should be numbered in logical order. The target node is usually node_1. Subsequent action nodes are increased in order of execution, and the vulnerability node is at the end.
4. **Integrity first**: All meaningful tool executions and key steps must be included, and important nodes should not be deleted in order to control the number of nodes. The attack chain must be able to fully demonstrate the complete process from target identification to vulnerability discovery.
5. **Logical Coherence**: Ensure that the attack chain tells a complete, coherent penetration test story, including all key steps and decision points.
6. **Educational value**: Give priority to retaining nodes with educational significance to help learners understand the thinking and complete process of penetration testing.
7. **Accuracy**: All node information must be based on actual data, no speculation or assumptions.
8. **Integrity check**: Ensure that each node has the necessary metadata fields, each edge has the correct source and target, and there are no isolated nodes and no loops.
9. **Don’t oversimplify**: If there are many actual execution steps, you can increase the number of nodes appropriately (up to 20) to ensure that key steps are not missed.
10. **输出前验证**：在输出JSON前，必须验证所有边都满足source < target的条件，确保DAG结构正确。

Now start analyzing and building the attack chain: `, reactInput, modelOutput)
}

// SaveChain saves the attack chain to the database
func (b *Builder) saveChain(conversationID string, nodes []Node, edges []Edge) error {
	// Delete old attack chain data first
	if err := b.db.DeleteAttackChain(conversationID); err != nil {
		b.logger.Warn("Failed to delete old attack chain", zap.Error(err))
	}

	for _, node := range nodes {
		metadataJSON, _ := json.Marshal(node.Metadata)
		if err := b.db.SaveAttackChainNode(conversationID, node.ID, node.Type, node.Label, "", string(metadataJSON), node.RiskScore); err != nil {
			b.logger.Warn("Failed to save attack chain node", zap.String("nodeId", node.ID), zap.Error(err))
		}
	}

	// Save edges
	for _, edge := range edges {
		if err := b.db.SaveAttackChainEdge(conversationID, edge.ID, edge.Source, edge.Target, edge.Type, edge.Weight); err != nil {
			b.logger.Warn("Failed to save attack chain edge", zap.String("edgeId", edge.ID), zap.Error(err))
		}
	}

	return nil
}

// LoadChainFromDatabase loads the attack chain from the database
func (b *Builder) LoadChainFromDatabase(conversationID string) (*Chain, error) {
	nodes, err := b.db.LoadAttackChainNodes(conversationID)
	if err != nil {
		return nil, fmt.Errorf("Failed to load attack chain node: %w", err)
	}

	edges, err := b.db.LoadAttackChainEdges(conversationID)
	if err != nil {
		return nil, fmt.Errorf("Failed to load attack chain edges: %w", err)
	}

	return &Chain{
		Nodes: nodes,
		Edges: edges,
	}, nil
}

// CallAIForChainGeneration calls AI to generate attack chain
func (b *Builder) callAIForChainGeneration(ctx context.Context, prompt string) (string, error) {
	requestBody := map[string]interface{}{
		"model": b.openAIConfig.Model,
		"messages": []map[string]interface{}{
			{
				"role":    "system",
				"content": "You are a professional security testing analyst who is good at building attack chain diagrams. Please return attack chain data strictly in JSON format.",
			},
			{
				"role":    "user",
				"content": prompt,
			},
		},
		"temperature": 0.3,
		"max_tokens":  8000,
	}

	var apiResponse struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if b.openAIClient == nil {
		return "", fmt.Errorf("OpenAI client not initialized")
	}
	if err := b.openAIClient.ChatCompletion(ctx, requestBody, &apiResponse); err != nil {
		var apiErr *openai.APIError
		if errors.As(err, &apiErr) {
			bodyStr := strings.ToLower(apiErr.Body)
			if strings.Contains(bodyStr, "context") || strings.Contains(bodyStr, "length") || strings.Contains(bodyStr, "too long") {
				return "", fmt.Errorf("context length exceeded")
			}
		} else if strings.Contains(strings.ToLower(err.Error()), "context") || strings.Contains(strings.ToLower(err.Error()), "length") {
			return "", fmt.Errorf("context length exceeded")
		}
		return "", fmt.Errorf("Request failed: %w", err)
	}

	if len(apiResponse.Choices) == 0 {
		return "", fmt.Errorf("API did not return a valid response")
	}

	content := strings.TrimSpace(apiResponse.Choices[0].Message.Content)
	// Try to extract JSON (may contain markdown code blocks)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	return content, nil
}

// ChainJSON attack chain JSON structure
type ChainJSON struct {
	Nodes []struct {
		ID        string                 `json:"id"`
		Type      string                 `json:"type"`
		Label     string                 `json:"label"`
		RiskScore int                    `json:"risk_score"`
		Metadata  map[string]interface{} `json:"metadata"`
	} `json:"nodes"`
	Edges []struct {
		Source string `json:"source"`
		Target string `json:"target"`
		Type   string `json:"type"`
		Weight int    `json:"weight"`
	} `json:"edges"`
}

// ParseChainJSON parses attack chain JSON
func (b *Builder) parseChainJSON(chainJSON string) (*Chain, error) {
	var chainData ChainJSON
	if err := json.Unmarshal([]byte(chainJSON), &chainData); err != nil {
		return nil, fmt.Errorf("Failed to parse JSON: %w", err)
	}

	// Create node ID mapping (ID returned by AI -> new UUID)
	nodeIDMap := make(map[string]string)

	// Convert to Chain structure
	nodes := make([]Node, 0, len(chainData.Nodes))
	for _, n := range chainData.Nodes {
		// Generate new UUID node ID
		newNodeID := fmt.Sprintf("node_%s", uuid.New().String())
		nodeIDMap[n.ID] = newNodeID

		node := Node{
			ID:        newNodeID,
			Type:      n.Type,
			Label:     n.Label,
			RiskScore: n.RiskScore,
			Metadata:  n.Metadata,
		}
		if node.Metadata == nil {
			node.Metadata = make(map[string]interface{})
		}
		nodes = append(nodes, node)
	}

	// Convert edges
	edges := make([]Edge, 0, len(chainData.Edges))
	for _, e := range chainData.Edges {
		sourceID, ok := nodeIDMap[e.Source]
		if !ok {
			continue
		}
		targetID, ok := nodeIDMap[e.Target]
		if !ok {
			continue
		}

		// Generate edge ID (required by front-end)
		edgeID := fmt.Sprintf("edge_%s", uuid.New().String())

		edges = append(edges, Edge{
			ID:     edgeID,
			Source: sourceID,
			Target: targetID,
			Type:   e.Type,
			Weight: e.Weight,
		})
	}

	return &Chain{
		Nodes: nodes,
		Edges: edges,
	}, nil
}

// All methods below are no longer used and have been removed to simplify the code
