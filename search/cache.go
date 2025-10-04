package search

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/yoavf/ai-sessions-mcp/adapters"
)

//go:embed schema.sql
var schemaSQL string

// Cache manages the search index and session cache
type Cache struct {
	db *sql.DB
}

// NewCache creates a new search cache with SQLite backend
func NewCache(dbPath string) (*Cache, error) {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Initialize schema
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return &Cache{db: db}, nil
}

// Close closes the database connection
func (c *Cache) Close() error {
	return c.db.Close()
}

// IndexSession indexes a session for searching
func (c *Cache) IndexSession(session adapters.Session, content string) error {
	tx, err := c.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Tokenize content
	tokens := Tokenize(content)
	termFreqs := TermFrequency(tokens)
	docLength := len(tokens)

	// Get file modification time
	fileInfo, err := os.Stat(session.FilePath)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	// Insert or update session metadata with content
	_, err = tx.Exec(`
		INSERT OR REPLACE INTO sessions
		(id, source, project_path, file_path, first_message, summary, timestamp, last_indexed, file_mtime, doc_length, content)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, session.ID, session.Source, session.ProjectPath, session.FilePath,
		session.FirstMessage, session.Summary, session.Timestamp.Unix(),
		time.Now().Unix(), fileInfo.ModTime().Unix(), docLength, content)

	if err != nil {
		return fmt.Errorf("failed to insert session: %w", err)
	}

	// Delete old term index entries for this session
	if _, err = tx.Exec("DELETE FROM term_index WHERE session_id = ?", session.ID); err != nil {
		return fmt.Errorf("failed to delete old term index: %w", err)
	}

	// Insert new term index entries
	stmt, err := tx.Prepare("INSERT INTO term_index (term, session_id, term_frequency) VALUES (?, ?, ?)")
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for term, freq := range termFreqs {
		if _, err = stmt.Exec(term, session.ID, freq); err != nil {
			return fmt.Errorf("failed to insert term: %w", err)
		}
	}

	// Update global stats
	if err := c.updateStats(tx); err != nil {
		return fmt.Errorf("failed to update stats: %w", err)
	}

	return tx.Commit()
}

// NeedsReindex checks if a session needs to be reindexed based on file modification time
func (c *Cache) NeedsReindex(sessionID string, filePath string) (bool, error) {
	var cachedMtime int64
	err := c.db.QueryRow("SELECT file_mtime FROM sessions WHERE id = ?", sessionID).Scan(&cachedMtime)

	if err == sql.ErrNoRows {
		return true, nil // Not indexed yet
	}
	if err != nil {
		return false, fmt.Errorf("failed to check cache: %w", err)
	}

	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return false, fmt.Errorf("failed to stat file: %w", err)
	}

	return fileInfo.ModTime().Unix() > cachedMtime, nil
}

// SearchResult represents a search result with score and matching snippet
type SearchResult struct {
	Session adapters.Session
	Score   float64
	Snippet string // Contextual snippet showing where the match occurred
}

// Search performs BM25-ranked search across indexed sessions
func (c *Cache) Search(query string, source string, projectPath string, limit int) ([]SearchResult, error) {
	queryTerms := Tokenize(query)
	if len(queryTerms) == 0 {
		return nil, fmt.Errorf("no valid search terms")
	}

	// Get global stats for BM25
	stats, err := c.getStats()
	if err != nil {
		return nil, err
	}

	scorer := NewBM25Scorer(stats.avgDocLength, stats.totalDocs)

	// Get document frequencies for query terms
	docFreqs, err := c.getDocumentFrequencies(queryTerms)
	if err != nil {
		return nil, err
	}

	// Build SQL query with filters - include content for snippet extraction
	sqlQuery := `
		SELECT DISTINCT s.id, s.source, s.project_path, s.file_path,
		       s.first_message, s.summary, s.timestamp, s.doc_length, s.content
		FROM sessions s
		JOIN term_index ti ON s.id = ti.session_id
		WHERE ti.term IN (`

	args := make([]interface{}, 0)
	for i, term := range queryTerms {
		if i > 0 {
			sqlQuery += ", "
		}
		sqlQuery += "?"
		args = append(args, term)
	}
	sqlQuery += ")"

	// Add filters
	if source != "" {
		sqlQuery += " AND s.source = ?"
		args = append(args, source)
	}
	if projectPath != "" {
		sqlQuery += " AND s.project_path = ?"
		args = append(args, projectPath)
	}

	rows, err := c.db.Query(sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to search: %w", err)
	}
	defer rows.Close()

	var results []SearchResult

	for rows.Next() {
		var session adapters.Session
		var timestampUnix int64
		var docLength int
		var content string

		err := rows.Scan(&session.ID, &session.Source, &session.ProjectPath,
			&session.FilePath, &session.FirstMessage, &session.Summary,
			&timestampUnix, &docLength, &content)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		session.Timestamp = time.Unix(timestampUnix, 0)

		// Get term frequencies for this document
		termFreqs, err := c.getTermFrequencies(session.ID, queryTerms)
		if err != nil {
			return nil, err
		}

		// Calculate BM25 score
		score := scorer.Score(queryTerms, termFreqs, docLength, docFreqs)

		// Extract snippet from cached content
		snippet := GetSnippet(content, queryTerms, 300)

		results = append(results, SearchResult{
			Session: session,
			Score:   score,
			Snippet: snippet,
		})
	}

	// Sort by score (descending)
	// We'll do this in-place using a simple bubble sort since results are typically small
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[i].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	// Apply limit
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// GetSnippet extracts a contextual snippet from content around the first occurrence of query terms
func GetSnippet(content string, queryTerms []string, maxLength int) string {
	if maxLength == 0 {
		maxLength = 300
	}

	contentLower := strings.ToLower(content)

	// Find the earliest position of any query term
	firstPos := len(content)
	matchedTerm := ""

	for _, term := range queryTerms {
		pos := strings.Index(contentLower, term)
		if pos != -1 && pos < firstPos {
			firstPos = pos
			matchedTerm = term
		}
	}

	// If no match found (shouldn't happen), return start of content
	if firstPos == len(content) {
		if len(content) <= maxLength {
			return content
		}
		return content[:maxLength] + "..."
	}

	// Calculate snippet boundaries
	halfLength := maxLength / 2
	start := firstPos - halfLength
	end := firstPos + len(matchedTerm) + halfLength

	// Adjust boundaries
	if start < 0 {
		start = 0
	}
	if end > len(content) {
		end = len(content)
	}

	// Try to start/end at word boundaries
	if start > 0 {
		// Look for space or newline before start
		for i := start; i > 0 && i > start-50; i-- {
			if content[i] == ' ' || content[i] == '\n' {
				start = i + 1
				break
			}
		}
	}

	if end < len(content) {
		// Look for space or newline after end
		for i := end; i < len(content) && i < end+50; i++ {
			if content[i] == ' ' || content[i] == '\n' {
				end = i
				break
			}
		}
	}

	snippet := content[start:end]

	// Add ellipsis if truncated
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(content) {
		snippet = snippet + "..."
	}

	return snippet
}

// getStats retrieves global search statistics
type searchStats struct {
	totalDocs    int
	avgDocLength float64
}

func (c *Cache) getStats() (*searchStats, error) {
	var totalDocs int
	var avgDocLength float64

	err := c.db.QueryRow("SELECT value FROM search_stats WHERE key = 'total_docs'").Scan(&totalDocs)
	if err != nil {
		return nil, fmt.Errorf("failed to get total_docs: %w", err)
	}

	err = c.db.QueryRow("SELECT value FROM search_stats WHERE key = 'avg_doc_length'").Scan(&avgDocLength)
	if err != nil {
		return nil, fmt.Errorf("failed to get avg_doc_length: %w", err)
	}

	return &searchStats{
		totalDocs:    totalDocs,
		avgDocLength: avgDocLength,
	}, nil
}

// updateStats recalculates and updates global statistics
func (c *Cache) updateStats(tx *sql.Tx) error {
	var totalDocs int
	var totalLength int64

	err := tx.QueryRow("SELECT COUNT(*), COALESCE(SUM(doc_length), 0) FROM sessions").Scan(&totalDocs, &totalLength)
	if err != nil {
		return fmt.Errorf("failed to calculate stats: %w", err)
	}

	avgDocLength := 0.0
	if totalDocs > 0 {
		avgDocLength = float64(totalLength) / float64(totalDocs)
	}

	if _, err = tx.Exec("UPDATE search_stats SET value = ? WHERE key = 'total_docs'", totalDocs); err != nil {
		return err
	}

	if _, err = tx.Exec("UPDATE search_stats SET value = ? WHERE key = 'avg_doc_length'", avgDocLength); err != nil {
		return err
	}

	return nil
}

// getDocumentFrequencies returns the number of documents containing each term
func (c *Cache) getDocumentFrequencies(terms []string) (map[string]int, error) {
	freqs := make(map[string]int)

	query := "SELECT term, COUNT(DISTINCT session_id) FROM term_index WHERE term IN ("
	args := make([]interface{}, len(terms))
	for i, term := range terms {
		if i > 0 {
			query += ", "
		}
		query += "?"
		args[i] = term
	}
	query += ") GROUP BY term"

	rows, err := c.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get document frequencies: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var term string
		var count int
		if err := rows.Scan(&term, &count); err != nil {
			return nil, err
		}
		freqs[term] = count
	}

	return freqs, nil
}

// getTermFrequencies returns term frequencies for a specific document
func (c *Cache) getTermFrequencies(sessionID string, terms []string) (map[string]int, error) {
	freqs := make(map[string]int)

	query := "SELECT term, term_frequency FROM term_index WHERE session_id = ? AND term IN ("
	args := []interface{}{sessionID}
	for i, term := range terms {
		if i > 0 {
			query += ", "
		}
		query += "?"
		args = append(args, term)
	}
	query += ")"

	rows, err := c.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get term frequencies: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var term string
		var freq int
		if err := rows.Scan(&term, &freq); err != nil {
			return nil, err
		}
		freqs[term] = freq
	}

	return freqs, nil
}
