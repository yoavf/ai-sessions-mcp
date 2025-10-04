// Package search provides BM25-based full-text search and indexing for AI assistant sessions.
package search

import (
	"math"
	"strings"
	"unicode"
)

// BM25 parameters (standard values)
const (
	k1 = 1.5  // Term frequency saturation
	b  = 0.75 // Length normalization
)

// BM25Scorer calculates relevance scores using the BM25 algorithm
type BM25Scorer struct {
	avgDocLength float64
	totalDocs    int
}

// NewBM25Scorer creates a new BM25 scorer with corpus statistics
func NewBM25Scorer(avgDocLength float64, totalDocs int) *BM25Scorer {
	return &BM25Scorer{
		avgDocLength: avgDocLength,
		totalDocs:    totalDocs,
	}
}

// Score calculates BM25 score for a document given query terms
// termFreqs: map of term -> frequency in document
// docLength: total number of terms in document
// docFreqs: map of term -> number of documents containing term
func (s *BM25Scorer) Score(queryTerms []string, termFreqs map[string]int, docLength int, docFreqs map[string]int) float64 {
	score := 0.0

	for _, term := range queryTerms {
		tf := float64(termFreqs[term])
		if tf == 0 {
			continue
		}

		df := float64(docFreqs[term])
		if df == 0 {
			continue
		}

		// IDF calculation: log((N - df + 0.5) / (df + 0.5))
		idf := math.Log((float64(s.totalDocs) - df + 0.5) / (df + 0.5))

		// TF normalization with length penalty
		tfNorm := (tf * (k1 + 1)) / (tf + k1*(1-b+b*float64(docLength)/s.avgDocLength))

		score += idf * tfNorm
	}

	return score
}

// Tokenize converts text to normalized tokens for indexing/searching
func Tokenize(text string) []string {
	text = strings.ToLower(text)

	var tokens []string
	var current strings.Builder

	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
		} else {
			if current.Len() > 0 {
				token := current.String()
				// Skip very short tokens (stopwords handled implicitly)
				if len(token) > 1 {
					tokens = append(tokens, token)
				}
				current.Reset()
			}
		}
	}

	// Don't forget last token
	if current.Len() > 0 {
		token := current.String()
		if len(token) > 1 {
			tokens = append(tokens, token)
		}
	}

	return tokens
}

// TermFrequency counts occurrences of each term in tokens
func TermFrequency(tokens []string) map[string]int {
	freqs := make(map[string]int)
	for _, token := range tokens {
		freqs[token]++
	}
	return freqs
}
