package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"cyberstrike-ai/internal/mcp"
	"cyberstrike-ai/internal/mcp/builtin"

	"go.uber.org/zap"
)

// RegisterKnowledgeTool registers knowledge retrieval tools to the MCP server
func RegisterKnowledgeTool(
	mcpServer *mcp.Server,
	retriever *Retriever,
	manager *Manager,
	logger *zap.Logger,
) {
	// Register for the first tool: Get a list of all available risk types
	listRiskTypesTool := mcp.Tool{
		Name:             builtin.ToolListKnowledgeRiskTypes,
		Description:      "Get a list of all risk types (risk_type) available in the knowledge base. Before searching the knowledge base, you can call this tool to obtain the available risk types, and then perform a precise search using the correct risk type, which can significantly reduce retrieval time and improve retrieval accuracy.",
		ShortDescription: "Get a list of all risk types available in the knowledge base",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
			"required":   []string{},
		},
	}

	listRiskTypesHandler := func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		categories, err := manager.GetCategories()
		if err != nil {
			logger.Error("Failed to get list of risk types", zap.Error(err))
			return &mcp.ToolResult{
				Content: []mcp.Content{
					{
						Type: "text",
						Text: fmt.Sprintf("Failed to get list of risk types: %v", err),
					},
				},
				IsError: true,
			}, nil
		}

		if len(categories) == 0 {
			return &mcp.ToolResult{
				Content: []mcp.Content{
					{
						Type: "text",
						Text: "There are currently no risk types in the knowledge base.",
					},
				},
			}, nil
		}

		var resultText strings.Builder
		resultText.WriteString(fmt.Sprintf("There are %d risk types in the knowledge base:\n\n", len(categories)))
		for i, category := range categories {
			resultText.WriteString(fmt.Sprintf("%d. %s\n", i+1, category))
		}
		resultText.WriteString("\nTips: Calling" + builtin.ToolSearchKnowledgeBase + "Tool, you can use one of the above risk types as the risk_type parameter to narrow the search scope and improve retrieval efficiency.")

		return &mcp.ToolResult{
			Content: []mcp.Content{
				{
					Type: "text",
					Text: resultText.String(),
				},
			},
		}, nil
	}

	mcpServer.RegisterTool(listRiskTypesTool, listRiskTypesHandler)
	logger.Info("Risk Type List Tool Registered", zap.String("toolName", listRiskTypesTool.Name))

	// Register a second tool: Search the knowledge base (maintain original functionality)
	searchTool := mcp.Tool{
		Name:             builtin.ToolSearchKnowledgeBase,
		Description:      "Search the knowledge base for relevant security knowledge. When you need to know security knowledge such as specific vulnerability types, attack techniques, detection methods, etc., you can use this tool to search. The tool uses vector retrieval and hybrid search technology to automatically find the most relevant knowledge fragments based on the semantic similarity and keyword matching of the query content. Suggestion: You can call before searching" + builtin.ToolListKnowledgeRiskTypes + "The tool takes the available risk types and then performs an exact search using the correct risk_type parameter, which can significantly reduce retrieval time.",
		ShortDescription: "Search security knowledge in the knowledge base (supports vector retrieval and hybrid search)",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search query content and describe the security knowledge topic you want to know about",
				},
				"risk_type": map[string]interface{}{
					"type":        "string",
					"description": "Optional: Specify the risk type (such as SQL injection, XSS, file upload, etc.). It is recommended to call first" + builtin.ToolListKnowledgeRiskTypes + "The tool takes a list of available risk types and then performs a precise search using the correct risk type, which can significantly reduce retrieval time. If not specified all types are searched.",
				},
			},
			"required": []string{"query"},
		},
	}

	searchHandler := func(ctx context.Context, args map[string]interface{}) (*mcp.ToolResult, error) {
		query, ok := args["query"].(string)
		if !ok || query == "" {
			return &mcp.ToolResult{
				Content: []mcp.Content{
					{
						Type: "text",
						Text: "Error: query parameter cannot be empty",
					},
				},
				IsError: true,
			}, nil
		}

		riskType := ""
		if rt, ok := args["risk_type"].(string); ok && rt != "" {
			riskType = rt
		}

		logger.Info("Perform a knowledge base search",
			zap.String("query", query),
			zap.String("riskType", riskType),
		)

		// Perform search
		searchReq := &SearchRequest{
			Query:    query,
			RiskType: riskType,
			TopK:     5,
		}

		results, err := retriever.Search(ctx, searchReq)
		if err != nil {
			logger.Error("Knowledge base search failed", zap.Error(err))
			return &mcp.ToolResult{
				Content: []mcp.Content{
					{
						Type: "text",
						Text: fmt.Sprintf("Retrieval failed: %v", err),
					},
				},
				IsError: true,
			}, nil
		}

		if len(results) == 0 {
			return &mcp.ToolResult{
				Content: []mcp.Content{
					{
						Type: "text",
						Text: fmt.Sprintf("No knowledge related to query '%s' found. Suggestions:\n1. Try using different keywords\n2. Check whether the risk type is correct\n3. Confirm whether the knowledge base contains relevant content", query),
					},
				},
			}, nil
		}

		// Format results
		var resultText strings.Builder

		// Sort by mixed score first to ensure that the document order is by mixed score (the core of mixed retrieval)
		sort.Slice(results, func(i, j int) bool {
			return results[i].Score > results[j].Score
		})

		// Group results by document to better demonstrate context
		// Use ordered slices to keep documents in order (by highest mix score)
		type itemGroup struct {
			itemID   string
			results  []*RetrievalResult
			maxScore float64 // The highest mixed score for this document
		}
		itemGroups := make([]*itemGroup, 0)
		itemMap := make(map[string]*itemGroup)

		for _, result := range results {
			itemID := result.Item.ID
			group, exists := itemMap[itemID]
			if !exists {
				group = &itemGroup{
					itemID:   itemID,
					results:  make([]*RetrievalResult, 0),
					maxScore: result.Score,
				}
				itemMap[itemID] = group
				itemGroups = append(itemGroups, group)
			}
			group.results = append(group.results, result)
			if result.Score > group.maxScore {
				group.maxScore = result.Score
			}
		}

		// Sort document groups by highest mix score
		sort.Slice(itemGroups, func(i, j int) bool {
			return itemGroups[i].maxScore > itemGroups[j].maxScore
		})

		// Collect retrieved knowledge item IDs (for logs)
		retrievedItemIDs := make([]string, 0, len(itemGroups))

		resultText.WriteString(fmt.Sprintf("Found %d related knowledge (including context expansion):\n\n", len(results)))

		resultIndex := 1
		for _, group := range itemGroups {
			itemResults := group.results
			// Find the one with the highest mixture score as the main result (use mixture score, not similarity)
			mainResult := itemResults[0]
			maxScore := mainResult.Score
			for _, result := range itemResults {
				if result.Score > maxScore {
					maxScore = result.Score
					mainResult = result
				}
			}

			// Sort by chunk_index to ensure the logical order of reading (the original order of the document)
			sort.Slice(itemResults, func(i, j int) bool {
				return itemResults[i].Chunk.ChunkIndex < itemResults[j].Chunk.ChunkIndex
			})

			// Show main results (highest blend score, showing both similarity and blend scores)
			resultText.WriteString(fmt.Sprintf("--- Result %d (similarity: %.2f%%, mixture score: %.2f%%) ---\n",
				resultIndex, mainResult.Similarity*100, mainResult.Score*100))
			resultText.WriteString(fmt.Sprintf("Source: [%s] %s (ID: %s)\n", mainResult.Item.Category, mainResult.Item.Title, mainResult.Item.ID))

			// Display all chunks (including main results and extended chunks) in logical order
			if len(itemResults) == 1 {
				// There is only one chunk, displayed directly
				resultText.WriteString(fmt.Sprintf("Content snippet:\n%s\n", mainResult.Chunk.ChunkText))
			} else {
				// Multiple chunks, displayed in logical order
				resultText.WriteString("Content fragments (in document order):\n")
				for i, result := range itemResults {
					// Mark main results
					marker := ""
					if result.Chunk.ID == mainResult.Chunk.ID {
						marker = "[Main match]"
					}
					resultText.WriteString(fmt.Sprintf("[Fragment %d%s]\n%s\n", i+1, marker, result.Chunk.ChunkText))
				}
			}
			resultText.WriteString("\n")

			if !contains(retrievedItemIDs, group.itemID) {
				retrievedItemIDs = append(retrievedItemIDs, group.itemID)
			}
			resultIndex++
		}

		// Add metadata (JSON format, used to extract knowledge item ID) at the end of the result
		// Use special tags to avoid affecting AI reading results
		if len(retrievedItemIDs) > 0 {
			metadataJSON, _ := json.Marshal(map[string]interface{}{
				"_metadata": map[string]interface{}{
					"retrievedItemIDs": retrievedItemIDs,
				},
			})
			resultText.WriteString(fmt.Sprintf("\n<!-- METADATA: %s -->", string(metadataJSON)))
		}

		// Record retrieval log (asynchronous, non-blocking)
		// Note: There is no conversationID and messageID here, they need to be recorded at the Agent level.
		// The actual logging should be done in the Agent's progressCallback

		return &mcp.ToolResult{
			Content: []mcp.Content{
				{
					Type: "text",
					Text: resultText.String(),
				},
			},
		}, nil
	}

	mcpServer.RegisterTool(searchTool, searchHandler)
	logger.Info("Knowledge retrieval tool has been registered", zap.String("toolName", searchTool.Name))
}

// Contains checks whether the slice contains an element
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// GetRetrievalMetadata Extracts retrieval metadata from tool calls (used for logging)
func GetRetrievalMetadata(args map[string]interface{}) (query string, riskType string) {
	if q, ok := args["query"].(string); ok {
		query = q
	}
	if rt, ok := args["risk_type"].(string); ok {
		riskType = rt
	}
	return
}

// FormatRetrievalResults formats the retrieval results as strings (for logging)
func FormatRetrievalResults(results []*RetrievalResult) string {
	if len(results) == 0 {
		return "No relevant results found"
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Retrieved %d results:\n", len(results)))

	itemIDs := make(map[string]bool)
	for i, result := range results {
		builder.WriteString(fmt.Sprintf("%d. [%s] %s (similarity: %.2f%%)\n",
			i+1, result.Item.Category, result.Item.Title, result.Similarity*100))
		itemIDs[result.Item.ID] = true
	}

	// Returns a list of knowledge item IDs (JSON format)
	ids := make([]string, 0, len(itemIDs))
	for id := range itemIDs {
		ids = append(ids, id)
	}
	idsJSON, _ := json.Marshal(ids)
	builder.WriteString(fmt.Sprintf("\nRetrieved knowledge item ID: %s", string(idsJSON)))

	return builder.String()
}
