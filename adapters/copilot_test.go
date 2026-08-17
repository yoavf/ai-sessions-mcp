package adapters

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopilotAdapterReadsNestedSessionLayout(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(home, "project")
	sessionID := "current-session"
	sessionDir := filepath.Join(home, ".copilot", "session-state", sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}

	events := `{"type":"session.start","data":{"sessionId":"current-session","startTime":"2026-08-14T12:00:00Z","context":{"cwd":"` + project + `","gitRoot":"` + project + `"}}}
{"type":"user.message","timestamp":"2026-08-14T12:00:01Z","data":{"content":"Find the compatibility bug"}}
{"type":"assistant.message","timestamp":"2026-08-14T12:00:02Z","data":{"content":"Found it"}}
`
	eventsPath := filepath.Join(sessionDir, "events.jsonl")
	if err := os.WriteFile(eventsPath, []byte(events), 0o600); err != nil {
		t.Fatal(err)
	}

	adapter := &CopilotAdapter{homeDir: home}
	sessions, err := adapter.ListSessions(project, 0)
	if err != nil {
		t.Fatalf("ListSessions returned error: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one session, got %d", len(sessions))
	}
	if sessions[0].ID != sessionID || sessions[0].ProjectPath != project {
		t.Fatalf("unexpected session metadata: %+v", sessions[0])
	}

	messages, err := adapter.GetSession(sessionID, 0, 10)
	if err != nil {
		t.Fatalf("GetSession returned error: %v", err)
	}
	if len(messages) != 2 || messages[0].Content != "Find the compatibility bug" {
		t.Fatalf("unexpected messages: %+v", messages)
	}

	matches, err := adapter.SearchSessions(project, "found it", 0)
	if err != nil {
		t.Fatalf("SearchSessions returned error: %v", err)
	}
	if len(matches) != 1 || matches[0].ID != sessionID {
		t.Fatalf("unexpected search results: %+v", matches)
	}
}

func TestCopilotAdapterKeepsLegacyFlatLayout(t *testing.T) {
	home := t.TempDir()
	sessionsDir := filepath.Join(home, ".copilot", "session-state")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionsDir, "legacy.jsonl"), []byte(
		"{\"type\":\"user.message\",\"data\":{\"content\":\"legacy\"}}\n",
	), 0o600); err != nil {
		t.Fatal(err)
	}

	adapter := &CopilotAdapter{homeDir: home}
	sessions, err := adapter.ListSessions("", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "legacy" {
		t.Fatalf("unexpected legacy sessions: %+v", sessions)
	}
}
