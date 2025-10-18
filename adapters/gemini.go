package adapters

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// GeminiAdapter implements SessionAdapter for Gemini CLI sessions.
// Gemini stores sessions as JSON files in ~/.gemini/tmp/[PROJECT_HASH]/chats/
// where PROJECT_HASH is SHA256(absolute project path).
type GeminiAdapter struct {
	homeDir string
}

// NewGeminiAdapter creates a new Gemini CLI session adapter.
func NewGeminiAdapter() (*GeminiAdapter, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}
	return &GeminiAdapter{homeDir: homeDir}, nil
}

// Name returns the adapter name.
func (g *GeminiAdapter) Name() string {
	return "gemini"
}

// geminiSession represents the structure of a Gemini session JSON file.
type geminiSession struct {
	SessionID string          `json:"sessionId"`
	StartTime string          `json:"startTime,omitempty"`
	Messages  []geminiMessage `json:"messages"`
}

// geminiMessage represents a single message in a Gemini session.
type geminiMessage struct {
	Role      string      `json:"role"`
	Content   interface{} `json:"content"`
	Timestamp string      `json:"timestamp,omitempty"`
}

// hashProjectPath computes the SHA256 hash of the project path.
// This matches Gemini CLI's logic for determining the session directory.
func hashProjectPath(path string) string {
	hash := sha256.Sum256([]byte(path))
	return hex.EncodeToString(hash[:])
}

// ListSessions returns all Gemini sessions for the given project.
// If projectPath is empty, returns sessions from ALL projects.
func (g *GeminiAdapter) ListSessions(projectPath string, limit int) ([]Session, error) {
	geminiTmpDir := filepath.Join(g.homeDir, ".gemini", "tmp")

	// If no project path specified, list sessions from ALL projects
	if projectPath == "" {
		return g.listAllSessions(geminiTmpDir, limit)
	}

	// Get absolute path
	projectPath, err := filepath.Abs(projectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Compute project hash
	projectHash := hashProjectPath(projectPath)
	chatsDir := filepath.Join(geminiTmpDir, projectHash, "chats")

	// Check if directory exists
	if _, err := os.Stat(chatsDir); os.IsNotExist(err) {
		return []Session{}, nil // No sessions for this project
	}

	// Read all session-*.json files
	files, err := filepath.Glob(filepath.Join(chatsDir, "session-*.json"))
	if err != nil {
		return nil, fmt.Errorf("failed to list session files: %w", err)
	}

	sessions := make([]Session, 0, len(files))
	for _, filePath := range files {
		session, err := g.parseSessionMetadata(filePath, projectPath)
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
func (g *GeminiAdapter) listAllSessions(geminiTmpDir string, limit int) ([]Session, error) {
	// Check if tmp directory exists
	if _, err := os.Stat(geminiTmpDir); os.IsNotExist(err) {
		return []Session{}, nil
	}

	// Read all project hash directories
	hashDirs, err := os.ReadDir(geminiTmpDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read Gemini tmp directory: %w", err)
	}

	var allSessions []Session
	for _, dir := range hashDirs {
		if !dir.IsDir() {
			continue
		}

		chatsDir := filepath.Join(geminiTmpDir, dir.Name(), "chats")
		files, err := filepath.Glob(filepath.Join(chatsDir, "session-*.json"))
		if err != nil {
			continue
		}

		for _, filePath := range files {
			// We don't know the original project path, use hash as identifier
			session, err := g.parseSessionMetadata(filePath, "unknown-project-"+dir.Name())
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

// parseSessionMetadata extracts metadata from a Gemini session file.
func (g *GeminiAdapter) parseSessionMetadata(filePath, projectPath string) (Session, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return Session{}, fmt.Errorf("failed to read session file: %w", err)
	}

	var geminiSess geminiSession
	if err := json.Unmarshal(data, &geminiSess); err != nil {
		return Session{}, fmt.Errorf("failed to parse session JSON: %w", err)
	}

	session := Session{
		ID:          geminiSess.SessionID,
		Source:      "gemini",
		ProjectPath: projectPath,
		FilePath:    filePath,
	}

	// Parse timestamp from first message or startTime
	if len(geminiSess.Messages) > 0 && geminiSess.Messages[0].Timestamp != "" {
		if ts, err := time.Parse(time.RFC3339, geminiSess.Messages[0].Timestamp); err == nil {
			session.Timestamp = ts
		}
	} else if geminiSess.StartTime != "" {
		if ts, err := time.Parse(time.RFC3339, geminiSess.StartTime); err == nil {
			session.Timestamp = ts
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
	for _, msg := range geminiSess.Messages {
		if msg.Role != "user" {
			continue
		}
		userCount++
		if session.FirstMessage == "" {
			session.FirstMessage = extractFirstLineFromContent(msg.Content)
		}
	}

	session.UserMessageCount = userCount

	return session, nil
}

// extractFirstLineFromContent extracts the first line from various content formats.
func extractFirstLineFromContent(content interface{}) string {
	switch v := content.(type) {
	case string:
		lines := strings.Split(v, "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				if len(trimmed) > 200 {
					return trimmed[:200] + "..."
				}
				return trimmed
			}
		}
	case []interface{}:
		// Gemini may use structured content with text fields
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				if text, ok := m["text"].(string); ok {
					return extractFirstLineFromContent(text)
				}
			}
		}
	case map[string]interface{}:
		if text, ok := v["text"].(string); ok {
			return extractFirstLineFromContent(text)
		}
	}
	return ""
}

// GetSession retrieves the full content of a Gemini session with pagination.
func (g *GeminiAdapter) GetSession(sessionID string, page, pageSize int) ([]Message, error) {
	// We need to search for the session file since we don't know the project path
	geminiTmpDir := filepath.Join(g.homeDir, ".gemini", "tmp")

	// Read all project hash directories
	projectDirs, err := os.ReadDir(geminiTmpDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read Gemini tmp directory: %w", err)
	}

	var sessionFile string
	for _, dir := range projectDirs {
		if !dir.IsDir() {
			continue
		}

		// Check for matching session file
		chatsDir := filepath.Join(geminiTmpDir, dir.Name(), "chats")
		files, err := filepath.Glob(filepath.Join(chatsDir, "session-*.json"))
		if err != nil {
			continue
		}

		for _, file := range files {
			// Read and check if this is the right session
			data, err := os.ReadFile(file)
			if err != nil {
				continue
			}

			var sess geminiSession
			if err := json.Unmarshal(data, &sess); err != nil {
				continue
			}

			if sess.SessionID == sessionID {
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

	// Read the session file
	messages, err := g.readAllMessages(sessionFile)
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

// readAllMessages reads all messages from a Gemini session file.
func (g *GeminiAdapter) readAllMessages(filePath string) ([]Message, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read session file: %w", err)
	}

	var sess geminiSession
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, fmt.Errorf("failed to parse session JSON: %w", err)
	}

	messages := make([]Message, 0, len(sess.Messages))
	for _, msg := range sess.Messages {
		message := Message{
			Role:     msg.Role,
			Content:  contentToStringGemini(msg.Content),
			Metadata: make(map[string]interface{}),
		}

		// Parse timestamp if available
		if msg.Timestamp != "" {
			if ts, err := time.Parse(time.RFC3339, msg.Timestamp); err == nil {
				message.Timestamp = ts
			}
		}

		messages = append(messages, message)
	}

	return messages, nil
}

// contentToStringGemini converts Gemini content to a string.
func contentToStringGemini(content interface{}) string {
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
		// Fallback to JSON
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
	}
	return fmt.Sprintf("%v", content)
}

// SearchSessions searches Gemini sessions for the given query.
func (g *GeminiAdapter) SearchSessions(projectPath, query string, limit int) ([]Session, error) {
	// First, list all sessions
	sessions, err := g.ListSessions(projectPath, 0)
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
		messages, err := g.readAllMessages(session.FilePath)
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
