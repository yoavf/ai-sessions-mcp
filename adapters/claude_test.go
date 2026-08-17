package adapters

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClaudeAdapterParsesCurrentNestedMessageFormat(t *testing.T) {
	homeDir := t.TempDir()
	projectPath := filepath.Join(homeDir, "project")
	sessionDir := filepath.Join(homeDir, ".claude", "projects", projectDirName(projectPath))
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("failed to create session directory: %v", err)
	}

	sessionPath := filepath.Join(sessionDir, "session-123.jsonl")
	transcript := "" +
		`{"type":"user","cwd":` + quoteJSON(projectPath) + `,"isSidechain":false,"timestamp":"2026-08-15T12:00:00Z","message":{"role":"user","content":"Current Claude question?\nMore detail"}}` + "\n" +
		`{"type":"assistant","cwd":` + quoteJSON(projectPath) + `,"isSidechain":false,"timestamp":"2026-08-15T12:00:01Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"private reasoning"},{"type":"text","text":"Current Claude answer"}]}}` + "\n" +
		`{"type":"progress","cwd":` + quoteJSON(projectPath) + `,"data":{"type":"agent_progress"}}` + "\n"
	if err := os.WriteFile(sessionPath, []byte(transcript), 0o600); err != nil {
		t.Fatalf("failed to write transcript: %v", err)
	}

	adapter := &ClaudeAdapter{homeDir: homeDir}
	sessions, err := adapter.ListSessions(projectPath, 10)
	if err != nil {
		t.Fatalf("ListSessions returned error: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one session, got %d", len(sessions))
	}
	if sessions[0].ID != "session-123" || sessions[0].FirstMessage != "Current Claude question?" {
		t.Fatalf("unexpected session metadata: %+v", sessions[0])
	}

	messages, err := adapter.GetSession("session-123", 0, 20)
	if err != nil {
		t.Fatalf("GetSession returned error: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected user and assistant messages, got %d", len(messages))
	}
	if messages[0].Role != "user" || messages[0].Content != "Current Claude question?\nMore detail" {
		t.Fatalf("unexpected user message: %+v", messages[0])
	}
	if messages[1].Role != "assistant" || messages[1].Content != "Current Claude answer" {
		t.Fatalf("unexpected assistant message: %+v", messages[1])
	}
}
