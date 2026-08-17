package adapters

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestOpencodeAdapterReadsSQLiteSessions(t *testing.T) {
	home := t.TempDir()
	dataDir := filepath.Join(home, ".local", "share", "opencode")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(dataDir, "opencode.db")
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	schema := []string{
		`CREATE TABLE project (id TEXT PRIMARY KEY, worktree TEXT NOT NULL)`,
		`CREATE TABLE session (id TEXT PRIMARY KEY, project_id TEXT NOT NULL, directory TEXT NOT NULL, title TEXT NOT NULL, time_created INTEGER NOT NULL)`,
		`CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, time_created INTEGER NOT NULL, data TEXT NOT NULL)`,
		`CREATE TABLE part (id TEXT PRIMARY KEY, message_id TEXT NOT NULL, session_id TEXT NOT NULL, time_created INTEGER NOT NULL, data TEXT NOT NULL)`,
		`INSERT INTO project VALUES ('project-1', '/worktree')`,
		`INSERT INTO session VALUES ('session-1', 'project-1', '/worktree', 'SQLite session', 1786442400000)`,
		`INSERT INTO message VALUES ('message-1', 'session-1', 1786442401000, '{"role":"user"}')`,
		`INSERT INTO part VALUES ('part-1', 'message-1', 'session-1', 1786442401000, '{"type":"text","text":"Current database prompt"}')`,
		`INSERT INTO message VALUES ('message-2', 'session-1', 1786442402000, '{"role":"assistant","modelID":"test-model"}')`,
		`INSERT INTO part VALUES ('part-2', 'message-2', 'session-1', 1786442402000, '{"type":"text","text":"Current database answer"}')`,
	}
	for _, statement := range schema {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("failed SQL %q: %v", statement, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	adapter := &OpencodeAdapter{homeDir: home}
	sessions, err := adapter.ListSessions("/worktree", 0)
	if err != nil {
		t.Fatalf("ListSessions returned error: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != "session-1" || sessions[0].FirstMessage != "Current database prompt" {
		t.Fatalf("unexpected sessions: %+v", sessions)
	}

	messages, err := adapter.GetSession("session-1", 0, 10)
	if err != nil {
		t.Fatalf("GetSession returned error: %v", err)
	}
	if len(messages) != 2 || messages[1].Content != "Current database answer" || messages[1].Metadata["model"] != "test-model" {
		t.Fatalf("unexpected messages: %+v", messages)
	}

	matches, err := adapter.SearchSessions("/worktree", "database answer", 0)
	if err != nil {
		t.Fatalf("SearchSessions returned error: %v", err)
	}
	if len(matches) != 1 || matches[0].ID != "session-1" {
		t.Fatalf("unexpected search results: %+v", matches)
	}
}
