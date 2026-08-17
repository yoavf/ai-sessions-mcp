package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yoavf/ai-sessions-mcp/adapters"
	"github.com/yoavf/ai-sessions-mcp/search"
)

func TestMCPServerProcess(t *testing.T) {
	if os.Getenv("AI_SESSIONS_MCP_TEST_PROCESS") != "1" {
		return
	}
	os.Args = []string{os.Args[0]}
	main()
	os.Exit(0)
}

func TestVersionProcess(t *testing.T) {
	if os.Getenv("AI_SESSIONS_VERSION_TEST_PROCESS") != "1" {
		return
	}
	os.Args = []string{os.Args[0], "--version"}
	main()
	os.Exit(0)
}

func TestVersionUsesBuildVersion(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestVersionProcess$")
	cmd.Env = append(os.Environ(), "AI_SESSIONS_VERSION_TEST_PROCESS=1")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("version command failed: %v", err)
	}
	if got, want := string(output), "aisessions version dev\n"; got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func overrideEnv(environ []string, overrides ...string) []string {
	overrideKeys := make(map[string]struct{}, len(overrides))
	for _, entry := range overrides {
		key, _, _ := strings.Cut(entry, "=")
		overrideKeys[strings.ToUpper(key)] = struct{}{}
	}

	result := make([]string, 0, len(environ)+len(overrides))
	for _, entry := range environ {
		key, _, _ := strings.Cut(entry, "=")
		if _, overridden := overrideKeys[strings.ToUpper(key)]; !overridden {
			result = append(result, entry)
		}
	}
	return append(result, overrides...)
}

func TestOverrideEnvReplacesExistingValuesCaseInsensitively(t *testing.T) {
	got := overrideEnv(
		[]string{"HOME=/real-home", "Path=/bin", "home=/duplicate-home"},
		"HOME=/test-home",
		"USERPROFILE=/test-home",
	)
	want := []string{"Path=/bin", "HOME=/test-home", "USERPROFILE=/test-home"}
	if !slices.Equal(got, want) {
		t.Fatalf("overrideEnv() = %q, want %q", got, want)
	}
}

func TestMCPStdioServerListsAndCallsReadOnlyTools(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	testHome := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestMCPServerProcess$")
	cmd.Env = overrideEnv(os.Environ(),
		"AI_SESSIONS_MCP_TEST_PROCESS=1",
		"HOME="+testHome,
		"USERPROFILE="+testHome,
	)

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "ai-sessions-test-client",
		Version: "1.0.0",
	}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("failed to connect over stdio: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	toolsResult, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("failed to list tools: %v", err)
	}

	wantTools := map[string]bool{
		"list_available_sources": false,
		"list_sessions":          false,
		"search_sessions":        false,
		"get_session":            false,
	}
	for _, tool := range toolsResult.Tools {
		if _, ok := wantTools[tool.Name]; !ok {
			t.Fatalf("unexpected tool %q", tool.Name)
		}
		wantTools[tool.Name] = true
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("tool %q must advertise readOnlyHint", tool.Name)
		}
	}
	for name, found := range wantTools {
		if !found {
			t.Errorf("missing tool %q", name)
		}
	}

	callResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_available_sources",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("failed to call list_available_sources: %v", err)
	}
	if callResult.IsError {
		t.Fatal("list_available_sources returned an MCP error")
	}
	if len(callResult.Content) != 1 {
		t.Fatalf("expected one content block, got %d", len(callResult.Content))
	}
	textContent, ok := callResult.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected text content, got %T", callResult.Content[0])
	}
	var payload struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(textContent.Text), &payload); err != nil {
		t.Fatalf("invalid JSON tool result: %v", err)
	}
	if payload.Count != 6 {
		t.Fatalf("expected 6 available sources, got %d", payload.Count)
	}
	cachePath := filepath.Join(testHome, ".cache", "ai-sessions", "search.db")
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("server did not create its cache under the isolated test home: %v", err)
	}
}

type stubAdapter struct {
	sessions  []adapters.Session
	messages  map[string][]adapters.Message
	listErr   error
	listCalls int
	getCalls  map[string]int
}

func newStubAdapter(sessions []adapters.Session, messages map[string][]adapters.Message) *stubAdapter {
	if messages == nil {
		messages = make(map[string][]adapters.Message)
	}
	return &stubAdapter{
		sessions: sessions,
		messages: messages,
		getCalls: make(map[string]int),
	}
}

func (s *stubAdapter) Name() string {
	return "stub"
}

func (s *stubAdapter) ListSessions(projectPath string, limit int) ([]adapters.Session, error) {
	s.listCalls++
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.sessions, nil
}

func (s *stubAdapter) GetSession(sessionID string, page, pageSize int) ([]adapters.Message, error) {
	s.getCalls[sessionID]++
	if msgs, ok := s.messages[sessionID]; ok {
		return msgs, nil
	}
	return nil, fmt.Errorf("unknown session %s", sessionID)
}

func (s *stubAdapter) SearchSessions(projectPath, query string, limit int) ([]adapters.Session, error) {
	return nil, nil
}

func newTestCache(t *testing.T) *search.Cache {
	t.Helper()
	cachePath := filepath.Join(t.TempDir(), "cache.db")
	cache, err := search.NewCache(cachePath)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	t.Cleanup(func() {
		_ = cache.Close()
	})
	return cache
}

func TestIndexSessionsIndexesAndSkipsUpToDateSessions(t *testing.T) {
	cache := newTestCache(t)

	sessionFile := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(sessionFile, []byte("dummy"), 0o644); err != nil {
		t.Fatalf("failed to create session file: %v", err)
	}

	session := adapters.Session{
		ID:           "sess-1",
		Source:       "stub",
		ProjectPath:  "/project",
		FirstMessage: "Initial question",
		Summary:      "Helpful summary",
		Timestamp:    time.Now(),
		FilePath:     sessionFile,
	}

	messages := map[string][]adapters.Message{
		"sess-1": {
			{Role: "user", Content: "unique keyword appears here", Timestamp: time.Now()},
			{Role: "assistant", Content: "assistant reply", Timestamp: time.Now()},
		},
	}

	adapter := newStubAdapter([]adapters.Session{session}, messages)

	adaptersMap := map[string]adapters.SessionAdapter{"stub": adapter}

	if err := indexSessions(adaptersMap, cache, "", ""); err != nil {
		t.Fatalf("indexSessions returned error: %v", err)
	}

	if got := adapter.getCalls["sess-1"]; got != 1 {
		t.Fatalf("expected 1 GetSession call after initial index, got %d", got)
	}

	results, err := cache.Search("unique keyword", "", "", 10)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(results))
	}
	if results[0].Session.ID != "sess-1" {
		t.Fatalf("expected search result for sess-1, got %s", results[0].Session.ID)
	}

	if err := indexSessions(adaptersMap, cache, "", ""); err != nil {
		t.Fatalf("indexSessions (second run) returned error: %v", err)
	}
	if got := adapter.getCalls["sess-1"]; got != 1 {
		t.Fatalf("expected GetSession call count to remain 1, got %d", got)
	}

	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(sessionFile, future, future); err != nil {
		t.Fatalf("failed to update file mtime: %v", err)
	}

	if err := indexSessions(adaptersMap, cache, "", ""); err != nil {
		t.Fatalf("indexSessions (after mtime change) returned error: %v", err)
	}
	if got := adapter.getCalls["sess-1"]; got != 2 {
		t.Fatalf("expected GetSession call count to be 2 after reindex, got %d", got)
	}
}

func TestIndexSessionsSkipsUnknownSource(t *testing.T) {
	cache := newTestCache(t)

	adapter := newStubAdapter(nil, nil)
	adaptersMap := map[string]adapters.SessionAdapter{"stub": adapter}

	if err := indexSessions(adaptersMap, cache, "other", ""); err != nil {
		t.Fatalf("indexSessions returned error: %v", err)
	}

	if adapter.listCalls != 0 {
		t.Fatalf("expected ListSessions not to be called, got %d", adapter.listCalls)
	}
	if len(adapter.getCalls) != 0 {
		t.Fatalf("expected GetSession not to be called, got %d calls", len(adapter.getCalls))
	}
}
