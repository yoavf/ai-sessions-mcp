package adapters

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// MistralAdapter implements SessionAdapter for Mistral Vibe CLI sessions.
// Mistral Vibe stores sessions as JSON files in ~/.vibe/logs/session/
type MistralAdapter struct {
	homeDir string
}

// NewMistralAdapter creates a new Mistral Vibe session adapter.
func NewMistralAdapter() (*MistralAdapter, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}
	return &MistralAdapter{
		homeDir: homeDir,
	}, nil
}

// Name returns the adapter name.
func (m *MistralAdapter) Name() string {
	return "mistral"
}

// mistralSession represents the structure of a Mistral Vibe session JSON file.
type mistralSession struct {
	Metadata mistralMetadata  `json:"metadata"`
	Messages []mistralMessage `json:"messages"`
}

// mistralMetadata represents the metadata section of a Mistral Vibe session.
type mistralMetadata struct {
	SessionID   string             `json:"session_id"`
	StartTime   string             `json:"start_time"`
	Environment mistralEnvironment `json:"environment"`
}

// mistralEnvironment represents the environment section of metadata.
type mistralEnvironment struct {
	WorkingDirectory string `json:"working_directory,omitempty"`
}

// mistralMessage represents a single message in a Mistral Vibe session.
type mistralMessage struct {
	Role            string              `json:"role"`
	Content         string              `json:"content"`
	ToolCalls       []mistralToolCall   `json:"tool_calls,omitempty"`
	ToolCallResults []mistralToolResult `json:"tool_call_results,omitempty"`
}

// mistralToolCall represents a tool call in Mistral Vibe.
type mistralToolCall struct {
	ID       string              `json:"id"`
	Index    int                 `json:"index,omitempty"`
	Type     string              `json:"type"`
	Function mistralToolFunction `json:"function"`
}

// mistralToolFunction represents the function details of a tool call.
type mistralToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

// mistralToolResult represents a tool result in Mistral Vibe.
type mistralToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
	IsError    bool   `json:"is_error,omitempty"`
	Timestamp  string `json:"timestamp,omitempty"`
}

// ListSessions returns all Mistral Vibe sessions for the given project.
// If projectPath is empty, returns sessions from ALL projects.
func (m *MistralAdapter) ListSessions(projectPath string, limit int) ([]Session, error) {
	sessionsDir := filepath.Join(m.homeDir, ".vibe", "logs", "session")

	// Check if directory exists
	if _, err := os.Stat(sessionsDir); os.IsNotExist(err) {
		return []Session{}, nil // No sessions
	}

	// Get absolute path if provided
	if projectPath != "" {
		var err error
		projectPath, err = filepath.Abs(projectPath)
		if err != nil {
			return nil, fmt.Errorf("failed to get absolute path: %w", err)
		}
	}

	// Read all session-*.json files
	files, err := filepath.Glob(filepath.Join(sessionsDir, "session_*.json"))
	if err != nil {
		return nil, fmt.Errorf("failed to list session files: %w", err)
	}

	sessions := make([]Session, 0, len(files))
	for _, filePath := range files {
		session, err := m.parseSessionMetadata(filePath)
		if err != nil {
			// Skip files we can't parse
			continue
		}

		// Filter by project path if specified
		if projectPath != "" && session.ProjectPath != projectPath {
			continue
		}

		sessions = append(sessions, session)
	}

	// Sort by timestamp (newest first)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Timestamp.After(sessions[j].Timestamp)
	})

	// Apply limit
	if limit > 0 && len(sessions) > limit {
		sessions = sessions[:limit]
	}

	return sessions, nil
}

// parseSessionMetadata extracts metadata from a Mistral Vibe session file.
func (m *MistralAdapter) parseSessionMetadata(filePath string) (Session, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return Session{}, fmt.Errorf("failed to read session file: %w", err)
	}

	var mistralSess mistralSession
	if err := json.Unmarshal(data, &mistralSess); err != nil {
		return Session{}, fmt.Errorf("failed to parse session JSON: %w", err)
	}

	session := Session{
		ID:          mistralSess.Metadata.SessionID,
		Source:      "mistral",
		ProjectPath: mistralSess.Metadata.Environment.WorkingDirectory,
		FilePath:    filePath,
	}

	// Parse timestamp from start_time
	if mistralSess.Metadata.StartTime != "" {
		// Try multiple time formats
		formats := []string{
			"2006-01-02T15:04:05.999999",       // Python datetime format without timezone
			"2006-01-02T15:04:05.999999Z07:00", // With timezone
			time.RFC3339,
			time.RFC3339Nano,
		}
		for _, format := range formats {
			if ts, err := time.Parse(format, mistralSess.Metadata.StartTime); err == nil {
				session.Timestamp = ts
				break
			}
		}
	}

	// If we still don't have a timestamp, use file modification time
	if session.Timestamp.IsZero() {
		if stat, err := os.Stat(filePath); err == nil {
			session.Timestamp = stat.ModTime()
		}
	}

	// Extract first user message and count all user messages
	userCount := 0
	for _, msg := range mistralSess.Messages {
		if msg.Role != "user" {
			continue
		}
		userCount++
		if session.FirstMessage == "" {
			session.FirstMessage = extractFirstLine(msg.Content)
		}
	}

	session.UserMessageCount = userCount

	return session, nil
}

// GetSession retrieves the full content of a Mistral Vibe session with pagination.
func (m *MistralAdapter) GetSession(sessionID string, page, pageSize int) ([]Message, error) {
	sessionsDir := filepath.Join(m.homeDir, ".vibe", "logs", "session")

	// Find the session file by searching through all files
	files, err := filepath.Glob(filepath.Join(sessionsDir, "session_*.json"))
	if err != nil {
		return nil, fmt.Errorf("failed to list session files: %w", err)
	}

	var sessionFile string
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		var sess mistralSession
		if err := json.Unmarshal(data, &sess); err != nil {
			continue
		}

		if sess.Metadata.SessionID == sessionID {
			sessionFile = file
			break
		}
	}

	if sessionFile == "" {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	// Read the session file
	messages, err := m.readAllMessages(sessionFile)
	if err != nil {
		return nil, err
	}

	// Apply pagination
	start := page * pageSize
	if start >= len(messages) {
		return []Message{}, nil
	}

	end := start + pageSize
	if end > len(messages) {
		end = len(messages)
	}

	return messages[start:end], nil
}

// readAllMessages reads all messages from a Mistral Vibe session file.
func (m *MistralAdapter) readAllMessages(filePath string) ([]Message, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read session file: %w", err)
	}

	var sess mistralSession
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, fmt.Errorf("failed to parse session JSON: %w", err)
	}

	messages := make([]Message, 0, len(sess.Messages))
	for _, msg := range sess.Messages {
		// Skip system messages
		if msg.Role == "system" {
			continue
		}

		role := normalizeMistralRole(msg.Role)

		message := Message{
			Role:     role,
			Content:  msg.Content,
			Metadata: make(map[string]interface{}),
		}

		// Add tool calls to metadata if present
		if len(msg.ToolCalls) > 0 {
			toolCalls := make([]map[string]interface{}, len(msg.ToolCalls))
			for i, tc := range msg.ToolCalls {
				toolCalls[i] = map[string]interface{}{
					"id":        tc.ID,
					"name":      tc.Function.Name,
					"arguments": tc.Function.Arguments,
				}
			}
			message.Metadata["tool_calls"] = toolCalls
		}

		// Add tool results to metadata if present
		if len(msg.ToolCallResults) > 0 {
			toolResults := make([]map[string]interface{}, len(msg.ToolCallResults))
			for i, tr := range msg.ToolCallResults {
				toolResults[i] = map[string]interface{}{
					"tool_call_id": tr.ToolCallID,
					"content":      tr.Content,
					"is_error":     tr.IsError,
				}
			}
			message.Metadata["tool_results"] = toolResults
		}

		messages = append(messages, message)
	}

	return messages, nil
}

// normalizeMistralRole normalizes role names to lowercase.
func normalizeMistralRole(role string) string {
	return strings.ToLower(strings.TrimSpace(role))
}

// SearchSessions searches Mistral Vibe sessions for the given query.
func (m *MistralAdapter) SearchSessions(projectPath, query string, limit int) ([]Session, error) {
	// First, list all sessions
	sessions, err := m.ListSessions(projectPath, 0)
	if err != nil {
		return nil, err
	}

	query = strings.ToLower(query)
	var matches []Session

	// Search through each session
	for _, session := range sessions {
		// Check if query is in first message
		if strings.Contains(strings.ToLower(session.FirstMessage), query) {
			matches = append(matches, session)
			continue
		}

		// Search through full session content
		messages, err := m.readAllMessages(session.FilePath)
		if err != nil {
			continue
		}

		for _, msg := range messages {
			if strings.Contains(strings.ToLower(msg.Content), query) {
				matches = append(matches, session)
				break
			}
		}

		// Apply limit if we've found enough
		if limit > 0 && len(matches) >= limit {
			break
		}
	}

	return matches, nil
}
