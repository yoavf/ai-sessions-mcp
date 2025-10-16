package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/briandowns/spinner"
	"github.com/manifoldco/promptui"
)

// UploadRequest represents the request body for CLI upload
type UploadRequest struct {
	FileData string `json:"fileData"`
	Title    string `json:"title,omitempty"`
}

// UploadResponse represents the response from the upload endpoint
type UploadResponse struct {
	ID          string `json:"id"`
	SecretToken string `json:"secretToken"`
	URL         string `json:"url"`
}

// ErrorResponse represents an error response from the API
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

// AuthError represents an authentication error (revoked/expired token)
type AuthError struct {
	Message string
}

func (e *AuthError) Error() string {
	return e.Message
}

// uploadFile uploads a transcript file to the AI Sessions API
func uploadFile(apiURL, token, filePath, title string) error {
	// Read the file
	fileData, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Check file size (5MB limit)
	const maxSize = 5 * 1024 * 1024 // 5MB
	if len(fileData) > maxSize {
		fmt.Println()
		fmt.Printf("\033[31m✗ Error:\033[0m File size (%.2f MB) exceeds the 5MB limit\n", float64(len(fileData))/1024/1024)
		fmt.Println()
		return fmt.Errorf("file too large")
	}

	// Show data responsibility notice
	fmt.Println()
	fmt.Println("\033[33m⚠ Data Responsibility Notice\033[0m")
	fmt.Println("\033[2mYou are responsible for ensuring that the transcript does not contain")
	fmt.Println("private or sensitive information before uploading. While we scan for")
	fmt.Println("common patterns, you should review the content yourself.\033[0m")
	fmt.Println()

	// Confirm with promptui
	prompt := promptui.Prompt{
		Label:     "Continue with upload",
		IsConfirm: true,
	}

	_, err = prompt.Run()
	if err != nil {
		// This handles 'n', 'N', Ctrl+C, etc.
		return fmt.Errorf("upload cancelled")
	}

	// If no title provided, use filename without extension
	if title == "" {
		title = getDefaultTitle(filePath)
	}

	// Prepare request body
	uploadReq := UploadRequest{
		FileData: string(fileData),
		Title:    title,
	}

	requestBody, err := json.Marshal(uploadReq)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	uploadURL := apiURL + "/api/cli/upload"
	req, err := http.NewRequest("POST", uploadURL, bytes.NewReader(requestBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	// Create and start spinner
	fmt.Println()
	s := spinner.New(spinner.CharSets[14], 100*time.Millisecond)
	s.Suffix = fmt.Sprintf("  Uploading \033[36m%s\033[0m (%.2f KB)", filepath.Base(filePath), float64(len(fileData))/1024)
	s.Start()

	// Send request
	client := &http.Client{}
	resp, err := client.Do(req)

	// Stop spinner
	s.Stop()

	if err != nil {
		fmt.Println()
		fmt.Printf("\033[31m✗ Upload Failed:\033[0m %v\n", err)
		fmt.Println()
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	// Handle error responses
	if resp.StatusCode != http.StatusOK {
		var errResp ErrorResponse
		if err := json.Unmarshal(responseBody, &errResp); err == nil {
			// Special handling for authentication errors (401)
			if resp.StatusCode == http.StatusUnauthorized {
				fmt.Println()
				fmt.Printf("\033[31m✗ Authentication Error:\033[0m %s\n", errResp.Message)
				fmt.Println()
				return &AuthError{Message: fmt.Sprintf("%s: %s", errResp.Error, errResp.Message)}
			}
			fmt.Println()
			if errResp.Message != "" {
				fmt.Printf("\033[31m✗ Upload Failed:\033[0m %s: %s\n", errResp.Error, errResp.Message)
			} else {
				fmt.Printf("\033[31m✗ Upload Failed:\033[0m %s\n", errResp.Error)
			}
			fmt.Println()
			return fmt.Errorf("upload failed")
		}
		fmt.Println()
		fmt.Printf("\033[31m✗ Upload Failed:\033[0m Status %d: %s\n", resp.StatusCode, string(responseBody))
		fmt.Println()
		return fmt.Errorf("upload failed")
	}

	// Parse success response
	var uploadResp UploadResponse
	if err := json.Unmarshal(responseBody, &uploadResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Display success message
	fmt.Println()
	fmt.Println("\033[32m✓ Upload successful!\033[0m")
	fmt.Println()
	fmt.Printf("\033[2mView your transcript at:\033[0m\n")
	fmt.Printf("\033[36m%s\033[0m\n", uploadResp.URL)
	fmt.Println()

	return nil
}

// getDefaultTitle generates a default title from the file path
func getDefaultTitle(filePath string) string {
	filename := filepath.Base(filePath)
	ext := filepath.Ext(filename)
	if ext != "" {
		filename = filename[:len(filename)-len(ext)]
	}
	return filename
}
