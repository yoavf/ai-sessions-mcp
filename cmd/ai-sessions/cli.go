package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/manifoldco/promptui"
	"github.com/yoavf/ai-sessions-mcp/adapters"
	"golang.org/x/term"
)

const (
	defaultAPIURL = "https://aisessions.dev"
	configDir     = ".aisessions"
	configFile    = "config.json"
)

type Config struct {
	Token string `json:"token"`
}

// validateAPIURL validates that a URL is from a trusted domain
// For security, we only allow:
// - https://aisessions.dev (production)
// - http://localhost:* (local development)
// - http://127.0.0.1:* (local development)
func validateAPIURL(apiURL string) error {
	parsedURL, err := url.Parse(apiURL)
	if err != nil {
		return fmt.Errorf("invalid URL format: %w", err)
	}

	// For localhost/127.0.0.1, only allow http (not https to avoid cert issues in dev)
	if parsedURL.Hostname() == "localhost" || parsedURL.Hostname() == "127.0.0.1" {
		if parsedURL.Scheme != "http" {
			return fmt.Errorf("localhost URLs must use http:// (not https://)")
		}
		return nil
	}

	// For production, only allow aisessions.dev with https
	if parsedURL.Hostname() == "aisessions.dev" {
		if parsedURL.Scheme != "https" {
			return fmt.Errorf("aisessions.dev must use https://")
		}
		return nil
	}

	return fmt.Errorf("untrusted domain: %s (only aisessions.dev, localhost, and 127.0.0.1 are allowed)", parsedURL.Hostname())
}

// handleCLI routes CLI commands to their handlers
func handleCLI() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "login", "config":
		// Parse optional --url flag for login command
		var apiURL string
		for i := 2; i < len(os.Args); i++ {
			if os.Args[i] == "--url" {
				if i+1 >= len(os.Args) {
					fmt.Fprintf(os.Stderr, "Error: --url requires a value\n")
					os.Exit(1)
				}
				apiURL = os.Args[i+1]
				break
			}
		}
		handleLogin(apiURL)
	case "upload":
		handleUploadCommand()
	case "version", "-v", "--version":
		fmt.Println("aisessions version 2.0.0")
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

// printUsage displays CLI usage information
func printUsage() {
	fmt.Println(`AI Sessions CLI

Usage:
  aisessions <command> [options]

Commands:
  login              Configure authentication token
  upload <file>      Upload a transcript file
  version            Show version information
  help               Show this help message

Options:
  --title <title>    Set the title for the uploaded transcript (upload only)
  --url <url>        Override API URL (default: https://aisessions.dev)

Examples:
  aisessions login
  aisessions upload session.jsonl
  aisessions upload session.jsonl --title "Bug Fix Session"

  # Development mode (use local server)
  aisessions login --url http://localhost:3000
  aisessions upload session.jsonl --url http://localhost:3000

Authentication:
  1. Visit https://aisessions.dev/my-transcripts
  2. Click "Generate CLI Token"
  3. Run 'aisessions login' and paste the token

For more information, visit: https://github.com/yoavf/ai-sessions-mcp`)
}

// openBrowser opens the default browser to the given URL
func openBrowser(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "linux":
		cmd = "xdg-open"
		args = []string{url}
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", url}
	default:
		return fmt.Errorf("unsupported platform")
	}

	return exec.Command(cmd, args...).Start()
}

// makeClickableURL creates a clickable terminal hyperlink using ANSI escape codes
func makeClickableURL(url string) string {
	// OSC 8 hyperlink format: \e]8;;URL\e\\TEXT\e]8;;\e\\
	return fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", url, url)
}

// validateTokenFormat validates that a token looks like a valid JWT
func validateTokenFormat(token string) error {
	// JWT tokens have 3 parts separated by dots: header.payload.signature
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return fmt.Errorf("invalid token format: expected JWT with 3 parts (header.payload.signature), got %d parts", len(parts))
	}

	// Check that each part is non-empty and contains valid base64url characters
	for i, part := range parts {
		if part == "" {
			return fmt.Errorf("invalid token format: part %d is empty", i+1)
		}
		// Base64url uses: A-Z, a-z, 0-9, -, _, and optional padding =
		for _, char := range part {
			if !((char >= 'A' && char <= 'Z') ||
				(char >= 'a' && char <= 'z') ||
				(char >= '0' && char <= '9') ||
				char == '-' || char == '_' || char == '=') {
				return fmt.Errorf("invalid token format: contains invalid character '%c'", char)
			}
		}
	}

	return nil
}

// handleLogin prompts for and saves authentication token
func handleLogin(apiURL string) {
	// Determine API URL with priority: command-line flag > default
	if apiURL == "" {
		apiURL = getAPIURL("")
	}

	// Validate the URL for security (prevent phishing via terminal hyperlinks)
	if err := validateAPIURL(apiURL); err != nil {
		fmt.Fprintf(os.Stderr, "Error: Invalid API URL: %v\n", err)
		fmt.Fprintf(os.Stderr, "For local development, use: --url http://localhost:3000\n")
		os.Exit(1)
	}

	tokenURL := apiURL + "/my-transcripts"

	fmt.Println("AI Sessions CLI - Login")
	fmt.Println()

	// Create reader once and reuse it
	reader := bufio.NewReader(os.Stdin)

	// Show clickable URL and wait for user input
	clickableURL := makeClickableURL(tokenURL)
	fmt.Printf("Press Enter to open %s in your browser, or paste your token: ", clickableURL)

	// Wait for user to press Enter or paste a token
	line, _ := reader.ReadString('\n')
	firstInput := strings.TrimSpace(line)

	if firstInput == "" {
		// They just pressed Enter - open browser
		fmt.Println("\033[36mOpening browser...\033[0m")
		if err := openBrowser(tokenURL); err != nil {
			fmt.Printf("\033[33m⚠\033[0m  Could not open browser: %v\n", err)
			fmt.Printf("Please visit %s to generate your token.\n", clickableURL)
		}
	}

	// If we didn't get a token on the first input, ask for it
	var token string
	if firstInput != "" {
		token = firstInput
	} else {
		fmt.Println()
		fmt.Print("Enter your token: ")
		tokenInput, err := reader.ReadString('\n')
		if err != nil {
			fmt.Fprintf(os.Stderr, "\033[31m✗\033[0m Error reading token: %v\n", err)
			os.Exit(1)
		}
		token = strings.TrimSpace(tokenInput)
	}

	if token == "" {
		fmt.Fprintf(os.Stderr, "\033[31m✗\033[0m Token cannot be empty\n")
		os.Exit(1)
	}

	// Validate token format
	if err := validateTokenFormat(token); err != nil {
		fmt.Fprintf(os.Stderr, "\033[31m✗\033[0m %v\n", err)
		fmt.Fprintf(os.Stderr, "Please ensure you copied the entire token from %s\n", tokenURL)
		os.Exit(1)
	}

	// Save configuration
	config := Config{
		Token: token,
	}

	if err := saveConfig(config); err != nil {
		fmt.Fprintf(os.Stderr, "\033[31m✗\033[0m Error saving configuration: %v\n", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("\033[32m✓ Token saved successfully!\033[0m")
	fmt.Println()
	fmt.Println("You can now upload transcripts:")
	fmt.Println("  \033[36maisessions upload session.jsonl\033[0m")
}

// formatRelativeTime converts a timestamp to relative time (e.g., "2 hours ago", "yesterday")
func formatRelativeTime(t time.Time) string {
	now := time.Now()
	diff := now.Sub(t)

	if diff < time.Minute {
		return "just now"
	} else if diff < time.Hour {
		mins := int(diff.Minutes())
		if mins == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d mins ago", mins)
	} else if diff < 24*time.Hour {
		hours := int(diff.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	} else if diff < 48*time.Hour {
		return "yesterday"
	} else if diff < 7*24*time.Hour {
		days := int(diff.Hours() / 24)
		return fmt.Sprintf("%d days ago", days)
	} else {
		return t.Format("Jan 2")
	}
}

// getProjectName extracts a meaningful project path segment from the full path
// It removes the user's home directory prefix to create a shorter, more readable name.
func getProjectName(projectPath string) string {
	if projectPath == "" {
		return "(unknown project)"
	}

	homeDir, err := os.UserHomeDir()
	if err == nil {
		// Ensure homeDir has a trailing separator for correct trimming
		homeDirWithSeparator := homeDir + string(filepath.Separator)
		if strings.HasPrefix(projectPath, homeDirWithSeparator) {
			relativePath := strings.TrimPrefix(projectPath, homeDirWithSeparator)
			if relativePath == "" {
				return "(home)"
			}
			return relativePath
		}

		claudeRoot := filepath.Join(homeDir, ".claude", "projects") + string(filepath.Separator)
		if strings.HasPrefix(projectPath, claudeRoot) {
			return strings.TrimPrefix(projectPath, claudeRoot)
		}
	}

	// Fallback: return the path as-is
	return projectPath
}

// getAgentDisplayName returns a friendly name for the agent source.
func getAgentDisplayName(source string) string {
	switch source {
	case "claude":
		return "Claude Code"
	case "codex":
		return "Codex"
	default:
		if source == "" {
			return "Unknown"
		}
		return source
	}
}

// cleanFirstMessage trims and truncates the first message
func cleanFirstMessage(msg string, maxLen int) string {
	msg = strings.TrimSpace(msg)

	// Truncate with ellipsis if needed
	if len(msg) > maxLen {
		return msg[:maxLen-3] + "..."
	}
	return msg
}

// getTerminalWidth returns the terminal width, defaulting to 80 if unable to determine
func getTerminalWidth() int {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width < 40 {
		return 80 // Default width
	}
	return width
}

// truncateString truncates a string to maxLen with ellipsis at the end
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen < 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// truncateStringStart truncates a string to maxLen with ellipsis at the start
func truncateStringStart(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen < 3 {
		return s[len(s)-maxLen:]
	}
	return "..." + s[len(s)-(maxLen-3):]
}

// formatSessionRow formats a session as a table row
func formatSessionRow(s adapters.Session, width int) string {
	timeStr := formatRelativeTime(s.Timestamp)
	project := getProjectName(s.ProjectPath)
	agent := getAgentDisplayName(s.Source)
	userMsgCol := fmt.Sprintf("%d", s.UserMessageCount)

	// Calculate available space for message
	// prefix(2) + time(12) + agent(12) + userMsgs(5) + project(28) + spacing(10) = 69
	const fixedWidth = 69
	messageWidth := width - fixedWidth
	if messageWidth < 20 {
		messageWidth = 20 // Minimum message width
	}

	message := cleanFirstMessage(s.FirstMessage, messageWidth)

	// Use padding for alignment
	timeCol := truncateString(timeStr, 12)
	agentCol := truncateString(agent, 12)
	// For project names, truncate from the start (show the end with ellipsis at the start)
	projectCol := truncateStringStart(project, 28)

	return fmt.Sprintf("  %-12s  %-12s  %5s  %-28s  %s", timeCol, agentCol, userMsgCol, projectCol, message)
}

// formatTableHeader formats the table header row
func formatTableHeader() string {
	return fmt.Sprintf("  %-12s  %-12s  %5s  %-28s  %s", "TIME", "AGENT", "#USER", "PROJECT", "MESSAGE")
}

// selectSessionInteractively displays an interactive list of recent sessions
// and returns the file path of the selected session
func selectSessionInteractively() (string, error) {
	// Initialize Claude adapter
	claudeAdapter, err := adapters.NewClaudeAdapter()
	if err != nil {
		return "", fmt.Errorf("failed to initialize Claude adapter: %w", err)
	}

	// List recent sessions (limit to 50 per adapter)
	sessions, err := claudeAdapter.ListSessions("", 50)
	if err != nil {
		return "", fmt.Errorf("failed to list sessions: %w", err)
	}

	// Try to load Codex sessions (ignore errors to keep Claude flow working)
	if codexAdapter, codexErr := adapters.NewCodexAdapter(); codexErr == nil {
		if codexSessions, listErr := codexAdapter.ListSessions("", 50); listErr == nil {
			sessions = append(sessions, codexSessions...)
		}
	}

	if len(sessions) == 0 {
		return "", fmt.Errorf("no sessions found")
	}

	// Sort sessions by timestamp (newest first), putting zero timestamps last
	sort.SliceStable(sessions, func(i, j int) bool {
		ti := sessions[i].Timestamp
		tj := sessions[j].Timestamp

		if ti.IsZero() && tj.IsZero() {
			return sessions[i].FirstMessage > sessions[j].FirstMessage
		}
		if ti.IsZero() {
			return false
		}
		if tj.IsZero() {
			return true
		}
		return ti.After(tj)
	})

	// Limit to 50 sessions overall to keep the list manageable
	if len(sessions) > 50 {
		sessions = sessions[:50]
	}

	// Get terminal width for formatting
	termWidth := getTerminalWidth()

	// Filter sessions in-place to remove those with no user messages
	// This is more memory-efficient than creating a new slice
	n := 0
	for _, session := range sessions {
		if session.UserMessageCount > 0 {
			sessions[n] = session
			n++
		}
	}
	sessions = sessions[:n]

	if len(sessions) == 0 {
		return "", fmt.Errorf("no sessions with user messages found")
	}

	// Create display items from the filtered sessions
	items := make([]string, len(sessions))
	for i, session := range sessions {
		items[i] = formatSessionRow(session, termWidth)
	}

	// Print title
	fmt.Println()
	fmt.Println("Select a session to upload")
	fmt.Println("Use the arrow keys to navigate: ↓ ↑ → ←  and / toggles search")
	fmt.Println()
	fmt.Println("\033[2m" + formatTableHeader() + "\033[0m") // Dim color for header

	// Create templates
	templates := &promptui.SelectTemplates{
		Label:    `{{ "" }}`,               // Empty label to hide the prompt line
		Active:   "\033[36m{{ . }}\033[0m", // Cyan for active
		Inactive: "{{ . }}",
		Selected: "\033[32m{{ . }}\033[0m", // Green for selected
	}

	// Create the prompt
	prompt := promptui.Select{
		Label:     " ",
		Items:     items,
		Templates: templates,
		Size:      15,
		HideHelp:  true, // Hide default help since we show our own
		Searcher: func(input string, index int) bool {
			session := sessions[index]
			input = strings.ToLower(input)
			return strings.Contains(strings.ToLower(session.FirstMessage), input) ||
				strings.Contains(strings.ToLower(session.ProjectPath), input) ||
				strings.Contains(strings.ToLower(getAgentDisplayName(session.Source)), input)
		},
	}

	// Display and get selection
	idx, _, err := prompt.Run()
	if err != nil {
		return "", fmt.Errorf("selection cancelled: %w", err)
	}

	selectedSession := sessions[idx]

	// Clear the screen and show what was selected
	fmt.Print("\033[H\033[2J") // Clear screen
	fmt.Println()
	fmt.Println("Selected session:")
	fmt.Printf("  %s\n", selectedSession.FirstMessage)
	fmt.Printf("  Project: %s\n", getProjectName(selectedSession.ProjectPath))
	fmt.Printf("  Agent: %s\n", getAgentDisplayName(selectedSession.Source))
	fmt.Printf("  User messages: %d\n", selectedSession.UserMessageCount)
	fmt.Printf("  Time: %s\n", formatRelativeTime(selectedSession.Timestamp))
	fmt.Printf("  File: %s\n", filepath.Base(selectedSession.FilePath))
	fmt.Println()

	return selectedSession.FilePath, nil
}

// handleUploadCommand processes upload command arguments
func handleUploadCommand() {
	var filepath string
	var title string
	var apiURL string
	var fileProvided bool

	// Check if a file path is provided (not a flag)
	if len(os.Args) >= 3 && !strings.HasPrefix(os.Args[2], "--") {
		filepath = os.Args[2]
		fileProvided = true
	}

	// Parse optional flags
	startIdx := 3
	if !fileProvided {
		startIdx = 2 // Flags start at position 2 if no file provided
	}
	for i := startIdx; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--title":
			if i+1 >= len(os.Args) {
				fmt.Fprintf(os.Stderr, "Error: --title requires a value\n")
				os.Exit(1)
			}
			title = os.Args[i+1]
			i++
		case "--url":
			if i+1 >= len(os.Args) {
				fmt.Fprintf(os.Stderr, "Error: --url requires a value\n")
				os.Exit(1)
			}
			apiURL = os.Args[i+1]
			i++
		default:
			fmt.Fprintf(os.Stderr, "Unknown flag: %s\n", os.Args[i])
			os.Exit(1)
		}
	}

	// If no file was provided, show interactive selector
	if !fileProvided {
		selectedPath, err := selectSessionInteractively()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		filepath = selectedPath
	}

	// Load configuration
	config, err := loadConfig()
	if err != nil {
		// Not authenticated - start login flow automatically
		fmt.Println("Not authenticated. Let's set up your CLI access.")
		fmt.Println()
		handleLogin("")

		// Try loading config again after login
		config, err = loadConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: Failed to load configuration after login: %v\n", err)
			os.Exit(1)
		}
	}

	// Determine API URL (--url flag or default)
	finalAPIURL := getAPIURL("")
	if apiURL != "" {
		finalAPIURL = apiURL
	}

	// Perform upload
	if err := uploadFile(finalAPIURL, config.Token, filepath, title); err != nil {
		// Check if it's an authentication error (revoked/expired token)
		if _, ok := err.(*AuthError); ok {
			fmt.Println()
			fmt.Println("Your token has expired or been revoked. Let's re-authenticate.")
			fmt.Println()
			handleLogin("")

			fmt.Println()
			fmt.Println("Login successful! Please run your upload command again:")
			fmt.Printf("  aisessions upload %s", filepath)
			if title != "" {
				fmt.Printf(" --title \"%s\"", title)
			}
			fmt.Println()
			os.Exit(0)
		}

		// Error was already printed in uploadFile(), just exit
		os.Exit(1)
	}
}

// getConfigPath returns the path to the config file
func getConfigPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	configPath := filepath.Join(homeDir, configDir, configFile)
	return configPath, nil
}

// getAPIURL returns the API URL with priority: config > default
func getAPIURL(configURL string) string {
	// Use config value if set
	if configURL != "" {
		return configURL
	}

	// Fall back to default
	return defaultAPIURL
}

// loadConfig loads the configuration from disk
func loadConfig() (*Config, error) {
	configPath, err := getConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("config file not found (run 'aisessions login'): %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("invalid config file: %w", err)
	}

	if config.Token == "" {
		return nil, fmt.Errorf("config file is missing token")
	}

	return &config, nil
}

// saveConfig saves the configuration to disk
func saveConfig(config Config) error {
	configPath, err := getConfigPath()
	if err != nil {
		return err
	}

	// Create config directory if it doesn't exist
	configDirPath := filepath.Dir(configPath)
	if err := os.MkdirAll(configDirPath, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Marshal config to JSON
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write config file
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}
