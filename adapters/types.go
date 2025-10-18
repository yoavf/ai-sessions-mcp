// Package adapters provides interfaces and types for accessing AI assistant sessions
// from different CLI coding agents (Claude Code, Gemini CLI, OpenAI Codex, opencode).
package adapters

import "time"

// Session represents a unified view of an AI assistant session, regardless of the source agent.
// Each session contains metadata about when it occurred, what was discussed, and how to retrieve its full content.
type Session struct {
	// ID is the unique identifier for this session (format varies by source)
	ID string `json:"id"`

	// Source identifies which CLI coding agent created this session (e.g., "claude", "gemini", "codex", "opencode")
	Source string `json:"source"`

	// ProjectPath is the absolute path to the project directory where this session occurred
	ProjectPath string `json:"project_path"`

	// FirstMessage contains the first line or summary of the initial user message
	FirstMessage string `json:"first_message"`

	// Timestamp is when the session started or first message was sent
	Timestamp time.Time `json:"timestamp"`

	// UserMessageCount is the number of user-authored messages in the session
	UserMessageCount int `json:"user_message_count,omitempty"`

	// FilePath is the absolute path to the session file on disk
	FilePath string `json:"file_path"`

	// Summary is an optional high-level summary of the session (if available)
	Summary string `json:"summary,omitempty"`
}

// Message represents a single message within a session.
// This provides a unified format for messages across different agents.
type Message struct {
	// Role identifies who sent the message: "user", "assistant", or "system"
	Role string `json:"role"`

	// Content is the text content of the message
	Content string `json:"content"`

	// Timestamp is when this message was sent (may be empty for some agents)
	Timestamp time.Time `json:"timestamp,omitempty"`

	// Metadata contains agent-specific additional data (e.g., tool calls, thinking blocks)
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// SessionAdapter is the interface that each agent-specific adapter must implement.
// It provides methods to list sessions and retrieve full session content.
type SessionAdapter interface {
	// Name returns the name of this adapter (e.g., "claude", "gemini")
	Name() string

	// ListSessions returns all sessions for the given project path.
	// If projectPath is empty, it returns sessions for the current directory.
	// The limit parameter restricts the number of results (0 = no limit).
	ListSessions(projectPath string, limit int) ([]Session, error)

	// GetSession retrieves the full content of a session by ID.
	// The page parameter allows paginating through long sessions (0-indexed).
	// Each page contains up to pageSize messages.
	GetSession(sessionID string, page, pageSize int) ([]Message, error)

	// SearchSessions finds sessions containing the query string in their messages.
	// Returns matching sessions with the query highlighted in context.
	SearchSessions(projectPath, query string, limit int) ([]Session, error)
}
