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

// OpencodeAdapter implements SessionAdapter for opencode CLI sessions.
// opencode stores sessions in ~/.local/share/opencode/storage/
// Structure:
// - project/[PROJECT_ID].json - project metadata (worktree path, vcs)
// - session/[PROJECT_ID]/ses_*.json - session metadata (title, timestamps)
// - message/ses_*/msg_*.json - individual messages in each session
type OpencodeAdapter struct {
	homeDir string
}

// NewOpencodeAdapter creates a new opencode session adapter.
func NewOpencodeAdapter() (*OpencodeAdapter, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}
	return &OpencodeAdapter{homeDir: homeDir}, nil
}

// Name returns the adapter name.
func (o *OpencodeAdapter) Name() string {
	return "opencode"
}

// opencodeProject represents a project file in storage/project/
type opencodeProject struct {
	ID       string `json:"id"`
	Worktree string `json:"worktree"`
	VCS      string `json:"vcs"`
	Time     struct {
		Created int64 `json:"created"`
	} `json:"time"`
}

// opencodeSession represents a session file in storage/session/[PROJECT_ID]/
type opencodeSession struct {
	ID        string `json:"id"`
	Version   string `json:"version"`
	ProjectID string `json:"projectID"`
	Directory string `json:"directory"`
	Title     string `json:"title"`
	Time      struct {
		Created int64 `json:"created"`
		Updated int64 `json:"updated"`
	} `json:"time"`
}

// opencodeMessage represents a message file in storage/message/[SESSION_ID]/
type opencodeMessage struct {
	ID       string                 `json:"id"`
	Role     string                 `json:"role"`
	System   interface{}            `json:"system,omitempty"` // Can be string or array
	Mode     string                 `json:"mode,omitempty"`
	Content  interface{}            `json:"content,omitempty"`
	Cost     float64                `json:"cost,omitempty"`
	Tokens   map[string]interface{} `json:"tokens,omitempty"`
	ModelID  string                 `json:"modelID,omitempty"`
	Time     map[string]interface{} `json:"time,omitempty"`
	SessionID string                `json:"sessionID,omitempty"`
}

// ListSessions returns all opencode sessions for the given project.
// If projectPath is empty, returns sessions from ALL projects.
func (o *OpencodeAdapter) ListSessions(projectPath string, limit int) ([]Session, error) {
	storageDir := filepath.Join(o.homeDir, ".local", "share", "opencode", "storage")

	// Check if storage directory exists
	if _, err := os.Stat(storageDir); os.IsNotExist(err) {
		return []Session{}, nil
	}

	// If project path specified, find matching project ID
	var targetProjectID string
	if projectPath != "" {
		absPath, err := filepath.Abs(projectPath)
		if err != nil {
			return nil, fmt.Errorf("failed to get absolute path: %w", err)
		}

		projectID, err := o.findProjectIDByPath(storageDir, absPath)
		if err != nil || projectID == "" {
			return []Session{}, nil // No matching project
		}
		targetProjectID = projectID
	}

	// List all sessions
	sessionDir := filepath.Join(storageDir, "session")
	projectDirs, err := os.ReadDir(sessionDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read session directory: %w", err)
	}

	var allSessions []Session
	for _, projectDir := range projectDirs {
		if !projectDir.IsDir() {
			continue
		}

		projectID := projectDir.Name()

		// Filter by project if specified
		if targetProjectID != "" && projectID != targetProjectID {
			continue
		}

		// Get project metadata for worktree path
		project, err := o.loadProject(storageDir, projectID)
		if err != nil {
			continue
		}

		// List sessions for this project
		sessions, err := o.listProjectSessions(storageDir, projectID, project.Worktree)
		if err != nil {
			continue
		}

		allSessions = append(allSessions, sessions...)
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

// findProjectIDByPath finds a project ID by matching the worktree path
func (o *OpencodeAdapter) findProjectIDByPath(storageDir, targetPath string) (string, error) {
	projectDir := filepath.Join(storageDir, "project")
	files, err := filepath.Glob(filepath.Join(projectDir, "*.json"))
	if err != nil {
		return "", err
	}

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		var project opencodeProject
		if err := json.Unmarshal(data, &project); err != nil {
			continue
		}

		if project.Worktree == targetPath {
			return project.ID, nil
		}
	}

	return "", nil
}

// loadProject loads project metadata
func (o *OpencodeAdapter) loadProject(storageDir, projectID string) (*opencodeProject, error) {
	projectFile := filepath.Join(storageDir, "project", projectID+".json")
	data, err := os.ReadFile(projectFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read project file: %w", err)
	}

	var project opencodeProject
	if err := json.Unmarshal(data, &project); err != nil {
		return nil, fmt.Errorf("failed to parse project JSON: %w", err)
	}

	return &project, nil
}

// listProjectSessions lists all sessions for a specific project
func (o *OpencodeAdapter) listProjectSessions(storageDir, projectID, worktree string) ([]Session, error) {
	sessionDir := filepath.Join(storageDir, "session", projectID)
	files, err := filepath.Glob(filepath.Join(sessionDir, "ses_*.json"))
	if err != nil {
		return nil, err
	}

	var sessions []Session
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		var sess opencodeSession
		if err := json.Unmarshal(data, &sess); err != nil {
			continue
		}

		// Get first message content
		firstMessage, err := o.getFirstUserMessage(storageDir, sess.ID)
		if err != nil {
			firstMessage = "" // Continue even if we can't get first message
		}

		session := Session{
			ID:           sess.ID,
			Tool:         "opencode",
			ProjectPath:  worktree,
			FirstMessage: firstMessage,
			Summary:      sess.Title,
			Timestamp:    time.UnixMilli(sess.Time.Created),
			FilePath:     file,
		}

		sessions = append(sessions, session)
	}

	return sessions, nil
}

// getFirstUserMessage extracts the first user message from a session
func (o *OpencodeAdapter) getFirstUserMessage(storageDir, sessionID string) (string, error) {
	messageDir := filepath.Join(storageDir, "message", sessionID)
	files, err := filepath.Glob(filepath.Join(messageDir, "msg_*.json"))
	if err != nil {
		return "", err
	}

	// Sort by filename (contains timestamp-like component)
	sort.Strings(files)

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		var msg opencodeMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		// Find first user message
		if msg.Role == "user" {
			content := o.extractMessageContent(msg.Content)
			if content != "" {
				return o.extractFirstLine(content), nil
			}
		}
	}

	return "", nil
}

// extractMessageContent converts message content to string
func (o *OpencodeAdapter) extractMessageContent(content interface{}) string {
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
	}
	return ""
}

// extractFirstLine extracts the first non-empty line from text
func (o *OpencodeAdapter) extractFirstLine(text string) string {
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			if len(trimmed) > 200 {
				return trimmed[:200] + "..."
			}
			return trimmed
		}
	}
	return ""
}

// GetSession retrieves the full content of an opencode session with pagination
func (o *OpencodeAdapter) GetSession(sessionID string, page, pageSize int) ([]Message, error) {
	storageDir := filepath.Join(o.homeDir, ".local", "share", "opencode", "storage")
	messageDir := filepath.Join(storageDir, "message", sessionID)

	// Check if message directory exists
	if _, err := os.Stat(messageDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	// Read all messages
	messages, err := o.readAllMessages(messageDir)
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

// readAllMessages reads all messages from a session directory
func (o *OpencodeAdapter) readAllMessages(messageDir string) ([]Message, error) {
	files, err := filepath.Glob(filepath.Join(messageDir, "msg_*.json"))
	if err != nil {
		return nil, fmt.Errorf("failed to list message files: %w", err)
	}

	// Sort by filename (contains timestamp)
	sort.Strings(files)

	var messages []Message
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		var msg opencodeMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		message := Message{
			Role:     msg.Role,
			Content:  o.extractMessageContent(msg.Content),
			Metadata: make(map[string]interface{}),
		}

		// Parse timestamp from time.created
		if msg.Time != nil {
			if created, ok := msg.Time["created"].(float64); ok {
				message.Timestamp = time.UnixMilli(int64(created))
			}
		}

		// Add metadata
		if msg.ModelID != "" {
			message.Metadata["model"] = msg.ModelID
		}
		if msg.Mode != "" {
			message.Metadata["mode"] = msg.Mode
		}
		if msg.Cost > 0 {
			message.Metadata["cost"] = msg.Cost
		}
		if msg.Tokens != nil {
			message.Metadata["tokens"] = msg.Tokens
		}

		messages = append(messages, message)
	}

	return messages, nil
}

// SearchSessions searches opencode sessions for the given query
func (o *OpencodeAdapter) SearchSessions(projectPath, query string, limit int) ([]Session, error) {
	// First, list all sessions
	sessions, err := o.ListSessions(projectPath, 0)
	if err != nil {
		return nil, err
	}

	query = strings.ToLower(query)
	var matches []Session

	// Search through each session
	for _, session := range sessions {
		// Check if query is in title or first message
		if strings.Contains(strings.ToLower(session.Summary), query) ||
			strings.Contains(strings.ToLower(session.FirstMessage), query) {
			matches = append(matches, session)
			continue
		}

		// Search through full session content
		storageDir := filepath.Join(o.homeDir, ".local", "share", "opencode", "storage")
		messageDir := filepath.Join(storageDir, "message", session.ID)
		messages, err := o.readAllMessages(messageDir)
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
