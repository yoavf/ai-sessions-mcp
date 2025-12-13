package adapters

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// CopilotAdapter implements SessionAdapter for GitHub Copilot CLI sessions.
// Copilot CLI stores sessions as JSONL files in ~/.copilot/session-state/
type CopilotAdapter struct {
	homeDir string
}

// NewCopilotAdapter creates a new GitHub Copilot CLI session adapter.
func NewCopilotAdapter() (*CopilotAdapter, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}
	return &CopilotAdapter{
		homeDir: homeDir,
	}, nil
}

// Name returns the adapter name.
func (c *CopilotAdapter) Name() string {
	return "copilot"
}

// copilotEvent represents a single event line in a Copilot JSONL session file.
type copilotEvent struct {
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
	ID        string          `json:"id"`
	Timestamp string          `json:"timestamp"`
	ParentID  *string         `json:"parentId"`
}

// copilotSessionStart represents the data for a session.start event.
type copilotSessionStart struct {
	SessionID      string `json:"sessionId"`
	Version        int    `json:"version"`
	Producer       string `json:"producer"`
	CopilotVersion string `json:"copilotVersion"`
	StartTime      string `json:"startTime"`
}

// copilotSessionInfo represents the data for a session.info event.
type copilotSessionInfo struct {
	InfoType string `json:"infoType"`
	Message  string `json:"message"`
}

// copilotModelChange represents the data for a session.model_change event.
type copilotModelChange struct {
	PreviousModel string `json:"previousModel,omitempty"`
	NewModel      string `json:"newModel"`
}

// copilotUserMessage represents the data for a user.message event.
type copilotUserMessage struct {
	Content     string        `json:"content"`
	Attachments []interface{} `json:"attachments,omitempty"`
}

// copilotAssistantMessage represents the data for an assistant.message event.
type copilotAssistantMessage struct {
	MessageID    string               `json:"messageId"`
	Content      string               `json:"content"`
	ToolRequests []copilotToolRequest `json:"toolRequests,omitempty"`
}

// copilotToolRequest represents a tool request in an assistant message.
type copilotToolRequest struct {
	ToolCallID string          `json:"toolCallId"`
	Name       string          `json:"name"`
	Arguments  json.RawMessage `json:"arguments"`
}

// copilotToolExecution represents the data for tool execution events.
type copilotToolExecution struct {
	ToolCallID string          `json:"toolCallId"`
	ToolName   string          `json:"toolName"`
	Arguments  json.RawMessage `json:"arguments"`
	Success    bool            `json:"success,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
}

// ListSessions returns all Copilot CLI sessions for the given project.
// If projectPath is empty, returns sessions from ALL projects.
func (c *CopilotAdapter) ListSessions(projectPath string, limit int) ([]Session, error) {
	sessionsDir := filepath.Join(c.homeDir, ".copilot", "session-state")

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

	// Read all *.jsonl files
	files, err := filepath.Glob(filepath.Join(sessionsDir, "*.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("failed to list session files: %w", err)
	}

	sessions := make([]Session, 0, len(files))
	for _, filePath := range files {
		session, err := c.parseSessionMetadata(filePath)
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

// parseSessionMetadata extracts metadata from a Copilot CLI session file.
func (c *CopilotAdapter) parseSessionMetadata(filePath string) (Session, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return Session{}, fmt.Errorf("failed to open session file: %w", err)
	}
	defer file.Close()

	session := Session{
		Source:   "copilot",
		FilePath: filePath,
	}

	// Regex to extract folder path from folder_trust message
	folderTrustRegex := regexp.MustCompile(`Folder (.+) has been added to trusted folders`)

	// Track file paths seen in tool calls for project path inference
	var seenFilePaths []string
	userCount := 0

	scanner := bufio.NewScanner(file)
	// Increase buffer size for long lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		var event copilotEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}

		switch event.Type {
		case "session.start":
			var data copilotSessionStart
			if err := json.Unmarshal(event.Data, &data); err == nil {
				session.ID = data.SessionID
				if ts, err := time.Parse(time.RFC3339Nano, data.StartTime); err == nil {
					session.Timestamp = ts
				} else if ts, err := time.Parse(time.RFC3339, data.StartTime); err == nil {
					session.Timestamp = ts
				}
			}

		case "session.info":
			var data copilotSessionInfo
			if err := json.Unmarshal(event.Data, &data); err == nil {
				if data.InfoType == "folder_trust" {
					// Extract project path from folder_trust message
					if matches := folderTrustRegex.FindStringSubmatch(data.Message); len(matches) > 1 {
						session.ProjectPath = matches[1]
					}
				}
			}

		case "user.message":
			var data copilotUserMessage
			if err := json.Unmarshal(event.Data, &data); err == nil {
				userCount++
				if session.FirstMessage == "" {
					session.FirstMessage = extractFirstLine(data.Content)
				}
			}

		case "tool.execution_start":
			// Extract file paths from tool arguments for project path inference
			var data copilotToolExecution
			if err := json.Unmarshal(event.Data, &data); err == nil {
				var args map[string]interface{}
				if err := json.Unmarshal(data.Arguments, &args); err == nil {
					if path, ok := args["path"].(string); ok && strings.HasPrefix(path, "/") {
						seenFilePaths = append(seenFilePaths, path)
					}
				}
			}
		}
	}

	session.UserMessageCount = userCount

	// If we don't have a project path from folder_trust, infer from file paths
	if session.ProjectPath == "" && len(seenFilePaths) > 0 {
		session.ProjectPath = findCommonDirectory(seenFilePaths)
	}

	// If we still don't have a timestamp, use file modification time
	if session.Timestamp.IsZero() {
		if stat, err := os.Stat(filePath); err == nil {
			session.Timestamp = stat.ModTime()
		}
	}

	// Extract session ID from filename if not found in content
	if session.ID == "" {
		base := filepath.Base(filePath)
		session.ID = strings.TrimSuffix(base, ".jsonl")
	}

	return session, nil
}

// findCommonDirectory finds the longest common directory path from a list of file paths.
func findCommonDirectory(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	if len(paths) == 1 {
		return filepath.Dir(paths[0])
	}

	// Start with the directory of the first path
	common := filepath.Dir(paths[0])

	for _, p := range paths[1:] {
		dir := filepath.Dir(p)
		for !strings.HasPrefix(dir, common) && common != "/" && common != "" {
			common = filepath.Dir(common)
		}
	}

	return common
}

// GetSession retrieves the full content of a Copilot CLI session with pagination.
func (c *CopilotAdapter) GetSession(sessionID string, page, pageSize int) ([]Message, error) {
	sessionsDir := filepath.Join(c.homeDir, ".copilot", "session-state")

	// Try to find the session file directly by ID
	sessionFile := filepath.Join(sessionsDir, sessionID+".jsonl")
	if _, err := os.Stat(sessionFile); os.IsNotExist(err) {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	// Read all messages from the session
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

// readAllMessages reads all messages from a Copilot CLI session file.
func (c *CopilotAdapter) readAllMessages(filePath string) ([]Message, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open session file: %w", err)
	}
	defer file.Close()

	var messages []Message
	var currentModel string

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		var event copilotEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}

		var timestamp time.Time
		if event.Timestamp != "" {
			if ts, err := time.Parse(time.RFC3339Nano, event.Timestamp); err == nil {
				timestamp = ts
			} else if ts, err := time.Parse(time.RFC3339, event.Timestamp); err == nil {
				timestamp = ts
			}
		}

		switch event.Type {
		case "session.model_change":
			var data copilotModelChange
			if err := json.Unmarshal(event.Data, &data); err == nil {
				currentModel = data.NewModel
			}

		case "user.message":
			var data copilotUserMessage
			if err := json.Unmarshal(event.Data, &data); err == nil {
				msg := Message{
					Role:      "user",
					Content:   data.Content,
					Timestamp: timestamp,
					Metadata:  make(map[string]interface{}),
				}
				if currentModel != "" {
					msg.Metadata["model"] = currentModel
				}
				messages = append(messages, msg)
			}

		case "assistant.message":
			var data copilotAssistantMessage
			if err := json.Unmarshal(event.Data, &data); err == nil {
				msg := Message{
					Role:      "assistant",
					Content:   data.Content,
					Timestamp: timestamp,
					Metadata:  make(map[string]interface{}),
				}
				if currentModel != "" {
					msg.Metadata["model"] = currentModel
				}
				// Add tool requests to metadata if present
				if len(data.ToolRequests) > 0 {
					toolCalls := make([]map[string]interface{}, len(data.ToolRequests))
					for i, tr := range data.ToolRequests {
						var args interface{}
						if err := json.Unmarshal(tr.Arguments, &args); err != nil {
							// Fallback to raw string if unmarshaling fails
							args = string(tr.Arguments)
						}
						toolCalls[i] = map[string]interface{}{
							"id":        tr.ToolCallID,
							"name":      tr.Name,
							"arguments": args,
						}
					}
					msg.Metadata["tool_calls"] = toolCalls
				}
				messages = append(messages, msg)
			}

		case "tool.execution_complete":
			var data copilotToolExecution
			if err := json.Unmarshal(event.Data, &data); err == nil {
				var result interface{}
				json.Unmarshal(data.Result, &result)
				msg := Message{
					Role:      "tool",
					Timestamp: timestamp,
					Metadata: map[string]interface{}{
						"tool_call_id": data.ToolCallID,
						"tool_name":    data.ToolName,
						"success":      data.Success,
						"result":       result,
					},
				}
				// Format tool result as content
				if resultStr, ok := result.(string); ok {
					msg.Content = resultStr
				} else if result != nil {
					if resultBytes, err := json.Marshal(result); err == nil {
						msg.Content = string(resultBytes)
					}
				}
				messages = append(messages, msg)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading session file: %w", err)
	}

	return messages, nil
}

// SearchSessions searches Copilot CLI sessions for the given query.
// It reads each file only once to avoid redundant I/O.
func (c *CopilotAdapter) SearchSessions(projectPath, query string, limit int) ([]Session, error) {
	sessionsDir := filepath.Join(c.homeDir, ".copilot", "session-state")

	if _, err := os.Stat(sessionsDir); os.IsNotExist(err) {
		return []Session{}, nil
	}

	if projectPath != "" {
		var err error
		projectPath, err = filepath.Abs(projectPath)
		if err != nil {
			return nil, fmt.Errorf("failed to get absolute path: %w", err)
		}
	}

	files, err := filepath.Glob(filepath.Join(sessionsDir, "*.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("failed to list session files: %w", err)
	}

	query = strings.ToLower(query)
	var matches []Session

	// Read each file once and search in a single pass
	for _, filePath := range files {
		session, contents, err := c.parseSessionWithContents(filePath)
		if err != nil {
			continue
		}

		// Filter by project path if specified
		if projectPath != "" && session.ProjectPath != projectPath {
			continue
		}

		// Search in all message content
		found := false
		for _, content := range contents {
			if strings.Contains(strings.ToLower(content), query) {
				found = true
				break
			}
		}

		if found {
			matches = append(matches, session)
			if limit > 0 && len(matches) >= limit {
				break
			}
		}
	}

	// Sort by timestamp (newest first)
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Timestamp.After(matches[j].Timestamp)
	})

	return matches, nil
}

// parseSessionWithContents reads a session file and returns metadata plus all message contents.
// This avoids reading the file twice when both are needed for searching.
func (c *CopilotAdapter) parseSessionWithContents(filePath string) (Session, []string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return Session{}, nil, fmt.Errorf("failed to open session file: %w", err)
	}
	defer file.Close()

	session := Session{
		Source:   "copilot",
		FilePath: filePath,
	}

	folderTrustRegex := regexp.MustCompile(`Folder (.+) has been added to trusted folders`)
	var seenFilePaths []string
	var contents []string
	userCount := 0

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		var event copilotEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}

		switch event.Type {
		case "session.start":
			var data copilotSessionStart
			if err := json.Unmarshal(event.Data, &data); err == nil {
				session.ID = data.SessionID
				if ts, err := time.Parse(time.RFC3339Nano, data.StartTime); err == nil {
					session.Timestamp = ts
				} else if ts, err := time.Parse(time.RFC3339, data.StartTime); err == nil {
					session.Timestamp = ts
				}
			}

		case "session.info":
			var data copilotSessionInfo
			if err := json.Unmarshal(event.Data, &data); err == nil {
				if data.InfoType == "folder_trust" {
					if matches := folderTrustRegex.FindStringSubmatch(data.Message); len(matches) > 1 {
						session.ProjectPath = matches[1]
					}
				}
			}

		case "user.message":
			var data copilotUserMessage
			if err := json.Unmarshal(event.Data, &data); err == nil {
				userCount++
				contents = append(contents, data.Content)
				if session.FirstMessage == "" {
					session.FirstMessage = extractFirstLine(data.Content)
				}
			}

		case "assistant.message":
			var data copilotAssistantMessage
			if err := json.Unmarshal(event.Data, &data); err == nil {
				contents = append(contents, data.Content)
			}

		case "tool.execution_start":
			var data copilotToolExecution
			if err := json.Unmarshal(event.Data, &data); err == nil {
				var args map[string]interface{}
				if err := json.Unmarshal(data.Arguments, &args); err == nil {
					if path, ok := args["path"].(string); ok && strings.HasPrefix(path, "/") {
						seenFilePaths = append(seenFilePaths, path)
					}
				}
			}
		}
	}

	session.UserMessageCount = userCount

	if session.ProjectPath == "" && len(seenFilePaths) > 0 {
		session.ProjectPath = findCommonDirectory(seenFilePaths)
	}

	if session.Timestamp.IsZero() {
		if stat, err := os.Stat(filePath); err == nil {
			session.Timestamp = stat.ModTime()
		}
	}

	if session.ID == "" {
		base := filepath.Base(filePath)
		session.ID = strings.TrimSuffix(base, ".jsonl")
	}

	return session, contents, nil
}
