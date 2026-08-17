package adapters

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
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
	ID        string                 `json:"id"`
	Role      string                 `json:"role"`
	System    interface{}            `json:"system,omitempty"` // Can be string or array
	Mode      string                 `json:"mode,omitempty"`
	Content   interface{}            `json:"content,omitempty"`
	Cost      float64                `json:"cost,omitempty"`
	Tokens    map[string]interface{} `json:"tokens,omitempty"`
	ModelID   string                 `json:"modelID,omitempty"`
	Time      map[string]interface{} `json:"time,omitempty"`
	SessionID string                 `json:"sessionID,omitempty"`
}

// ListSessions returns all opencode sessions for the given project.
// If projectPath is empty, returns sessions from ALL projects.
func (o *OpencodeAdapter) ListSessions(projectPath string, limit int) ([]Session, error) {
	databaseSessions, err := o.listDatabaseSessions(projectPath)
	if err != nil {
		return nil, err
	}
	legacySessions, err := o.listLegacySessions(projectPath)
	if err != nil {
		return nil, err
	}

	// Prefer current SQLite records when an ID also remains in the legacy JSON
	// migration tree, while retaining older sessions that were not migrated.
	seen := make(map[string]struct{}, len(databaseSessions))
	allSessions := make([]Session, 0, len(databaseSessions)+len(legacySessions))
	for _, session := range databaseSessions {
		seen[session.ID] = struct{}{}
		allSessions = append(allSessions, session)
	}
	for _, session := range legacySessions {
		if _, exists := seen[session.ID]; exists {
			continue
		}
		allSessions = append(allSessions, session)
	}

	sort.Slice(allSessions, func(i, j int) bool {
		return allSessions[i].Timestamp.After(allSessions[j].Timestamp)
	})
	if limit > 0 && len(allSessions) > limit {
		allSessions = allSessions[:limit]
	}
	return allSessions, nil
}

func (o *OpencodeAdapter) listLegacySessions(projectPath string) ([]Session, error) {
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

	return allSessions, nil
}

func (o *OpencodeAdapter) databasePath() string {
	return filepath.Join(o.homeDir, ".local", "share", "opencode", "opencode.db")
}

func (o *OpencodeAdapter) openDatabase() (*sql.DB, error) {
	databasePath := o.databasePath()
	if _, err := os.Stat(databasePath); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to inspect opencode database: %w", err)
	}
	uri := (&url.URL{Scheme: "file", Path: databasePath, RawQuery: "mode=ro"}).String()
	db, err := sql.Open("sqlite", uri)
	if err != nil {
		return nil, fmt.Errorf("failed to open opencode database: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to open opencode database: %w", err)
	}
	return db, nil
}

func (o *OpencodeAdapter) listDatabaseSessions(projectPath string) ([]Session, error) {
	db, err := o.openDatabase()
	if err != nil || db == nil {
		return nil, err
	}
	defer db.Close()

	query := `
		SELECT s.id, s.directory, s.title, s.time_created, COALESCE(p.worktree, '')
		FROM session s
		LEFT JOIN project p ON p.id = s.project_id`
	var args []interface{}
	if projectPath != "" {
		absolutePath, err := filepath.Abs(projectPath)
		if err != nil {
			return nil, fmt.Errorf("failed to get absolute path: %w", err)
		}
		query += " WHERE s.directory = ? OR p.worktree = ?"
		args = append(args, absolutePath, absolutePath)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query opencode sessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var id, directory, title, worktree string
		var created int64
		if err := rows.Scan(&id, &directory, &title, &created, &worktree); err != nil {
			return nil, fmt.Errorf("failed to read opencode session: %w", err)
		}
		firstMessage, userCount, err := databaseFirstUserMessageAndCount(db, id)
		if err != nil {
			return nil, err
		}
		if directory == "" {
			directory = worktree
		}
		sessions = append(sessions, Session{
			ID:               id,
			Source:           "opencode",
			ProjectPath:      directory,
			FirstMessage:     extractFirstLine(firstMessage),
			Summary:          title,
			Timestamp:        time.UnixMilli(created),
			FilePath:         o.databasePath(),
			UserMessageCount: userCount,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to query opencode sessions: %w", err)
	}
	return sessions, nil
}

func databaseFirstUserMessageAndCount(db *sql.DB, sessionID string) (string, int, error) {
	var count int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM message
		WHERE session_id = ? AND json_extract(data, '$.role') = 'user'`, sessionID).Scan(&count); err != nil {
		return "", 0, fmt.Errorf("failed to count opencode messages: %w", err)
	}

	var first sql.NullString
	err := db.QueryRow(`
		SELECT json_extract(p.data, '$.text')
		FROM message m
		JOIN part p ON p.message_id = m.id
		WHERE m.session_id = ?
		  AND json_extract(m.data, '$.role') = 'user'
		  AND json_extract(p.data, '$.type') = 'text'
		ORDER BY m.time_created, m.id, p.time_created, p.id
		LIMIT 1`, sessionID).Scan(&first)
	if err != nil && err != sql.ErrNoRows {
		return "", 0, fmt.Errorf("failed to read opencode message: %w", err)
	}
	return first.String, count, nil
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
		firstMessage, userCount, err := o.getFirstUserMessageAndCount(storageDir, sess.ID)
		if err != nil {
			firstMessage = "" // Continue even if we can't get first message
			userCount = 0
		}

		session := Session{
			ID:               sess.ID,
			Source:           "opencode",
			ProjectPath:      worktree,
			FirstMessage:     firstMessage,
			Summary:          sess.Title,
			Timestamp:        time.UnixMilli(sess.Time.Created),
			FilePath:         file,
			UserMessageCount: userCount,
		}

		sessions = append(sessions, session)
	}

	return sessions, nil
}

// getFirstUserMessageAndCount extracts the first user message from a session and counts all user messages.
func (o *OpencodeAdapter) getFirstUserMessageAndCount(storageDir, sessionID string) (string, int, error) {
	messageDir := filepath.Join(storageDir, "message", sessionID)
	files, err := filepath.Glob(filepath.Join(messageDir, "msg_*.json"))
	if err != nil {
		return "", 0, err
	}

	// Sort by filename (contains timestamp-like component)
	sort.Strings(files)

	firstMessage := ""
	userCount := 0

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
				userCount++
				if firstMessage == "" {
					firstMessage = o.extractFirstLine(content)
				}
			}
		}
	}

	return firstMessage, userCount, nil
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
	messages, err := o.readSessionMessages(sessionID)
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

func (o *OpencodeAdapter) readSessionMessages(sessionID string) ([]Message, error) {
	if db, err := o.openDatabase(); err != nil {
		return nil, err
	} else if db != nil {
		messages, found, err := readDatabaseMessages(db, sessionID)
		db.Close()
		if err != nil {
			return nil, err
		}
		if found {
			return messages, nil
		}
	}
	return o.readLegacySessionMessages(sessionID)
}

func (o *OpencodeAdapter) readLegacySessionMessages(sessionID string) ([]Message, error) {
	messageDir := filepath.Join(o.homeDir, ".local", "share", "opencode", "storage", "message", sessionID)
	if _, err := os.Stat(messageDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	return o.readAllMessages(messageDir)
}

func readDatabaseMessages(db *sql.DB, sessionID string) ([]Message, bool, error) {
	var exists int
	if err := db.QueryRow("SELECT COUNT(*) FROM session WHERE id = ?", sessionID).Scan(&exists); err != nil {
		return nil, false, fmt.Errorf("failed to find opencode session: %w", err)
	}
	if exists == 0 {
		return nil, false, nil
	}

	rows, err := db.Query(`
		SELECT id, time_created, data
		FROM message
		WHERE session_id = ?
		ORDER BY time_created, id`, sessionID)
	if err != nil {
		return nil, true, fmt.Errorf("failed to query opencode messages: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var id, rawData string
		var created int64
		if err := rows.Scan(&id, &created, &rawData); err != nil {
			return nil, true, fmt.Errorf("failed to read opencode message: %w", err)
		}
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(rawData), &data); err != nil {
			continue
		}

		message := Message{
			Role:      stringValue(data["role"]),
			Timestamp: time.UnixMilli(created),
			Metadata:  make(map[string]interface{}),
		}
		metadataKeys := map[string]string{
			"modelID":    "model",
			"providerID": "provider",
			"mode":       "mode",
			"agent":      "agent",
			"cost":       "cost",
			"tokens":     "tokens",
		}
		for source, target := range metadataKeys {
			if value, ok := data[source]; ok {
				message.Metadata[target] = value
			}
		}

		partRows, err := db.Query(`
			SELECT data FROM part
			WHERE message_id = ?
			ORDER BY time_created, id`, id)
		if err != nil {
			return nil, true, fmt.Errorf("failed to query opencode message parts: %w", err)
		}
		var textParts []string
		var nonTextParts []interface{}
		for partRows.Next() {
			var rawPart string
			if err := partRows.Scan(&rawPart); err != nil {
				partRows.Close()
				return nil, true, fmt.Errorf("failed to read opencode message part: %w", err)
			}
			var part map[string]interface{}
			if json.Unmarshal([]byte(rawPart), &part) != nil {
				continue
			}
			if part["type"] == "text" {
				if text := stringValue(part["text"]); text != "" {
					textParts = append(textParts, text)
				}
			} else {
				nonTextParts = append(nonTextParts, part)
			}
		}
		if err := partRows.Err(); err != nil {
			partRows.Close()
			return nil, true, fmt.Errorf("failed to read opencode message parts: %w", err)
		}
		partRows.Close()
		message.Content = strings.Join(textParts, "\n")
		if message.Content == "" {
			message.Content = contentValue(data["content"])
		}
		if len(nonTextParts) > 0 {
			message.Metadata["parts"] = nonTextParts
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, true, fmt.Errorf("failed to query opencode messages: %w", err)
	}
	return messages, true, nil
}

func stringValue(value interface{}) string {
	text, _ := value.(string)
	return text
}

func contentValue(value interface{}) string {
	switch content := value.(type) {
	case string:
		return content
	case nil:
		return ""
	default:
		encoded, err := json.Marshal(content)
		if err != nil {
			return ""
		}
		return string(encoded)
	}
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
	db, err := o.openDatabase()
	if err != nil {
		return nil, err
	}
	if db != nil {
		defer db.Close()
	}

	// Search through each session
	for _, session := range sessions {
		// Check if query is in title or first message
		if strings.Contains(strings.ToLower(session.Summary), query) ||
			strings.Contains(strings.ToLower(session.FirstMessage), query) {
			matches = append(matches, session)
			continue
		}

		var messages []Message
		foundInDatabase := false
		if db != nil {
			messages, foundInDatabase, err = readDatabaseMessages(db, session.ID)
		}
		if err == nil && !foundInDatabase {
			messages, err = o.readLegacySessionMessages(session.ID)
		}
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
