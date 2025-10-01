package adapters

import (
	"fmt"
)

// CursorAdapter is a placeholder for Cursor CLI sessions.
//
// Cursor stores sessions in SQLite databases with binary/compressed blobs:
// ~/.cursor/chats/[PROJECT_HASH]/[SESSION_ID]/store.db
//
// Structure:
// - meta table: hex-encoded JSON metadata (agentId, name, createdAt, etc.)
// - blobs table: hash-addressed binary/compressed conversation data
//
// Implementation blocked on understanding Cursor's compression/serialization format.
type CursorAdapter struct{}

// NewCursorAdapter creates a new Cursor CLI session adapter.
func NewCursorAdapter() (*CursorAdapter, error) {
	return nil, fmt.Errorf("cursor adapter not yet implemented")
}

// Name returns the adapter name.
func (c *CursorAdapter) Name() string {
	return "cursor"
}

func (c *CursorAdapter) ListSessions(projectPath string, limit int) ([]Session, error) {
	return nil, fmt.Errorf("cursor adapter not yet implemented")
}

func (c *CursorAdapter) GetSession(sessionID string, page, pageSize int) ([]Message, error) {
	return nil, fmt.Errorf("cursor adapter not yet implemented")
}

func (c *CursorAdapter) SearchSessions(projectPath, query string, limit int) ([]Session, error) {
	return nil, fmt.Errorf("cursor adapter not yet implemented")
}
