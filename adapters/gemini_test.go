package adapters

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseSessionMetadataCountsUserMessagesCaseInsensitive(t *testing.T) {
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

func TestGeminiAdapterReadsCurrentJSONLRecordings(t *testing.T) {
	home := t.TempDir()
	projectPath := filepath.Join(home, "project")
	hash := hashProjectPath(projectPath)
	chatsDir := filepath.Join(home, ".gemini", "tmp", hash, "chats")
	if err := os.MkdirAll(chatsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	recording := `{"sessionId":"jsonl-session","projectHash":"` + hash + `","startTime":"2026-08-11T10:00:00Z","lastUpdated":"2026-08-11T10:01:00Z"}
{"id":"user-1","timestamp":"2026-08-11T10:00:01Z","type":"user","content":[{"text":"First current prompt"}]}
{"id":"gemini-1","timestamp":"2026-08-11T10:00:02Z","type":"gemini","content":[{"text":"Superseded answer"}]}
{"$rewindTo":"gemini-1"}
{"id":"gemini-2","timestamp":"2026-08-11T10:00:03Z","type":"gemini","content":[{"text":"Current answer"}]}
{"$set":{"summary":"Current JSONL session"}}
`
	path := filepath.Join(chatsDir, "session-2026-08-11T10-00-jsonl-se.jsonl")
	if err := os.WriteFile(path, []byte(recording), 0o600); err != nil {
		t.Fatal(err)
	}

	adapter := &GeminiAdapter{homeDir: home, projectCache: make(map[string]string)}
	sessions, err := adapter.ListSessions(projectPath, 0)
	if err != nil {
		t.Fatalf("ListSessions returned error: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one session, got %d", len(sessions))
	}
	if sessions[0].ID != "jsonl-session" || sessions[0].FirstMessage != "First current prompt" || sessions[0].Summary != "Current JSONL session" {
		t.Fatalf("unexpected session metadata: %+v", sessions[0])
	}

	messages, err := adapter.GetSession("jsonl-session", 0, 10)
	if err != nil {
		t.Fatalf("GetSession returned error: %v", err)
	}
	if len(messages) != 2 || messages[1].Content != "Current answer" {
		t.Fatalf("unexpected JSONL messages: %+v", messages)
	}
}

func TestLoadGeminiSessionAcceptsJSONLRecordLargerThanTenMiB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session-large.jsonl")
	largeContent := strings.Repeat("x", 10*1024*1024+1)
	metadata := `{"sessionId":"large-session","projectHash":"hash","startTime":"2026-08-17T10:00:00Z"}`
	message, err := json.Marshal(geminiMessage{
		ID:      "large-message",
		Type:    "user",
		Content: largeContent,
	})
	if err != nil {
		t.Fatal(err)
	}
	contents := append([]byte(metadata+"\n"), message...)
	contents = append(contents, '\n')
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}

	session, err := loadGeminiSession(path)
	if err != nil {
		t.Fatalf("loadGeminiSession returned error: %v", err)
	}
	if len(session.Messages) != 1 {
		t.Fatalf("expected one message, got %d", len(session.Messages))
	}
	if got := session.Messages[0].Content; got != largeContent {
		t.Fatalf("large message content was not preserved: got %T with length %d", got, len(fmt.Sprint(got)))
	}
}

func TestReadGeminiJSONLRecordEnforcesLimit(t *testing.T) {
	reader := bufio.NewReaderSize(strings.NewReader("12345678\n"), 4)
	if _, err := readGeminiJSONLRecord(reader, 8); !errors.Is(err, errGeminiJSONLRecordTooLarge) {
		t.Fatalf("readGeminiJSONLRecord error = %v, want %v", err, errGeminiJSONLRecordTooLarge)
	}
}
