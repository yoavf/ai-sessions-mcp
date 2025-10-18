package adapters

import (
	"strings"
	"testing"
)

func TestProjectDirName(t *testing.T) {
	got := projectDirName("/Users/dev/project/subdir")
	if got != "-Users-dev-project-subdir" {
		t.Fatalf("projectDirName produced %q", got)
	}
}

func TestStripSystemXMLTags(t *testing.T) {
	input := "<ide_opened_file>/path/to/file</ide_opened_file>   user question"
	if got := stripSystemXMLTags(input); got != "user question" {
		t.Fatalf("stripSystemXMLTags returned %q", got)
	}
}

func TestExtractFirstLineString(t *testing.T) {
	longLine := strings.Repeat("a", 210)
	text := "\n\n" + longLine + "\nnext line"
	got := extractFirstLine(text)
	if len(got) != 203 || !strings.HasSuffix(got, "...") {
		t.Fatalf("extractFirstLine should truncate long lines, got %q", got)
	}
}

func TestExtractFirstLineStructured(t *testing.T) {
	content := []interface{}{
		map[string]interface{}{"text": "First meaningful line\nSecond line"},
	}
	got := extractFirstLine(content)
	if got != "First meaningful line" {
		t.Fatalf("extractFirstLine failed for structured content, got %q", got)
	}
}

func TestExtractFirstLineSkipsSystemPrefixes(t *testing.T) {
	content := "<local-command-stdout>ignored</local-command-stdout>\nReal question?"
	if got := extractFirstLine(content); got != "Real question?" {
		t.Fatalf("extractFirstLine failed to skip system block, got %q", got)
	}
}

func TestHashProjectPathStable(t *testing.T) {
	want := "6d80187b454107127bf995f2c31a2a92940d931c228d612aee955facb08fe415"
	if got := hashProjectPath("/abs/path"); got != want {
		t.Fatalf("hashProjectPath mismatch: got %q want %q", got, want)
	}
}

func TestExtractFirstLineFromContentVariants(t *testing.T) {
	if got := extractFirstLineFromContent("   first\nsecond"); got != "first" {
		t.Fatalf("extractFirstLineFromContent string: %q", got)
	}

	arrayContent := []interface{}{
		map[string]interface{}{"text": "\nvalue from map\n"},
	}
	if got := extractFirstLineFromContent(arrayContent); got != "value from map" {
		t.Fatalf("extractFirstLineFromContent array: %q", got)
	}
}

func TestContentToStringGemini(t *testing.T) {
	content := []interface{}{
		map[string]interface{}{"text": "part A"},
		map[string]interface{}{"text": "part B"},
	}
	got := contentToStringGemini(content)
	if got != "part A\npart B" {
		t.Fatalf("contentToStringGemini returned %q", got)
	}
}

func TestCodexExtractUserText(t *testing.T) {
	adapter := &CodexAdapter{}
	content := []interface{}{
		map[string]interface{}{"type": "input_text", "text": "first"},
		map[string]interface{}{"type": "input_code", "text": "ignored"},
		map[string]interface{}{"type": "input_text", "text": " second"},
	}
	if got := adapter.extractUserText(content); got != "first second" {
		t.Fatalf("extractUserText returned %q", got)
	}
}

func TestCodexIsSessionPrefix(t *testing.T) {
	adapter := &CodexAdapter{}
	table := []struct {
		text string
		want bool
	}{
		{"<user_instructions>hi</user_instructions>", true},
		{"<environment_context>info</environment_context>", true},
		{" user prompt ", false},
	}
	for _, tc := range table {
		if got := adapter.isSessionPrefix(tc.text); got != tc.want {
			t.Fatalf("isSessionPrefix(%q)=%v want %v", tc.text, got, tc.want)
		}
	}
}

func TestCodexExtractFirstLine(t *testing.T) {
	adapter := &CodexAdapter{}
	text := "   line one\nline two"
	if got := adapter.extractFirstLine(text); got != "line one" {
		t.Fatalf("extractFirstLine returned %q", got)
	}
}

func TestSessionInfoCWDMatches(t *testing.T) {
	info := &sessionInfo{CWD: "/a/b"}
	if !info.CWDMatches("/a/b") {
		t.Fatal("CWDMatches should match identical paths")
	}
	if info.CWDMatches("/different") {
		t.Fatal("CWDMatches should not match different paths")
	}
}

func TestCursorAdapterNotImplemented(t *testing.T) {
	if _, err := NewCursorAdapter(); err == nil {
		t.Fatal("expected error from NewCursorAdapter")
	}
	adapter := &CursorAdapter{}
	if _, err := adapter.ListSessions("", 0); err == nil {
		t.Fatal("ListSessions should return error")
	}
	if _, err := adapter.GetSession("id", 0, 10); err == nil {
		t.Fatal("GetSession should return error")
	}
	if _, err := adapter.SearchSessions("", "", 0); err == nil {
		t.Fatal("SearchSessions should return error")
	}
}
