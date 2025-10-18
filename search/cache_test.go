package search

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yoavf/ai-sessions-mcp/adapters"
)

func newTempCache(t *testing.T) *Cache {
	t.Helper()
	cachePath := filepath.Join(t.TempDir(), "cache.db")
	cache, err := NewCache(cachePath)
	if err != nil {
		t.Fatalf("NewCache failed: %v", err)
	}
	t.Cleanup(func() {
		_ = cache.Close()
	})
	return cache
}

func TestTokenizeAndTermFrequency(t *testing.T) {
	tokens := Tokenize("Hello, HELLO! numbers123 stay; x y z.")
	want := []string{"hello", "hello", "numbers123", "stay"}
	if len(tokens) != len(want) {
		t.Fatalf("Tokenize produced %v, want %v", tokens, want)
	}
	for i, token := range tokens {
		if token != want[i] {
			t.Fatalf("Tokenize[%d]=%q want %q", i, token, want[i])
		}
	}

	freqs := TermFrequency(tokens)
	if freq := freqs["hello"]; freq != 2 {
		t.Fatalf("TermFrequency for hello=%d want 2", freq)
	}
	if _, ok := freqs["x"]; ok {
		t.Fatal("Tokenize should skip single letter tokens")
	}
}

func TestBM25Score(t *testing.T) {
	scorer := NewBM25Scorer(100, 10)
	termFreqs := map[string]int{"gopher": 2}
	docFreqs := map[string]int{"gopher": 1}
	score := scorer.Score([]string{"gopher"}, termFreqs, 120, docFreqs)

	// Recalculate expected score inline for clarity
	idf := math.Log((10 - 1 + 0.5) / (1 + 0.5))
	tfNorm := (2 * (k1 + 1)) / (2 + k1*(1-b+b*120/100))
	want := idf * tfNorm

	if math.Abs(score-want) > 1e-9 {
		t.Fatalf("BM25 score=%f want %f", score, want)
	}
	if score <= 0 {
		t.Fatal("BM25 score should be positive")
	}
}

func TestGetSnippet(t *testing.T) {
	content := "This is the beginning of the document. Important keyword appears here followed by more context."
	snippet := GetSnippet(content, []string{"keyword"}, 40)
	if !strings.Contains(snippet, "keyword") {
		t.Fatalf("snippet missing keyword: %q", snippet)
	}
	if !strings.HasPrefix(snippet, "...") || !strings.HasSuffix(snippet, "...") {
		t.Fatalf("snippet should use ellipsis when trimming, got %q", snippet)
	}
}

func TestCacheIndexSearchAndNeedsReindex(t *testing.T) {
	cache := newTempCache(t)
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "session.jsonl")
	if err := os.WriteFile(filePath, []byte("test"), 0o644); err != nil {
		t.Fatalf("write session file: %v", err)
	}

	session := adapters.Session{
		ID:           "sess-123",
		Source:       "codex",
		ProjectPath:  "/workspace",
		FirstMessage: "Initial intro",
		Summary:      "Summary info",
		Timestamp:    time.Now(),
		FilePath:     filePath,
	}

	content := "Initial intro explains context. Keyword appears in the detailed content block to verify search."
	if err := cache.IndexSession(session, content); err != nil {
		t.Fatalf("IndexSession failed: %v", err)
	}

	needs, err := cache.NeedsReindex(session.ID, filePath)
	if err != nil {
		t.Fatalf("NeedsReindex failed: %v", err)
	}
	if needs {
		t.Fatal("session should not need reindex immediately after indexing")
	}

	results, err := cache.Search("keyword", "codex", "/workspace", 5)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Search returned %d results, want 1", len(results))
	}
	if !strings.Contains(strings.ToLower(results[0].Snippet), "keyword") {
		t.Fatalf("snippet missing keyword: %q", results[0].Snippet)
	}

	// Ensure source/project filters apply
	results, err = cache.Search("keyword", "other", "/workspace", 5)
	if err != nil {
		t.Fatalf("Search with source filter failed: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no results with mismatched source, got %d", len(results))
	}

	// Update file mtime to trigger reindex requirement
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(filePath, future, future); err != nil {
		t.Fatalf("Chtimes failed: %v", err)
	}

	needs, err = cache.NeedsReindex(session.ID, filePath)
	if err != nil {
		t.Fatalf("NeedsReindex (after touch) failed: %v", err)
	}
	if !needs {
		t.Fatal("expected NeedsReindex to return true after file mtime change")
	}
}
