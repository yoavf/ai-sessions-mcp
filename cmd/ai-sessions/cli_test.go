package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yoavf/ai-sessions-mcp/adapters"
)

func TestValidateAPIURL(t *testing.T) {
	allowed := []string{
		"https://aisessions.dev",
		"http://localhost:3000",
		"http://127.0.0.1:8080",
	}
	for _, url := range allowed {
		if err := validateAPIURL(url); err != nil {
			t.Fatalf("validateAPIURL(%q) returned error: %v", url, err)
		}
	}

	disallowed := []string{
		"https://localhost:3000", // wrong scheme
		"http://example.com",
		"ftp://aisessions.dev",
	}
	for _, url := range disallowed {
		if err := validateAPIURL(url); err == nil {
			t.Fatalf("validateAPIURL(%q) expected error", url)
		}
	}
}

func TestValidateTokenFormat(t *testing.T) {
	if err := validateTokenFormat("abc.def.ghi"); err != nil {
		t.Fatalf("validateTokenFormat valid token returned error: %v", err)
	}

	invalid := []string{
		"abc.def",         // missing part
		"abc..def",        // empty part
		"abc.def.g h",     // space
		"abc.def.ghi+jkl", // illegal char
		"",                // empty
		"abc.def.ghi.",    // trailing dot => empty part
	}
	for _, token := range invalid {
		if err := validateTokenFormat(token); err == nil {
			t.Fatalf("validateTokenFormat(%q) expected error", token)
		}
	}
}

func TestMakeClickableURL(t *testing.T) {
	url := "https://aisessions.dev"
	clickable := makeClickableURL(url)
	expected := "\x1b]8;;" + url + "\x1b\\" + url + "\x1b]8;;\x1b\\"
	if clickable != expected {
		t.Fatalf("makeClickableURL produced %q, want %q", clickable, expected)
	}
}

func TestFormatRelativeTime(t *testing.T) {
	now := time.Now()

	cases := []struct {
		input time.Time
		want  string
	}{
		{now.Add(-30 * time.Second), "just now"},
		{now.Add(-2 * time.Minute), "2 mins ago"},
		{now.Add(-1 * time.Hour), "1 hour ago"},
		{now.Add(-26 * time.Hour), "yesterday"},
	}

	for _, tc := range cases {
		if got := formatRelativeTime(tc.input); got != tc.want {
			t.Fatalf("formatRelativeTime(%v)=%q want %q", tc.input, got, tc.want)
		}
	}

	old := now.Add(-8 * 24 * time.Hour)
	if got := formatRelativeTime(old); got != old.Format("Jan 2") {
		t.Fatalf("formatRelativeTime old=%q want %q", got, old.Format("Jan 2"))
	}
}

func TestGetProjectName(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome) // windows compatibility

	pathInside := filepath.Join(tempHome, "code", "proj")
	got := getProjectName(pathInside)
	if got != "code-proj" {
		t.Fatalf("getProjectName returned %q, want code-proj", got)
	}

	pathOutside := "/tmp/other/project"
	wantOutside := strings.ReplaceAll(filepath.Base(pathOutside), string(filepath.Separator), "-")
	if got := getProjectName(pathOutside); got != wantOutside {
		t.Fatalf("getProjectName outside home=%q want %q", got, wantOutside)
	}
}

func TestCleanFirstMessage(t *testing.T) {
	msg := "  hello world  "
	if got := cleanFirstMessage(msg, 20); got != "hello world" {
		t.Fatalf("cleanFirstMessage trimming failed: %q", got)
	}

	if got := cleanFirstMessage("1234567890", 7); got != "1234..." {
		t.Fatalf("cleanFirstMessage truncation failed: %q", got)
	}
}

func TestTruncateStringHelpers(t *testing.T) {
	if got := truncateString("short", 10); got != "short" {
		t.Fatalf("truncateString no-op failed: %q", got)
	}
	if got := truncateString("averylongstring", 8); got != "avery..." {
		t.Fatalf("truncateString truncation failed: %q", got)
	}

	if got := truncateStringStart("project-name-long", 6); got != "...ong" {
		t.Fatalf("truncateStringStart truncation failed: %q", got)
	}
	if got := truncateStringStart("small", 10); got != "small" {
		t.Fatalf("truncateStringStart no-op failed: %q", got)
	}
}

func TestFormatSessionRow(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	session := adapters.Session{
		ProjectPath:  filepath.Join(tempHome, "proj"),
		FirstMessage: "Message from user",
		Timestamp:    time.Now().Add(-3 * time.Hour),
	}
	width := 100
	row := formatSessionRow(session, width)

	projectName := getProjectName(session.ProjectPath)
	if !strings.Contains(row, projectName) {
		t.Fatalf("formatSessionRow missing project name %q in %q", projectName, row)
	}
	if !strings.Contains(row, "Message from user") {
		t.Fatalf("formatSessionRow missing message in %q", row)
	}
	if !strings.Contains(row, truncateString(formatRelativeTime(session.Timestamp), 12)) {
		t.Fatalf("formatSessionRow missing time column in %q", row)
	}
}

func TestFormatTableHeader(t *testing.T) {
	header := formatTableHeader()
	if !strings.Contains(header, "TIME") || !strings.Contains(header, "PROJECT") || !strings.Contains(header, "MESSAGE") {
		t.Fatalf("formatTableHeader missing columns: %q", header)
	}
}

func TestGetAPIURL(t *testing.T) {
	if got := getAPIURL(""); got != defaultAPIURL {
		t.Fatalf("getAPIURL empty returned %q want %q", got, defaultAPIURL)
	}
	if got := getAPIURL("https://custom.dev"); got != "https://custom.dev" {
		t.Fatalf("getAPIURL override failed: %q", got)
	}
}

func TestConfigPersistence(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	config := Config{Token: "abc.def.ghi"}
	if err := saveConfig(config); err != nil {
		t.Fatalf("saveConfig failed: %v", err)
	}

	configPath, err := getConfigPath()
	if err != nil {
		t.Fatalf("getConfigPath failed: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("config file not written: %v", err)
	}

	var stored Config
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatalf("config json invalid: %v", err)
	}
	if stored.Token != config.Token {
		t.Fatalf("config token mismatch: %q", stored.Token)
	}

	loaded, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}
	if loaded.Token != config.Token {
		t.Fatalf("loadConfig returned token %q, want %q", loaded.Token, config.Token)
	}
}

func TestRunLoginWithImmediateToken(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	var saved Config
	deps := loginDeps{
		stdin:  strings.NewReader("abc.def.ghi\n"),
		stdout: stdout,
		stderr: stderr,
		openBrowser: func(string) error {
			t.Fatalf("openBrowser should not be called when token is provided")
			return nil
		},
		validateToken: func(token string) error {
			if token != "abc.def.ghi" {
				t.Fatalf("unexpected token %q", token)
			}
			return nil
		},
		saveConfig: func(cfg Config) error {
			saved = cfg
			return nil
		},
	}

	if err := runLogin("https://aisessions.dev", deps); err != nil {
		t.Fatalf("runLogin returned error: %v", err)
	}

	if saved.Token != "abc.def.ghi" {
		t.Fatalf("saveConfig called with token %q", saved.Token)
	}

	output := stdout.String()
	if !strings.Contains(output, "Token saved successfully") {
		t.Fatalf("stdout missing success message: %q", output)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr should be empty, got %q", stderr.String())
	}
}

func TestRunLoginPromptsAndOpensBrowser(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	var browserCalls int
	deps := loginDeps{
		stdin:  strings.NewReader("\nabc.def.ghi\n"),
		stdout: stdout,
		stderr: stderr,
		openBrowser: func(url string) error {
			browserCalls++
			if url != "https://aisessions.dev/my-transcripts" {
				t.Fatalf("openBrowser received wrong URL %q", url)
			}
			return nil
		},
		validateToken: func(token string) error {
			if token != "abc.def.ghi" {
				t.Fatalf("unexpected token %q", token)
			}
			return nil
		},
		saveConfig: func(Config) error {
			return nil
		},
	}

	if err := runLogin("https://aisessions.dev", deps); err != nil {
		t.Fatalf("runLogin returned error: %v", err)
	}

	if browserCalls != 1 {
		t.Fatalf("expected openBrowser to be called once, got %d", browserCalls)
	}
	if !strings.Contains(stdout.String(), "Opening browser") {
		t.Fatalf("stdout missing browser message: %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr should be empty, got %q", stderr.String())
	}
}

func TestRunLoginInvalidURL(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	deps := loginDeps{
		stdin:         strings.NewReader("token\n"),
		stdout:        stdout,
		stderr:        stderr,
		openBrowser:   func(string) error { return nil },
		validateToken: func(string) error { return nil },
		saveConfig:    func(Config) error { return nil },
	}

	err := runLogin("http://example.com", deps)
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
	if !strings.Contains(stderr.String(), "Invalid API URL") {
		t.Fatalf("stderr missing invalid URL message: %q", stderr.String())
	}
}

func TestRunLoginInvalidToken(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	deps := loginDeps{
		stdin:         strings.NewReader("badtoken\n"),
		stdout:        stdout,
		stderr:        stderr,
		openBrowser:   func(string) error { return nil },
		validateToken: func(string) error { return fmt.Errorf("bad token") },
		saveConfig:    func(Config) error { return nil },
	}

	err := runLogin("https://aisessions.dev", deps)
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
	if !strings.Contains(stderr.String(), "bad token") {
		t.Fatalf("stderr missing token message: %q", stderr.String())
	}
}

func TestRunLoginSaveConfigError(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	deps := loginDeps{
		stdin:         strings.NewReader("abc.def.ghi\n"),
		stdout:        stdout,
		stderr:        stderr,
		openBrowser:   func(string) error { return nil },
		validateToken: func(string) error { return nil },
		saveConfig:    func(Config) error { return fmt.Errorf("disk full") },
	}

	err := runLogin("https://aisessions.dev", deps)
	if err == nil {
		t.Fatal("expected error when saveConfig fails")
	}
	if !strings.Contains(stderr.String(), "Error saving configuration") {
		t.Fatalf("stderr missing save error message: %q", stderr.String())
	}
}
