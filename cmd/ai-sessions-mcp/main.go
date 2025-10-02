// Package main implements an MCP (Model Context Protocol) server that provides
// access to AI assistant CLI sessions from various tools.
//
// This server allows AI assistants to search, list, and read previous coding sessions
// from Claude Code, Gemini CLI, OpenAI Codex, and Cursor CLI.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yoavf/ai-sessions-mcp/adapters"
)

func main() {
	// Create the MCP server with metadata
	opts := &mcp.ServerOptions{
		Instructions: "This server provides access to AI assistant CLI sessions from Claude Code, Gemini CLI, OpenAI Codex, and Cursor CLI. Use the tools to search, list, and read previous coding sessions.",
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "ai-sessions-mcp",
		Version: "1.0.0",
	}, opts)

	// Initialize adapters
	adaptersMap := make(map[string]adapters.SessionAdapter)
	if claudeAdapter, err := adapters.NewClaudeAdapter(); err == nil {
		adaptersMap["claude"] = claudeAdapter
	}
	if geminiAdapter, err := adapters.NewGeminiAdapter(); err == nil {
		adaptersMap["gemini"] = geminiAdapter
	}
	if codexAdapter, err := adapters.NewCodexAdapter(); err == nil {
		adaptersMap["codex"] = codexAdapter
	}
	if opencodeAdapter, err := adapters.NewOpenCodeAdapter(); err == nil {
		adaptersMap["opencode"] = opencodeAdapter
	}

	// Add tools with strongly-typed argument structures
	addListAvailableToolsTool(server, adaptersMap)
	addListSessionsTool(server, adaptersMap)
	addSearchSessionsTool(server, adaptersMap)
	addGetSessionTool(server, adaptersMap)

	// Run the server over stdio
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

// Tool 1: list_available_tools
type listAvailableToolsArgs struct{}

func addListAvailableToolsTool(server *mcp.Server, adaptersMap map[string]adapters.SessionAdapter) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_available_tools",
		Description: "List which AI CLI tools have sessions available (e.g., claude, gemini, codex, cursor)",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args listAvailableToolsArgs) (*mcp.CallToolResult, any, error) {
		available := make([]map[string]interface{}, 0, len(adaptersMap))
		for name, adapter := range adaptersMap {
			available = append(available, map[string]interface{}{
				"name":      name,
				"full_name": adapter.Name(),
			})
		}

		result := map[string]interface{}{
			"available_tools": available,
			"count":           len(available),
		}

		resultJSON, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal result: %w", err)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(resultJSON)},
			},
		}, nil, nil
	})
}

// Tool 2: list_sessions
type listSessionsArgs struct {
	Tool        string `json:"tool,omitempty" jsonschema:"Filter by tool name (claude, gemini, codex, cursor). Leave empty for all tools."`
	ProjectPath string `json:"project_path,omitempty" jsonschema:"Filter by project directory path. Leave empty for current directory."`
	Limit       int    `json:"limit,omitempty" jsonschema:"Maximum number of sessions to return"`
}

func addListSessionsTool(server *mcp.Server, adaptersMap map[string]adapters.SessionAdapter) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_sessions",
		Description: "List recent AI assistant sessions with optional filtering by tool and project",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args listSessionsArgs) (*mcp.CallToolResult, any, error) {
		if args.Limit == 0 {
			args.Limit = 10
		}

		var allSessions []adapters.Session

		// Determine which adapters to query
		adaptersToQuery := make(map[string]adapters.SessionAdapter)
		if args.Tool != "" {
			if adapter, ok := adaptersMap[args.Tool]; ok {
				adaptersToQuery[args.Tool] = adapter
			} else {
				return nil, nil, fmt.Errorf("unknown tool: %s", args.Tool)
			}
		} else {
			adaptersToQuery = adaptersMap
		}

		// Query each adapter
		for _, adapter := range adaptersToQuery {
			sessions, err := adapter.ListSessions(args.ProjectPath, args.Limit)
			if err != nil {
				// Log error but continue with other adapters
				log.Printf("Error listing sessions for %s: %v", adapter.Name(), err)
				continue
			}
			allSessions = append(allSessions, sessions...)
		}

		// Sort by timestamp (newest first)
		sort.Slice(allSessions, func(i, j int) bool {
			return allSessions[i].Timestamp.After(allSessions[j].Timestamp)
		})

		// Apply limit
		if args.Limit > 0 && len(allSessions) > args.Limit {
			allSessions = allSessions[:args.Limit]
		}

		result := map[string]interface{}{
			"sessions": allSessions,
			"count":    len(allSessions),
		}

		resultJSON, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal result: %w", err)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(resultJSON)},
			},
		}, nil, nil
	})
}

// Tool 3: search_sessions
type searchSessionsArgs struct {
	Query       string `json:"query" jsonschema:"Search query to find in session content"`
	Tool        string `json:"tool,omitempty" jsonschema:"Filter by tool name (claude, gemini, codex, cursor). Leave empty for all tools."`
	ProjectPath string `json:"project_path,omitempty" jsonschema:"Filter by project directory path. Leave empty for current directory."`
	Limit       int    `json:"limit,omitempty" jsonschema:"Maximum number of matching sessions to return"`
}

func addSearchSessionsTool(server *mcp.Server, adaptersMap map[string]adapters.SessionAdapter) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_sessions",
		Description: "Search through session content for a specific query",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args searchSessionsArgs) (*mcp.CallToolResult, any, error) {
		if args.Query == "" {
			return nil, nil, fmt.Errorf("query is required")
		}

		if args.Limit == 0 {
			args.Limit = 10
		}

		var allMatches []adapters.Session

		// Determine which adapters to query
		adaptersToQuery := make(map[string]adapters.SessionAdapter)
		if args.Tool != "" {
			if adapter, ok := adaptersMap[args.Tool]; ok {
				adaptersToQuery[args.Tool] = adapter
			} else {
				return nil, nil, fmt.Errorf("unknown tool: %s", args.Tool)
			}
		} else {
			adaptersToQuery = adaptersMap
		}

		// Query each adapter
		for _, adapter := range adaptersToQuery {
			matches, err := adapter.SearchSessions(args.ProjectPath, args.Query, args.Limit)
			if err != nil {
				log.Printf("Error searching sessions for %s: %v", adapter.Name(), err)
				continue
			}
			allMatches = append(allMatches, matches...)
		}

		// Apply limit
		if args.Limit > 0 && len(allMatches) > args.Limit {
			allMatches = allMatches[:args.Limit]
		}

		result := map[string]interface{}{
			"query":   args.Query,
			"matches": allMatches,
			"count":   len(allMatches),
		}

		resultJSON, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal result: %w", err)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(resultJSON)},
			},
		}, nil, nil
	})
}

// Tool 4: get_session
type getSessionArgs struct {
	SessionID string `json:"session_id" jsonschema:"The session ID to retrieve"`
	Tool      string `json:"tool" jsonschema:"The tool that created this session (claude, gemini, codex, cursor)"`
	Page      int    `json:"page,omitempty" jsonschema:"Page number for pagination (0-indexed)"`
	PageSize  int    `json:"page_size,omitempty" jsonschema:"Number of messages per page"`
}

func addGetSessionTool(server *mcp.Server, adaptersMap map[string]adapters.SessionAdapter) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_session",
		Description: "Get the full content of a session with pagination support",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args getSessionArgs) (*mcp.CallToolResult, any, error) {
		if args.SessionID == "" {
			return nil, nil, fmt.Errorf("session_id is required")
		}
		if args.Tool == "" {
			return nil, nil, fmt.Errorf("tool is required")
		}

		adapter, ok := adaptersMap[args.Tool]
		if !ok {
			return nil, nil, fmt.Errorf("unknown tool: %s", args.Tool)
		}

		if args.PageSize == 0 {
			args.PageSize = 50
		}

		messages, err := adapter.GetSession(args.SessionID, args.Page, args.PageSize)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get session: %w", err)
		}

		result := map[string]interface{}{
			"session_id": args.SessionID,
			"tool":       args.Tool,
			"page":       args.Page,
			"page_size":  args.PageSize,
			"messages":   messages,
			"count":      len(messages),
		}

		resultJSON, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal result: %w", err)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(resultJSON)},
			},
		}, nil, nil
	})
}
