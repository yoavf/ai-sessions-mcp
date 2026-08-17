package adapters

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCodexAdapterParsesCurrentRolloutFormat(t *testing.T) {
	homeDir := t.TempDir()
	projectPath := filepath.Join(homeDir, "project")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	sessionDir := filepath.Join(homeDir, ".codex", "sessions", "2026", "08", "15")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("failed to create session directory: %v", err)
	}
	sessionPath := filepath.Join(sessionDir, "rollout-2026-08-15T12-00-00-session-123.jsonl")
	rollout := "" +
		`{"timestamp":"2026-08-15T12:00:00.123Z","type":"session_meta","payload":{"id":"session-123","timestamp":"2026-08-15T12:00:00.123Z","cwd":` + quoteJSON(projectPath) + `,"cli_version":"0.146.0"}}` + "\n" +
		`{"timestamp":"2026-08-15T12:00:01Z","type":"turn_context","payload":{"cwd":` + quoteJSON(projectPath) + `}}` + "\n" +
		`{"timestamp":"2026-08-15T12:00:02Z","type":"response_item","payload":{"type":"message","role":"developer","content":[{"type":"input_text","text":"developer instructions"}]}}` + "\n" +
		`{"timestamp":"2026-08-15T12:00:02.500Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<recommended_plugins>plugin context</recommended_plugins>"},{"type":"input_text","text":"<environment_context>environment context</environment_context>"}]}}` + "\n" +
		`{"timestamp":"2026-08-15T12:00:02.750Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<turn_aborted reason=\"interrupted\"/>"}]}}` + "\n" +
		`{"timestamp":"2026-08-15T12:00:03Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Current Codex question?\nMore detail"}]}}` + "\n" +
		`{"timestamp":"2026-08-15T12:00:04Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Current Codex answer"}]}}` + "\n"
	if err := os.WriteFile(sessionPath, []byte(rollout), 0o600); err != nil {
		t.Fatalf("failed to write rollout: %v", err)
	}

	adapter := &CodexAdapter{homeDir: homeDir}
	sessions, err := adapter.ListSessions(projectPath, 10)
	if err != nil {
		t.Fatalf("ListSessions returned error: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one session, got %d", len(sessions))
	}
	if sessions[0].ID != "session-123" || sessions[0].FirstMessage != "Current Codex question?" {
		t.Fatalf("unexpected session metadata: %+v", sessions[0])
	}

	messages, err := adapter.GetSession("session-123", 0, 20)
	if err != nil {
		t.Fatalf("GetSession returned error: %v", err)
	}
	if len(messages) != 3 {
		t.Fatalf("expected developer, user, and assistant messages, got %d", len(messages))
	}
	if messages[1].Role != "user" || messages[1].Content != "Current Codex question?\nMore detail" {
		t.Fatalf("unexpected user message: %+v", messages[1])
	}
	if messages[2].Role != "assistant" || messages[2].Content != "Current Codex answer" {
		t.Fatalf("unexpected assistant message: %+v", messages[2])
	}
}

func TestCodexAdapterPreservesPromptAfterInjectedContext(t *testing.T) {
	homeDir := t.TempDir()
	projectPath := filepath.Join(homeDir, "project")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	sessionDir := filepath.Join(homeDir, ".codex", "sessions", "2026", "08", "15")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("failed to create session directory: %v", err)
	}
	sessionPath := filepath.Join(sessionDir, "rollout-2026-08-15T12-00-00-session-mixed.jsonl")
	rollout := "" +
		`{"timestamp":"2026-08-15T12:00:00Z","type":"session_meta","payload":{"id":"session-mixed","cwd":` + quoteJSON(projectPath) + `}}` + "\n" +
		`{"timestamp":"2026-08-15T12:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<recommended_plugins>plugin context</recommended_plugins>Real question"}]}}` + "\n"
	if err := os.WriteFile(sessionPath, []byte(rollout), 0o600); err != nil {
		t.Fatalf("failed to write rollout: %v", err)
	}

	adapter := &CodexAdapter{homeDir: homeDir}
	sessions, err := adapter.ListSessions(projectPath, 10)
	if err != nil {
		t.Fatalf("ListSessions returned error: %v", err)
	}
	if len(sessions) != 1 || sessions[0].FirstMessage != "Real question" {
		t.Fatalf("unexpected sessions: %+v", sessions)
	}

	messages, err := adapter.GetSession("session-mixed", 0, 20)
	if err != nil {
		t.Fatalf("GetSession returned error: %v", err)
	}
	if len(messages) != 1 || messages[0].Content != "Real question" {
		t.Fatalf("unexpected messages: %+v", messages)
	}
}

func quoteJSON(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}
