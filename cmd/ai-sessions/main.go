// Package main implements an MCP (Model Context Protocol) server that provides
// access to AI assistant CLI sessions from various tools.
//
// This server allows AI assistants to search, list, and read previous coding sessions
// from Claude Code, Gemini CLI, OpenAI Codex, and opencode.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yoavf/ai-sessions-mcp/adapters"
	"github.com/yoavf/ai-sessions-mcp/search"
)

func main() {
	// Check if running in CLI mode (has command arguments)
	if len(os.Args) > 1 {
		handleCLI()
		return
	}

	// Otherwise, run as MCP server
	// Create the MCP server with metadata
	opts := &mcp.ServerOptions{
		Instructions: "This server provides access to AI assistant CLI sessions from Claude Code, Gemini CLI, OpenAI Codex, and opencode. Use the tools to search, list, and read previous coding sessions.",
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "ai-sessions",
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
	if opencodeAdapter, err := adapters.NewOpencodeAdapter(); err == nil {
		adaptersMap["opencode"] = opencodeAdapter
	}

	// Initialize search cache
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Failed to get home directory: %v", err)
	}
	cachePath := filepath.Join(homeDir, ".cache", "ai-sessions", "search.db")
	searchCache, err := search.NewCache(cachePath)
	if err != nil {
		log.Fatalf("Failed to initialize search cache: %v", err)
	}
	defer searchCache.Close()

	// Add tools with strongly-typed argument structures
	addListAvailableSourcesTool(server, adaptersMap)
	addListSessionsTool(server, adaptersMap)
	addSearchSessionsTool(server, adaptersMap, searchCache)
	addGetSessionTool(server, adaptersMap)

	// Run the server over stdio
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

// Tool 1: list_available_sources
type listAvailableSourcesArgs struct{}

func addListAvailableSourcesTool(server *mcp.Server, adaptersMap map[string]adapters.SessionAdapter) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_available_sources",
		Description: "List which AI CLI sources have sessions available (e.g., claude, gemini, codex, opencode)",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args listAvailableSourcesArgs) (*mcp.CallToolResult, any, error) {
		available := make([]map[string]interface{}, 0, len(adaptersMap))
		for name, adapter := range adaptersMap {
			available = append(available, map[string]interface{}{
				"source":    name,
				"full_name": adapter.Name(),
			})
		}

		result := map[string]interface{}{
			"available_sources": available,
			"count":             len(available),
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
	Source      string `json:"source,omitempty" jsonschema:"Filter by source name (claude, gemini, codex, opencode). Leave empty for all sources."`
	ProjectPath string `json:"project_path,omitempty" jsonschema:"Filter by project directory path. Leave empty for current directory."`
	Limit       int    `json:"limit,omitempty" jsonschema:"Maximum number of sessions to return"`
}

func addListSessionsTool(server *mcp.Server, adaptersMap map[string]adapters.SessionAdapter) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_sessions",
		Description: "List recent AI assistant sessions with optional filtering by source and project",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args listSessionsArgs) (*mcp.CallToolResult, any, error) {
		if args.Limit == 0 {
			args.Limit = 10
		}

		var allSessions []adapters.Session

		// Determine which adapters to query
		adaptersToQuery := make(map[string]adapters.SessionAdapter)
		if args.Source != "" {
			if adapter, ok := adaptersMap[args.Source]; ok {
				adaptersToQuery[args.Source] = adapter
			} else {
				return nil, nil, fmt.Errorf("unknown source: %s", args.Source)
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
	Source      string `json:"source,omitempty" jsonschema:"Filter by source name (claude, gemini, codex, opencode). Leave empty for all sources."`
	ProjectPath string `json:"project_path,omitempty" jsonschema:"Filter by project directory path. Leave empty for current directory."`
	Limit       int    `json:"limit,omitempty" jsonschema:"Maximum number of matching sessions to return"`
}

func addSearchSessionsTool(server *mcp.Server, adaptersMap map[string]adapters.SessionAdapter, searchCache *search.Cache) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_sessions",
		Description: "Search through session content using BM25 ranking for relevance",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args searchSessionsArgs) (*mcp.CallToolResult, any, error) {
		if args.Query == "" {
			return nil, nil, fmt.Errorf("query is required")
		}

		if args.Limit == 0 {
			args.Limit = 10
		}

		// Lazy indexing: index sessions that need it
		if err := indexSessions(adaptersMap, searchCache, args.Source, args.ProjectPath); err != nil {
			log.Printf("Warning: indexing error: %v", err)
			// Continue with search anyway - we may have some indexed data
		}

		// Perform BM25 search (snippets are extracted from cached content)
		results, err := searchCache.Search(args.Query, args.Source, args.ProjectPath, args.Limit)
		if err != nil {
			return nil, nil, fmt.Errorf("search failed: %w", err)
		}

		// Convert to session list with scores and snippets
		matches := make([]map[string]interface{}, len(results))
		for i, result := range results {
			matches[i] = map[string]interface{}{
				"session": result.Session,
				"score":   result.Score,
				"snippet": result.Snippet,
			}
		}

		result := map[string]interface{}{
			"query":   args.Query,
			"matches": matches,
			"count":   len(matches),
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

// indexSessions lazily indexes sessions that need updating
func indexSessions(adaptersMap map[string]adapters.SessionAdapter, cache *search.Cache, source string, projectPath string) error {
	// Determine which adapters to index
	adaptersToQuery := make(map[string]adapters.SessionAdapter)
	if source != "" {
		if adapter, ok := adaptersMap[source]; ok {
			adaptersToQuery[source] = adapter
		}
	} else {
		adaptersToQuery = adaptersMap
	}

	// Index sessions from each adapter
	for _, adapter := range adaptersToQuery {
		sessions, err := adapter.ListSessions(projectPath, 0) // Get all sessions
		if err != nil {
			log.Printf("Error listing sessions for %s: %v", adapter.Name(), err)
			continue
		}

		for _, session := range sessions {
			// Check if session needs reindexing
			needsReindex, err := cache.NeedsReindex(session.ID, session.FilePath)
			if err != nil {
				log.Printf("Error checking if session needs reindex: %v", err)
				continue
			}

			if !needsReindex {
				continue
			}

			// Get full session content for indexing
			messages, err := adapter.GetSession(session.ID, 0, 100000) // Get all messages
			if err != nil {
				log.Printf("Error getting session %s: %v", session.ID, err)
				continue
			}

			// Combine all message content
			var contentBuilder strings.Builder
			contentBuilder.WriteString(session.FirstMessage)
			contentBuilder.WriteString(" ")
			contentBuilder.WriteString(session.Summary)
			for _, msg := range messages {
				contentBuilder.WriteString(" ")
				contentBuilder.WriteString(msg.Content)
			}

			// Index the session
			if err := cache.IndexSession(session, contentBuilder.String()); err != nil {
				log.Printf("Error indexing session %s: %v", session.ID, err)
				continue
			}
		}
	}

	return nil
}

// Tool 4: get_session
type getSessionArgs struct {
	SessionID string `json:"session_id" jsonschema:"The session ID to retrieve"`
	Source    string `json:"source" jsonschema:"The source that created this session (claude, gemini, codex, opencode)"`
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
		if args.Source == "" {
			return nil, nil, fmt.Errorf("source is required")
		}

		adapter, ok := adaptersMap[args.Source]
		if !ok {
			return nil, nil, fmt.Errorf("unknown source: %s", args.Source)
		}

		if args.PageSize == 0 {
			args.PageSize = 20
		}

		messages, err := adapter.GetSession(args.SessionID, args.Page, args.PageSize)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get session: %w", err)
		}

		result := map[string]interface{}{
			"session_id": args.SessionID,
			"source":     args.Source,
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
