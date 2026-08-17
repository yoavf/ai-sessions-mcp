package adapters

import (
	"bufio"
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
	homeDir      string
	projectCache map[string]string
}

// NewGeminiAdapter creates a new Gemini CLI session adapter.
func NewGeminiAdapter() (*GeminiAdapter, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}
	return &GeminiAdapter{
		homeDir:      homeDir,
		projectCache: make(map[string]string),
	}, nil
}

// Name returns the adapter name.
func (g *GeminiAdapter) Name() string {
	return "gemini"
}

// geminiSession represents the structure of a Gemini session JSON file.
type geminiSession struct {
	SessionID   string          `json:"sessionId"`
	ProjectHash string          `json:"projectHash,omitempty"`
	StartTime   string          `json:"startTime,omitempty"`
	LastUpdated string          `json:"lastUpdated,omitempty"`
	Summary     string          `json:"summary,omitempty"`
	Messages    []geminiMessage `json:"messages"`
}

// geminiMessage represents a single message in a Gemini session.
type geminiMessage struct {
	ID        string           `json:"id,omitempty"`
	Role      string           `json:"role,omitempty"`
	Type      string           `json:"type,omitempty"`
	Content   interface{}      `json:"content"`
	Timestamp string           `json:"timestamp,omitempty"`
	ToolCalls []geminiToolCall `json:"toolCalls,omitempty"`
}

type geminiToolCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args,omitempty"`
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

	// Read current JSONL recordings and legacy JSON recordings.
	files, err := geminiSessionFiles(chatsDir)
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
		files, err := geminiSessionFiles(chatsDir)
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

func geminiSessionFiles(chatsDir string) ([]string, error) {
	patterns := []string{
		filepath.Join(chatsDir, "session-*.jsonl"),
		filepath.Join(chatsDir, "session-*.json"),
	}
	var files []string
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		files = append(files, matches...)
	}
	return files, nil
}

// parseSessionMetadata extracts metadata from a Gemini session file.
func (g *GeminiAdapter) parseSessionMetadata(filePath, projectPath string) (Session, error) {
	geminiSess, err := loadGeminiSession(filePath)
	if err != nil {
		return Session{}, err
	}

	hashDir := extractHashFromPath(filePath)
	resolvedProjectPath := g.resolveProjectPath(hashDir, projectPath, geminiSess)

	session := Session{
		ID:          geminiSess.SessionID,
		Source:      "gemini",
		ProjectPath: resolvedProjectPath,
		FilePath:    filePath,
		Summary:     geminiSess.Summary,
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
		role := normalizeGeminiRole(msg)
		if role != "user" {
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

// loadGeminiSession supports both legacy whole-document JSON recordings and
// current append-only JSONL recordings. JSONL files may contain metadata
// updates, message replacements, and rewind markers.
func loadGeminiSession(filePath string) (*geminiSession, error) {
	if filepath.Ext(filePath) == ".json" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read session file: %w", err)
		}
		var session geminiSession
		if err := json.Unmarshal(data, &session); err != nil {
			return nil, fmt.Errorf("failed to parse session JSON: %w", err)
		}
		return &session, nil
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read session file: %w", err)
	}
	defer file.Close()

	session := &geminiSession{}
	messageIndex := make(map[string]int)
	rebuildIndex := func() {
		clear(messageIndex)
		for i, message := range session.Messages {
			if message.ID != "" {
				messageIndex[message.ID] = i
			}
		}
	}
	applyMetadata := func(raw map[string]json.RawMessage) {
		if value, ok := raw["sessionId"]; ok {
			_ = json.Unmarshal(value, &session.SessionID)
		}
		if value, ok := raw["projectHash"]; ok {
			_ = json.Unmarshal(value, &session.ProjectHash)
		}
		if value, ok := raw["startTime"]; ok {
			_ = json.Unmarshal(value, &session.StartTime)
		}
		if value, ok := raw["lastUpdated"]; ok {
			_ = json.Unmarshal(value, &session.LastUpdated)
		}
		if value, ok := raw["summary"]; ok {
			_ = json.Unmarshal(value, &session.Summary)
		}
		if value, ok := raw["messages"]; ok {
			var messages []geminiMessage
			if json.Unmarshal(value, &messages) == nil {
				session.Messages = messages
				rebuildIndex()
			}
		}
	}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		var record map[string]json.RawMessage
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			continue
		}

		if value, ok := record["$rewindTo"]; ok {
			var messageID string
			if json.Unmarshal(value, &messageID) == nil {
				if index, found := messageIndex[messageID]; found {
					session.Messages = session.Messages[:index]
					rebuildIndex()
				} else {
					session.Messages = nil
					clear(messageIndex)
				}
			}
			continue
		}

		if value, ok := record["$set"]; ok {
			var update map[string]json.RawMessage
			if json.Unmarshal(value, &update) == nil {
				applyMetadata(update)
			}
			continue
		}

		if _, hasID := record["id"]; hasID {
			if _, hasType := record["type"]; hasType {
				var message geminiMessage
				if json.Unmarshal(scanner.Bytes(), &message) == nil {
					if index, found := messageIndex[message.ID]; found {
						session.Messages[index] = message
					} else {
						messageIndex[message.ID] = len(session.Messages)
						session.Messages = append(session.Messages, message)
					}
				}
				continue
			}
		}

		applyMetadata(record)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to parse session JSONL: %w", err)
	}
	if session.SessionID == "" {
		return nil, fmt.Errorf("failed to parse session JSONL: missing sessionId")
	}
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
		files, err := geminiSessionFiles(chatsDir)
		if err != nil {
			continue
		}

		for _, file := range files {
			// Read and check if this is the right session
			sess, err := loadGeminiSession(file)
			if err != nil {
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
	sess, err := loadGeminiSession(filePath)
	if err != nil {
		return nil, err
	}

	messages := make([]Message, 0, len(sess.Messages))
	for _, msg := range sess.Messages {
		role := normalizeGeminiRole(msg)

		message := Message{
			Role:     role,
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

func normalizeGeminiRole(msg geminiMessage) string {
	role := strings.TrimSpace(msg.Role)
	if role == "" {
		role = strings.TrimSpace(msg.Type)
	}
	role = strings.ToLower(role)

	switch role {
	case "":
		return ""
	case "user":
		return "user"
	case "assistant", "model", "gemini":
		return "assistant"
	case "system":
		return "system"
	case "tool":
		return "tool"
	default:
		return role
	}
}

func extractHashFromPath(filePath string) string {
	chatsDir := filepath.Dir(filePath)
	hashDir := filepath.Base(filepath.Dir(chatsDir))
	return hashDir
}

func (g *GeminiAdapter) resolveProjectPath(hash, provided string, sess *geminiSession) string {
	if provided != "" && !strings.HasPrefix(provided, "unknown-project-") {
		g.projectCache[hash] = provided
		return provided
	}

	if path, ok := g.projectCache[hash]; ok && path != "" {
		return path
	}

	if sess != nil {
		if inferred := inferProjectPathFromSession(hash, sess); inferred != "" {
			g.projectCache[hash] = inferred
			return inferred
		}
	}

	if provided != "" {
		g.projectCache[hash] = provided
	}
	return provided
}

func inferProjectPathFromSession(hash string, sess *geminiSession) string {
	var candidates []string

	for _, msg := range sess.Messages {
		// Collect content-derived paths
		if content := contentToStringGemini(msg.Content); content != "" {
			candidates = append(candidates, extractPathsFromText(content)...)
		}

		// Collect tool call argument paths
		for _, call := range msg.ToolCalls {
			for _, key := range []string{"path", "file_path", "filePath", "directory", "cwd", "root"} {
				if val, ok := call.Args[key]; ok {
					if str, ok := val.(string); ok {
						candidates = append(candidates, str)
					}
				}
			}
		}
	}

	for _, candidate := range candidates {
		clean := normalizeCandidatePath(candidate)
		if clean == "" || !isLikelyAbsolutePath(clean) {
			continue
		}

		current := clean
		for {
			if hashProjectPath(current) == hash {
				return current
			}
			parent := filepath.Dir(current)
			if parent == current || parent == "." {
				break
			}
			current = parent
		}
	}

	return ""
}

func extractPathsFromText(text string) []string {
	fields := strings.FieldsFunc(text, func(r rune) bool {
		switch r {
		case ' ', '\n', '\r', '\t', '"', '\'', '`', '<', '>', '(', ')', '[', ']', '{', '}', ',', ';':
			return true
		default:
			return false
		}
	})

	results := make([]string, 0, len(fields))
	for _, field := range fields {
		if field == "" {
			continue
		}
		results = append(results, field)
	}
	return results
}

func normalizeCandidatePath(path string) string {
	if path == "" {
		return ""
	}
	path = strings.TrimSpace(path)
	path = strings.Trim(path, `"'`)
	path = strings.Trim(path, "`")
	path = strings.Trim(path, "[](){}<>")
	path = strings.TrimRight(path, ":,.")
	return path
}

func isLikelyAbsolutePath(path string) bool {
	if filepath.IsAbs(path) {
		return true
	}

	// Windows drive letter (e.g., C:\ or C:/)
	if len(path) >= 3 && path[1] == ':' && (path[2] == '\\' || path[2] == '/') && isASCIIAlpha(path[0]) {
		return true
	}

	// UNC path (e.g., \\server\share)
	if strings.HasPrefix(path, `\\`) && len(path) > 2 {
		return true
	}

	return false
}

func isASCIIAlpha(b byte) bool {
	return ('A' <= b && b <= 'Z') || ('a' <= b && b <= 'z')
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
