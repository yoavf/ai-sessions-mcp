package adapters

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ClaudeAdapter implements SessionAdapter for Claude Code CLI sessions.
// Claude Code stores sessions as JSONL files in ~/.claude/projects/[PROJECT_DIR]/
// where PROJECT_DIR is derived from the actual project path.
type ClaudeAdapter struct {
	homeDir string
}

// NewClaudeAdapter creates a new Claude Code session adapter.
// It automatically determines the user's home directory.
func NewClaudeAdapter() (*ClaudeAdapter, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}
	return &ClaudeAdapter{homeDir: homeDir}, nil
}

// Name returns the adapter name.
func (c *ClaudeAdapter) Name() string {
	return "claude"
}

// claudeMessage represents a single message entry in a Claude Code JSONL file.
type claudeMessage struct {
	Type    string                 `json:"type"`
	Summary string                 `json:"summary,omitempty"`
	Role    string                 `json:"role,omitempty"`
	Content interface{}            `json:"content,omitempty"`
	LeafUUID string                `json:"leafUuid,omitempty"`
	Metadata map[string]interface{} `json:"-"` // Capture any extra fields
}

// projectDirName converts an absolute project path to Claude's directory naming format.
// Claude uses the path with slashes converted to hyphens.
func projectDirName(projectPath string) string {
	// Clean the path and replace slashes with hyphens
	cleaned := filepath.Clean(projectPath)
	return strings.ReplaceAll(cleaned, "/", "-")
}

// ListSessions returns all Claude Code sessions for the given project.
// If projectPath is empty, returns sessions from ALL projects.
func (c *ClaudeAdapter) ListSessions(projectPath string, limit int) ([]Session, error) {
	claudeProjectsDir := filepath.Join(c.homeDir, ".claude", "projects")

	// If no project path specified, list sessions from ALL projects
	if projectPath == "" {
		return c.listAllSessions(claudeProjectsDir, limit)
	}

	// Get absolute path
	projectPath, err := filepath.Abs(projectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Convert to Claude's directory naming format
	dirName := projectDirName(projectPath)
	sessionsDir := filepath.Join(claudeProjectsDir, dirName)

	// Check if directory exists
	if _, err := os.Stat(sessionsDir); os.IsNotExist(err) {
		return []Session{}, nil // No sessions for this project
	}

	// Read all .jsonl files
	files, err := filepath.Glob(filepath.Join(sessionsDir, "*.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("failed to list session files: %w", err)
	}

	sessions := make([]Session, 0, len(files))
	for _, filePath := range files {
		session, err := c.parseSessionMetadata(filePath, projectPath)
		if err != nil {
			// Skip files we can't parse
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

// listAllSessions lists sessions from all projects.
func (c *ClaudeAdapter) listAllSessions(claudeProjectsDir string, limit int) ([]Session, error) {
	// Check if projects directory exists
	if _, err := os.Stat(claudeProjectsDir); os.IsNotExist(err) {
		return []Session{}, nil
	}

	// Read all project directories
	projectDirs, err := os.ReadDir(claudeProjectsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read projects directory: %w", err)
	}

	var allSessions []Session
	for _, dir := range projectDirs {
		if !dir.IsDir() {
			continue
		}

		projectDir := filepath.Join(claudeProjectsDir, dir.Name())
		files, err := filepath.Glob(filepath.Join(projectDir, "*.jsonl"))
		if err != nil {
			continue
		}

		// Reverse-engineer project path from directory name
		// Directory names start with a dash, e.g., "-Users-yoavfarhi-dev-project"
		projectPath := strings.ReplaceAll(dir.Name(), "-", "/")
		// Remove leading slash if present
		projectPath = strings.TrimPrefix(projectPath, "/")

		for _, filePath := range files {
			session, err := c.parseSessionMetadata(filePath, projectPath)
			if err != nil {
				continue
			}
			allSessions = append(allSessions, session)
		}
	}

	// Sort by timestamp (newest first)
	sort.Slice(allSessions, func(i, j int) bool {
		return allSessions[i].Timestamp.After(allSessions[j].Timestamp)
	})

	// Apply limit
	if limit > 0 && len(allSessions) > limit {
		allSessions = allSessions[:limit]
	}

	return allSessions, nil
}

// parseSessionMetadata extracts metadata from a Claude Code session file.
// It reads the first few lines to get the summary and first user message.
func (c *ClaudeAdapter) parseSessionMetadata(filePath, projectPath string) (Session, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return Session{}, fmt.Errorf("failed to open session file: %w", err)
	}
	defer file.Close()

	var session Session
	session.ID = strings.TrimSuffix(filepath.Base(filePath), ".jsonl")
	session.Tool = "claude"
	session.ProjectPath = projectPath
	session.FilePath = filePath

	// Get file modification time as a fallback timestamp
	if stat, err := os.Stat(filePath); err == nil {
		session.Timestamp = stat.ModTime()
	}

	scanner := bufio.NewScanner(file)
	foundFirstMessage := false

	// Read through the file to find summary and first user message
	for scanner.Scan() {
		var msg claudeMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue // Skip malformed lines
		}

		// Capture summary if available
		if msg.Type == "summary" && msg.Summary != "" {
			session.Summary = msg.Summary
		}

		// Capture first user message
		if msg.Type == "user" && !foundFirstMessage {
			session.FirstMessage = extractFirstLine(msg.Content)
			foundFirstMessage = true
			break // We have what we need
		}
	}

	if err := scanner.Err(); err != nil {
		return session, fmt.Errorf("error reading session file: %w", err)
	}

	return session, nil
}

// extractFirstLine extracts the first non-empty line from content.
// Content can be a string or a structured object.
func extractFirstLine(content interface{}) string {
	switch v := content.(type) {
	case string:
		lines := strings.Split(v, "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				// Limit to 200 characters
				if len(trimmed) > 200 {
					return trimmed[:200] + "..."
				}
				return trimmed
			}
		}
	case []interface{}:
		// Claude sometimes uses structured content blocks
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				if text, ok := m["text"].(string); ok {
					return extractFirstLine(text)
				}
			}
		}
	case map[string]interface{}:
		if text, ok := v["text"].(string); ok {
			return extractFirstLine(text)
		}
	}
	return ""
}

// GetSession retrieves the full content of a Claude Code session with pagination.
func (c *ClaudeAdapter) GetSession(sessionID string, page, pageSize int) ([]Message, error) {
	// Find the session file
	// We need to search all project directories since we only have the session ID
	claudeDir := filepath.Join(c.homeDir, ".claude", "projects")
	projectDirs, err := os.ReadDir(claudeDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read Claude projects directory: %w", err)
	}

	var sessionFile string
	for _, dir := range projectDirs {
		if !dir.IsDir() {
			continue
		}
		candidate := filepath.Join(claudeDir, dir.Name(), sessionID+".jsonl")
		if _, err := os.Stat(candidate); err == nil {
			sessionFile = candidate
			break
		}
	}

	if sessionFile == "" {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	// Read all messages from the file
	messages, err := c.readAllMessages(sessionFile)
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

// readAllMessages reads all messages from a Claude Code session file.
func (c *ClaudeAdapter) readAllMessages(filePath string) ([]Message, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open session file: %w", err)
	}
	defer file.Close()

	var messages []Message
	scanner := bufio.NewScanner(file)

	// Increase buffer size for large messages
	buf := make([]byte, 0, 1024*1024) // 1MB buffer
	scanner.Buffer(buf, 10*1024*1024)  // Max 10MB per line

	for scanner.Scan() {
		var msg claudeMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue // Skip malformed lines
		}

		// Only process user and assistant messages
		if msg.Type != "user" && msg.Type != "assistant" {
			continue
		}

		message := Message{
			Role:     msg.Type,
			Content:  contentToString(msg.Content),
			Metadata: make(map[string]interface{}),
		}

		// Add any additional metadata
		if msg.Type == "assistant" {
			// Preserve structured content for tool calls, thinking blocks, etc.
			message.Metadata["raw_content"] = msg.Content
		}

		messages = append(messages, message)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading session file: %w", err)
	}

	return messages, nil
}

// contentToString converts various content formats to a plain string.
func contentToString(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				if text, ok := m["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	case map[string]interface{}:
		if text, ok := v["text"].(string); ok {
			return text
		}
		// Fallback to JSON representation
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
	}
	return fmt.Sprintf("%v", content)
}

// SearchSessions searches Claude Code sessions for the given query.
func (c *ClaudeAdapter) SearchSessions(projectPath, query string, limit int) ([]Session, error) {
	// First, list all sessions
	sessions, err := c.ListSessions(projectPath, 0)
	if err != nil {
		return nil, err
	}

	query = strings.ToLower(query)
	var matches []Session

	// Search through each session
	for _, session := range sessions {
		// Check if query is in summary or first message
		if strings.Contains(strings.ToLower(session.Summary), query) ||
			strings.Contains(strings.ToLower(session.FirstMessage), query) {
			matches = append(matches, session)
			continue
		}

		// Search through full session content
		messages, err := c.readAllMessages(session.FilePath)
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
