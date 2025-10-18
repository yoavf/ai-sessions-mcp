package adapters

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseSessionMetadataCountsUserMessagesCaseInsensitive(t *testing.T) {
	t.Helper()

	tmpDir := t.TempDir()
	projectPath := "/abs/project"
	hash := hashProjectPath(projectPath)
	sessionDir := filepath.Join(tmpDir, hash, "chats")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("failed to create session dir: %v", err)
	}
	sessionPath := filepath.Join(sessionDir, "session-test.json")

	sess := geminiSession{
		SessionID: "session-123",
		StartTime: time.Now().Format(time.RFC3339),
		Messages: []geminiMessage{
			{
				Type:    "USER",
				Content: "First question?\nSecond line",
			},
			{
				Type:    "GEMINI",
				Content: "Some reply",
			},
		},
	}

	data, err := json.Marshal(sess)
	if err != nil {
		t.Fatalf("failed to marshal session: %v", err)
	}

	if err := os.WriteFile(sessionPath, data, 0o600); err != nil {
		t.Fatalf("failed to write session file: %v", err)
	}

	adapter := &GeminiAdapter{homeDir: tmpDir, projectCache: make(map[string]string)}
	session, err := adapter.parseSessionMetadata(sessionPath, projectPath)
	if err != nil {
		t.Fatalf("parseSessionMetadata returned error: %v", err)
	}

	if session.UserMessageCount != 1 {
		t.Fatalf("expected UserMessageCount to be 1, got %d", session.UserMessageCount)
	}

	if session.FirstMessage != "First question?" {
		t.Fatalf("expected FirstMessage to be %q, got %q", "First question?", session.FirstMessage)
	}

	messages, err := adapter.readAllMessages(sessionPath)
	if err != nil {
		t.Fatalf("readAllMessages returned error: %v", err)
	}

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	if messages[0].Role != "user" {
		t.Fatalf("expected first message role to be 'user', got %q", messages[0].Role)
	}
	if messages[1].Role != "assistant" {
		t.Fatalf("expected second message role to be 'assistant', got %q", messages[1].Role)
	}
}

func TestParseSessionMetadataInfersProjectPath(t *testing.T) {
	tmpDir := t.TempDir()
	projectPath := "/Users/test/project"
	hash := hashProjectPath(projectPath)
	sessionDir := filepath.Join(tmpDir, hash, "chats")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("failed to create session dir: %v", err)
	}
	sessionPath := filepath.Join(sessionDir, "session-test.json")

	sess := geminiSession{
		SessionID: "session-hash",
		Messages: []geminiMessage{
			{
				Type: "GEMINI",
				ToolCalls: []geminiToolCall{
					{
						Name: "list_directory",
						Args: map[string]interface{}{
							"path": projectPath + "/cmd",
						},
					},
				},
			},
		},
	}

	data, err := json.Marshal(sess)
	if err != nil {
		t.Fatalf("failed to marshal session: %v", err)
	}
	if err := os.WriteFile(sessionPath, data, 0o600); err != nil {
		t.Fatalf("failed to write session file: %v", err)
	}

	adapter := &GeminiAdapter{homeDir: tmpDir, projectCache: make(map[string]string)}
	session, err := adapter.parseSessionMetadata(sessionPath, "unknown-project-"+hash)
	if err != nil {
		t.Fatalf("parseSessionMetadata returned error: %v", err)
	}

	if session.ProjectPath != projectPath {
		t.Fatalf("expected ProjectPath %q, got %q", projectPath, session.ProjectPath)
	}
}

func TestNormalizeGeminiRole(t *testing.T) {
	table := []struct {
		msg  geminiMessage
		want string
	}{
		{geminiMessage{Role: "USER"}, "user"},
		{geminiMessage{Role: "Assistant"}, "assistant"},
		{geminiMessage{Type: "MODEL"}, "assistant"},
		{geminiMessage{Type: "GEMINI"}, "assistant"},
		{geminiMessage{Type: "system"}, "system"},
		{geminiMessage{Type: "TOOL"}, "tool"},
	}

	for _, tc := range table {
		if got := normalizeGeminiRole(tc.msg); got != tc.want {
			t.Fatalf("normalizeGeminiRole(%+v)=%q want %q", tc.msg, got, tc.want)
		}
	}
}
