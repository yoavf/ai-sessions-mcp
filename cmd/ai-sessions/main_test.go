package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yoavf/ai-sessions-mcp/adapters"
	"github.com/yoavf/ai-sessions-mcp/search"
)

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
