package adapters

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestCopilotSearchLimitReturnsNewestMatch(t *testing.T) {
	home := t.TempDir()
	sessionsDir := filepath.Join(home, ".copilot", "session-state")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeSession := func(name, sessionID, startTime string) {
		t.Helper()
		contents := `{"type":"session.start","data":{"sessionId":"` + sessionID + `","startTime":"` + startTime + `"}}` + "\n" +
			`{"type":"user.message","data":{"content":"matching prompt"}}` + "\n"
		if err := os.WriteFile(filepath.Join(sessionsDir, name+".jsonl"), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// Lexical file order is intentionally the reverse of session recency.
	writeSession("a-older", "older", "2026-08-16T10:00:00Z")
	writeSession("z-newer", "newer", "2026-08-17T10:00:00Z")

	adapter := &CopilotAdapter{homeDir: home}
	matches, err := adapter.SearchSessions("", "matching", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].ID != "newer" {
		t.Fatalf("expected newest matching session, got %+v", matches)
	}
	wantTimestamp := time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)
	if !matches[0].Timestamp.Equal(wantTimestamp) {
		t.Fatalf("timestamp = %v, want %v", matches[0].Timestamp, wantTimestamp)
	}
}
