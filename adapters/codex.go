package adapters

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// CodexAdapter implements SessionAdapter for OpenAI Codex CLI sessions.
// Codex stores sessions as JSONL files in ~/.codex/sessions and ~/.codex/archived_sessions
// Files are named rollout-*.jsonl and contain structured log entries.
type CodexAdapter struct {
	homeDir string
}

// NewCodexAdapter creates a new Codex CLI session adapter.
func NewCodexAdapter() (*CodexAdapter, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}
	return &CodexAdapter{homeDir: homeDir}, nil
}

// Name returns the adapter name.
func (c *CodexAdapter) Name() string {
	return "codex"
}

// codexEntry represents a single entry in a Codex rollout JSONL file.
type codexEntry struct {
	Type      string                 `json:"type"`
	Timestamp string                 `json:"timestamp,omitempty"`
	Payload   map[string]interface{} `json:"payload,omitempty"`
}

// sessionInfo holds parsed information about a Codex session.
type sessionInfo struct {
	ID                    string
	CWD                   string
	FirstUserMessage      string
	FirstMessageTimestamp string
	SessionMetaTimestamp  string
	FilePath              string
	UserMessageCount      int
}

// parseCodexTimestamp parses timestamps produced by Codex rollout files.
func parseCodexTimestamp(ts string) (time.Time, error) {
	if ts == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}

	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
	}

	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, ts); err == nil {
			return parsed, nil
		}
	}

	return time.Time{}, fmt.Errorf("unsupported timestamp format: %s", ts)
}

// ListSessions returns all Codex sessions for the given project.
// If projectPath is empty, returns sessions from ALL projects.
func (c *CodexAdapter) ListSessions(projectPath string, limit int) ([]Session, error) {
	codexHome := filepath.Join(c.homeDir, ".codex")
	sessionDirs := []string{
		filepath.Join(codexHome, "sessions"),
		filepath.Join(codexHome, "archived_sessions"),
	}

	// If no project path specified, list sessions from ALL projects
	if projectPath == "" {
		return c.listAllSessions(sessionDirs, limit)
	}

	// Get absolute path and resolve symlinks
	projectPath, err := filepath.Abs(projectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}
	projectPath, err = filepath.EvalSymlinks(projectPath)
	if err != nil {
		projectPath, _ = filepath.Abs(projectPath) // Fallback if symlink resolution fails
	}

	// Find all rollout files
	var allFiles []string
	for _, dir := range sessionDirs {
		files, err := c.findRolloutFiles(dir)
		if err != nil {
			continue // Skip directories that don't exist
		}
		allFiles = append(allFiles, files...)
	}

	if len(allFiles) == 0 {
		return []Session{}, nil
	}

	// Parse each file and filter by project path
	var sessions []Session
	for _, file := range allFiles {
		info, err := c.scanRolloutFile(file, projectPath)
		if err != nil || !info.CWDMatches(projectPath) {
			continue
		}

		session := Session{
			ID:               info.ID,
			Source:           "codex",
			ProjectPath:      projectPath,
			FirstMessage:     info.FirstUserMessage,
			UserMessageCount: info.UserMessageCount,
			FilePath:         info.FilePath,
		}

		// Parse timestamp
		tsStr := info.FirstMessageTimestamp
		if tsStr == "" {
			tsStr = info.SessionMetaTimestamp
		}
		if ts, err := parseCodexTimestamp(tsStr); err == nil {
			session.Timestamp = ts
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
func (c *CodexAdapter) listAllSessions(sessionDirs []string, limit int) ([]Session, error) {
	var allFiles []string
	for _, dir := range sessionDirs {
		files, err := c.findRolloutFiles(dir)
		if err != nil {
			continue
		}
		allFiles = append(allFiles, files...)
	}

	if len(allFiles) == 0 {
		return []Session{}, nil
	}

	var allSessions []Session
	for _, file := range allFiles {
		info, err := c.scanRolloutFile(file, "")
		if err != nil || info.CWD == "" {
			continue
		}

		session := Session{
			ID:               info.ID,
			Source:           "codex",
			ProjectPath:      info.CWD,
			FirstMessage:     info.FirstUserMessage,
			UserMessageCount: info.UserMessageCount,
			FilePath:         info.FilePath,
		}

		// Parse timestamp
		tsStr := info.FirstMessageTimestamp
		if tsStr == "" {
			tsStr = info.SessionMetaTimestamp
		}
		if ts, err := parseCodexTimestamp(tsStr); err == nil {
			session.Timestamp = ts
		}

		allSessions = append(allSessions, session)
	}

	// Sort by timestamp (newest first)
	sort.Slice(allSessions, func(i, j int) bool {
		return allSessions[i].Timestamp.After(allSessions[j].Timestamp)
	})

	return allSessions, nil
}

// findRolloutFiles recursively finds all rollout-*.jsonl files in a directory.
func (c *CodexAdapter) findRolloutFiles(root string) ([]string, error) {
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil, err
	}

	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip inaccessible files
		}
		if !info.IsDir() && strings.HasPrefix(info.Name(), "rollout-") && strings.HasSuffix(info.Name(), ".jsonl") {
			files = append(files, path)
		}
		return nil
	})

	return files, err
}

// scanRolloutFile scans a Codex rollout file to extract session information.
// It reads until it finds both the CWD and the first user message.
func (c *CodexAdapter) scanRolloutFile(filePath, targetCWD string) (*sessionInfo, error) {
	// Performance optimization: Quick pre-scan using fast byte search
	// to detect if there are any user messages before doing expensive JSON parsing.
	fileData, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read rollout file: %w", err)
	}

	info := &sessionInfo{
		FilePath: filePath,
	}

	// Fast check: does this file contain ANY user messages?
	// We look for `"role":"user"` which appears in user message entries.
	// This is much faster than JSON parsing.
	hasUserMessages := bytes.Contains(fileData, []byte(`"role":"user"`))

	// If no user messages, we still need CWD/metadata, but can skip detailed parsing
	if !hasUserMessages {
		// Quick scan for just CWD and session metadata
		scanner := bufio.NewScanner(bytes.NewReader(fileData))
		buf := make([]byte, 0, 1024*1024)
		scanner.Buffer(buf, 10*1024*1024)

		for scanner.Scan() {
			var entry codexEntry
			if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
				continue
			}

			switch entry.Type {
			case "session_meta":
				if cwd, ok := entry.Payload["cwd"].(string); ok && info.CWD == "" {
					if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
						info.CWD = resolved
					} else {
						info.CWD = filepath.Clean(cwd)
					}
				}
				if id, ok := entry.Payload["id"].(string); ok && info.ID == "" {
					info.ID = id
				}
				if ts, ok := entry.Payload["timestamp"].(string); ok && info.SessionMetaTimestamp == "" {
					info.SessionMetaTimestamp = ts
				}
			case "turn_context":
				if cwd, ok := entry.Payload["cwd"].(string); ok && info.CWD == "" {
					if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
						info.CWD = resolved
					} else {
						info.CWD = filepath.Clean(cwd)
					}
				}
			}

			// Early exit once we have CWD and session metadata
			if info.CWD != "" && info.ID != "" {
				break
			}
		}

		info.UserMessageCount = 0
		return info, nil
	}

	// File has user messages - do full JSON parse to get exact count and first message
	scanner := bufio.NewScanner(bytes.NewReader(fileData))
	buf := make([]byte, 0, 1024*1024) // 1MB buffer
	scanner.Buffer(buf, 10*1024*1024) // Max 10MB per line

	for scanner.Scan() {
		var entry codexEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue // Skip malformed lines
		}

		switch entry.Type {
		case "session_meta":
			if cwd, ok := entry.Payload["cwd"].(string); ok && info.CWD == "" {
				// Resolve to real path
				if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
					info.CWD = resolved
				} else {
					info.CWD = filepath.Clean(cwd)
				}
			}
			if id, ok := entry.Payload["id"].(string); ok && info.ID == "" {
				info.ID = id
			}
			if ts, ok := entry.Payload["timestamp"].(string); ok && info.SessionMetaTimestamp == "" {
				info.SessionMetaTimestamp = ts
			}

		case "turn_context":
			if cwd, ok := entry.Payload["cwd"].(string); ok && info.CWD == "" {
				if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
					info.CWD = resolved
				} else {
					info.CWD = filepath.Clean(cwd)
				}
			}

		case "response_item":
			// Look for first user message
			if riType, ok := entry.Payload["type"].(string); ok && riType == "message" {
				if role, ok := entry.Payload["role"].(string); ok && role == "user" {
					if content, ok := entry.Payload["content"].([]interface{}); ok {
						text := c.extractUserText(content)
						trimmed := strings.TrimSpace(text)
						if trimmed == "" || c.isSessionPrefix(trimmed) {
							continue
						}

						info.UserMessageCount++

						if info.FirstUserMessage == "" {
							info.FirstUserMessage = c.extractFirstLine(text)
							info.FirstMessageTimestamp = entry.Timestamp
							if info.FirstMessageTimestamp == "" {
								info.FirstMessageTimestamp = info.SessionMetaTimestamp
							}
						}
					}
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error scanning rollout file: %w", err)
	}

	return info, nil
}

// CWDMatches checks if the session's CWD matches the target path.
func (info *sessionInfo) CWDMatches(targetPath string) bool {
	if info.CWD == "" {
		return false
	}
	return info.CWD == targetPath
}

// extractUserText extracts text from Codex content blocks.
func (c *CodexAdapter) extractUserText(content []interface{}) string {
	var parts []string
	for _, item := range content {
		if m, ok := item.(map[string]interface{}); ok {
			if itemType, ok := m["type"].(string); ok && itemType == "input_text" {
				if text, ok := m["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
	}
	return strings.Join(parts, "")
}

// isSessionPrefix checks if a message is a session prefix (user_instructions or environment_context).
// The text parameter is expected to already be trimmed.
func (c *CodexAdapter) isSessionPrefix(text string) bool {
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	return (strings.HasPrefix(lower, "<user_instructions>") && strings.HasSuffix(lower, "</user_instructions>")) ||
		(strings.HasPrefix(lower, "<environment_context>") && strings.HasSuffix(lower, "</environment_context>"))
}

// extractFirstLine extracts the first non-empty line from text.
func (c *CodexAdapter) extractFirstLine(text string) string {
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

// GetSession retrieves the full content of a Codex session with pagination.
func (c *CodexAdapter) GetSession(sessionID string, page, pageSize int) ([]Message, error) {
	// Find the session file by scanning all rollout files
	codexHome := filepath.Join(c.homeDir, ".codex")
	sessionDirs := []string{
		filepath.Join(codexHome, "sessions"),
		filepath.Join(codexHome, "archived_sessions"),
	}

	var sessionFile string
	for _, dir := range sessionDirs {
		files, err := c.findRolloutFiles(dir)
		if err != nil {
			continue
		}

		for _, file := range files {
			// Quick check: does this file contain the session ID?
			if info, err := c.scanRolloutFile(file, ""); err == nil && info.ID == sessionID {
				sessionFile = file
				break
			}
		}

		if sessionFile != "" {
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

// readAllMessages reads all messages from a Codex rollout file.
func (c *CodexAdapter) readAllMessages(filePath string) ([]Message, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open rollout file: %w", err)
	}
	defer file.Close()

	var messages []Message
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		var entry codexEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}

		if entry.Type != "response_item" {
			continue
		}

		if riType, ok := entry.Payload["type"].(string); ok && riType == "message" {
			if role, ok := entry.Payload["role"].(string); ok {
				message := Message{
					Role:     role,
					Metadata: make(map[string]interface{}),
				}

				// Parse timestamp
				if ts, err := parseCodexTimestamp(entry.Timestamp); err == nil {
					message.Timestamp = ts
				}

				// Extract content
				if content, ok := entry.Payload["content"].([]interface{}); ok {
					if role == "user" {
						message.Content = c.extractUserText(content)
					} else {
						// For assistant messages, extract all text parts
						message.Content = c.extractAllText(content)
						message.Metadata["raw_content"] = content
					}
				}

				// Skip session prefix messages
				if role == "user" && c.isSessionPrefix(strings.TrimSpace(message.Content)) {
					continue
				}

				messages = append(messages, message)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading rollout file: %w", err)
	}

	return messages, nil
}

// extractAllText extracts all text from content blocks (for assistant messages).
func (c *CodexAdapter) extractAllText(content []interface{}) string {
	var parts []string
	for _, item := range content {
		if m, ok := item.(map[string]interface{}); ok {
			if text, ok := m["text"].(string); ok {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

// SearchSessions searches Codex sessions for the given query.
func (c *CodexAdapter) SearchSessions(projectPath, query string, limit int) ([]Session, error) {
	// List all sessions first
	sessions, err := c.ListSessions(projectPath, 0)
	if err != nil {
		return nil, err
	}

	query = strings.ToLower(query)
	var matches []Session

	for _, session := range sessions {
		// Check if query is in first message
		if strings.Contains(strings.ToLower(session.FirstMessage), query) {
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

		// Apply limit
		if limit > 0 && len(matches) >= limit {
			break
		}
	}

	return matches, nil
}
