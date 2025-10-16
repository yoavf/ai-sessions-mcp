package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	Token  string `json:"token"`
	APIURL string `json:"apiUrl"`
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
		handleLogin()
	case "upload":
		handleUploadCommand()
	case "version", "-v", "--version":
		fmt.Println("aisessions version 1.0.0")
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

Environment Variables:
  AISESSIONS_API_URL    Override API URL (e.g., http://localhost:3000)

Examples:
  aisessions login
  aisessions upload session.jsonl
  aisessions upload session.jsonl --title "Bug Fix Session"

  # Development mode (use local server)
  export AISESSIONS_API_URL=http://localhost:3000
  aisessions upload session.jsonl

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

// handleLogin prompts for and saves authentication token
func handleLogin() {
	// Determine API URL (respects AISESSIONS_API_URL env var)
	apiURL := getAPIURL("")
	tokenURL := apiURL + "/my-transcripts"

	fmt.Println("AI Sessions CLI - Login")
	fmt.Println()

	// Create reader once and reuse it
	reader := bufio.NewReader(os.Stdin)

	// Show clickable URL and wait for Enter or timeout
	clickableURL := makeClickableURL(tokenURL)
	fmt.Printf("Press Enter to open %s in your browser: ", clickableURL)

	// Channel to receive user input
	inputChan := make(chan string)

	// Goroutine to wait for Enter key
	go func() {
		line, _ := reader.ReadString('\n')
		inputChan <- line
	}()

	// Wait for Enter or 5 second timeout
	var firstInput string
	select {
	case firstInput = <-inputChan:
		// User pressed Enter - clear line and show browser opening message
		fmt.Print("\r\033[K") // Clear current line
		firstInput = strings.TrimSpace(firstInput)

		// If they entered a token directly, use it
		if firstInput != "" {
			// They pasted the token directly
			fmt.Printf("Visit %s to generate your token.\n", clickableURL)
			fmt.Println()
			fmt.Println("Got it! Using your token...")
		} else {
			// They just pressed Enter - open browser
			fmt.Println("Opening browser...")
			if err := openBrowser(tokenURL); err != nil {
				fmt.Printf("Could not open browser: %v\n", err)
				fmt.Printf("Visit %s to generate your token.\n", clickableURL)
			}
		}
	case <-time.After(5 * time.Second):
		// Timeout - clear line and show visit message
		fmt.Print("\r\033[K") // Clear current line
		fmt.Printf("Visit %s to generate your token.\n", clickableURL)
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
			fmt.Fprintf(os.Stderr, "Error reading token: %v\n", err)
			os.Exit(1)
		}
		token = strings.TrimSpace(tokenInput)
	}

	if token == "" {
		fmt.Fprintf(os.Stderr, "Token cannot be empty\n")
		os.Exit(1)
	}

	// Optionally prompt for API URL (though env var is preferred for dev)
	fmt.Print("API URL (press Enter for default, or use AISESSIONS_API_URL env): ")
	configAPIURL, err2 := reader.ReadString('\n')
	if err2 != nil {
		fmt.Fprintf(os.Stderr, "Error reading API URL: %v\n", err2)
		os.Exit(1)
	}

	configAPIURL = strings.TrimSpace(configAPIURL)
	// Don't set apiURL in config if empty - let getAPIURL handle env var priority
	if configAPIURL == "" {
		configAPIURL = ""
	}

	// Save configuration
	config := Config{
		Token:  token,
		APIURL: configAPIURL,
	}

	if err := saveConfig(config); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving configuration: %v\n", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("✓ Token saved successfully!")
	fmt.Println()
	fmt.Println("You can now upload transcripts:")
	fmt.Println("  aisessions upload session.jsonl")
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
// It removes the "Users/[username]/" prefix and returns the rest
func getProjectName(projectPath string) string {
	// For paths like "Users/yoavfarhi/dev/ai-sessions-mcp", we want "dev/ai-sessions-mcp"
	// Find the pattern "Users/<username>/" and remove it

	// First, try to find the username pattern
	// Look for "Users/" followed by anything up to the next "/"
	if idx := strings.Index(projectPath, "Users/"); idx != -1 {
		// Find the end of the username (next "/" after "Users/")
		remaining := projectPath[idx+6:] // Skip "Users/"
		if slashIdx := strings.Index(remaining, "/"); slashIdx != -1 {
			// Get everything after "Users/[username]/"
			result := remaining[slashIdx+1:]
			// Convert slashes back to dashes for display
			return strings.ReplaceAll(result, "/", "-")
		}
	}

	// Fallback: convert slashes to dashes and use the base name
	return strings.ReplaceAll(filepath.Base(projectPath), "/", "-")
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

	// Calculate available space for message
	// prefix(2) + time(14) + project(42) + spacing(8) = 66
	const fixedWidth = 66
	messageWidth := width - fixedWidth
	if messageWidth < 20 {
		messageWidth = 20 // Minimum message width
	}

	message := cleanFirstMessage(s.FirstMessage, messageWidth)

	// Use padding for alignment
	timeCol := truncateString(timeStr, 12)
	// For project names, truncate from the start (show the end with ellipsis at the start)
	projectCol := truncateStringStart(project, 40)

	return fmt.Sprintf("  %-12s    %-40s    %s", timeCol, projectCol, message)
}

// formatTableHeader formats the table header row
func formatTableHeader() string {
	return fmt.Sprintf("  %-12s    %-40s    %s", "TIME", "PROJECT", "MESSAGE")
}

// selectSessionInteractively displays an interactive list of recent Claude Code sessions
// and returns the file path of the selected session
func selectSessionInteractively() (string, error) {
	// Initialize Claude adapter
	claudeAdapter, err := adapters.NewClaudeAdapter()
	if err != nil {
		return "", fmt.Errorf("failed to initialize Claude adapter: %w", err)
	}

	// List recent sessions (limit to 50)
	sessions, err := claudeAdapter.ListSessions("", 50)
	if err != nil {
		return "", fmt.Errorf("failed to list sessions: %w", err)
	}

	if len(sessions) == 0 {
		return "", fmt.Errorf("no Claude Code sessions found")
	}

	// Get terminal width for formatting
	termWidth := getTerminalWidth()

	// Create display items
	items := make([]string, len(sessions))
	for i, session := range sessions {
		items[i] = formatSessionRow(session, termWidth)
	}

	// Print title
	fmt.Println()
	fmt.Println("Select a Claude Code session to upload")
	fmt.Println("Use the arrow keys to navigate: ↓ ↑ → ←  and / toggles search")
	fmt.Println()
	fmt.Println("\033[2m" + formatTableHeader() + "\033[0m") // Dim color for header

	// Create templates
	templates := &promptui.SelectTemplates{
		Label:    `{{ "" }}`, // Empty label to hide the prompt line
		Active:   "\033[36m{{ . }}\033[0m",  // Cyan for active
		Inactive: "{{ . }}",
		Selected: "\033[32m{{ . }}\033[0m",  // Green for selected
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
				strings.Contains(strings.ToLower(session.ProjectPath), input)
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
		handleLogin()

		// Try loading config again after login
		config, err = loadConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: Failed to load configuration after login: %v\n", err)
			os.Exit(1)
		}
	}

	// Override API URL if provided
	if apiURL != "" {
		config.APIURL = apiURL
	}

	// Perform upload
	if err := uploadFile(config.APIURL, config.Token, filepath, title); err != nil {
		// Check if it's an authentication error (revoked/expired token)
		if _, ok := err.(*AuthError); ok {
			fmt.Println()
			fmt.Println("Your token has expired or been revoked. Let's re-authenticate.")
			fmt.Println()
			handleLogin()

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

// getAPIURL returns the API URL with priority: env var > config > default
func getAPIURL(configURL string) string {
	// 1. Check environment variable first (for development)
	if envURL := os.Getenv("AISESSIONS_API_URL"); envURL != "" {
		return envURL
	}

	// 2. Use config value if set
	if configURL != "" {
		return configURL
	}

	// 3. Fall back to default
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

	// Apply API URL priority: env var > config > default
	config.APIURL = getAPIURL(config.APIURL)

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
