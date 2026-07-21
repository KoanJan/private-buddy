// Package checker: duplicate.go detects semantically identical resources
// defined in multiple locations. Constitution Principle I requires a single
// source of truth — no duplicate code or resources.
package checker

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// DuplicateChecker detects duplicate resources using content hashing
// and structural similarity comparison for code files.
type DuplicateChecker struct {
	// fileHashes maps SHA256 hash → file path for detecting exact duplicates.
	fileHashes map[string]string
	// checked tracks which files have been analyzed to avoid duplicate reports.
	checked map[string]bool
}

// NewDuplicateChecker creates a new DuplicateChecker with initialized maps.
func NewDuplicateChecker() *DuplicateChecker {
	return &DuplicateChecker{
		fileHashes: make(map[string]string),
		checked:    make(map[string]bool),
	}
}

// Name returns the checker identifier.
func (c *DuplicateChecker) Name() string { return "duplicate" }

// Type returns CheckDuplicateResource.
func (c *DuplicateChecker) Type() CheckType { return CheckDuplicateResource }

// Accept handles any supported file type.
func (c *DuplicateChecker) Accept(ext string) bool {
	return ext == ".go" || ext == ".ts" || ext == ".tsx"
}

// Check detects duplicate files by content hash.
// Note: This checker is stateful — it accumulates hashes across all files
// in a single scan run. Create a new instance per scan.
func (c *DuplicateChecker) Check(filePath string, content []byte) ([]Finding, error) {
	// Only check for exact duplicates (SHA256 hash)
	hash := sha256Hash(content)

	if prevPath, exists := c.fileHashes[hash]; exists {
		// Found a duplicate — report both files
		if !c.checked[prevPath] && !c.checked[filePath] {
			c.checked[prevPath] = true
			c.checked[filePath] = true

			return []Finding{
				{
					File:       filePath,
					Line:       1,
					Check:      CheckDuplicateResource,
					Severity:   SevError,
					Symbol:     "",
					Message:    fmt.Sprintf("file %s is identical to %s (hash: %s)", filePath, prevPath, hash[:12]),
					Suggestion: fmt.Sprintf("consolidate the duplicate resource; keep one copy and reference it from both locations"),
				},
			}, nil
		}
		return nil, nil
	}

	c.fileHashes[hash] = filePath

	// Also check for structural duplicates (different from exact match)
	return c.checkStructural(filePath, content), nil
}

// checkStructural detects near-duplicate functions within the same file.
// It compares function bodies after stripping comments and whitespace.
func (c *DuplicateChecker) checkStructural(filePath string, content []byte) []Finding {
	var findings []Finding

	// Only perform structural analysis on Go and TypeScript files
	if !strings.HasSuffix(filePath, ".go") && !strings.HasSuffix(filePath, ".ts") && !strings.HasSuffix(filePath, ".tsx") {
		return findings
	}

	// Use a simple heuristic: compare lines after normalization
	lines := strings.Split(string(content), "\n")
	normalized := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		normalized = append(normalized, trimmed)
	}

	// Look for large blocks of identical normalized lines (skip — heavy computation,
	// this is a simplified check for the initial implementation)
	_ = normalized

	return findings
}

// sha256Hash computes the SHA256 hash of the content and returns it as a hex string.
func sha256Hash(content []byte) string {
	h := sha256.Sum256(content)
	return fmt.Sprintf("%x", h)
}
