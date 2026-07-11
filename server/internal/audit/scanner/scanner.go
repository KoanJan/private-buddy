// Package scanner provides file discovery and traversal for the audit tool.
// It walks the project directory tree, filtering by file extension and
// excluding third-party/generated directories.
package scanner

import (
	"os"
	"path/filepath"
	"strings"
)

// extensions lists the file extensions the audit tool can scan.
var extensions = map[string]bool{
	".go":  true,
	".ts":  true,
	".tsx": true,
}

// excludedDirs lists directories to skip during scanning.
// These contain third-party or generated code that should not be audited.
var excludedDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	".git":         true,
	"dist":         true,
	"dist-electron": true,
	"build":        true,
	"data":         true,
}

// ScanResult holds the result of scanning a single file.
type ScanResult struct {
	Path    string // Relative file path from project root
	Content []byte // Raw file content
}

// Scan walks the project directory and returns all source files matching
// the supported extensions, excluding third-party and generated directories.
// If modulePath is non-empty, only files under that path are scanned.
func Scan(root string, modulePath string) ([]ScanResult, error) {
	var results []ScanResult

	walkRoot := root
	if modulePath != "" {
		walkRoot = filepath.Join(root, modulePath)
	}

	err := filepath.Walk(walkRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip excluded directories
		if info.IsDir() {
			base := filepath.Base(path)
			if excludedDirs[base] {
				return filepath.SkipDir
			}
			return nil
		}

		// Only include supported file extensions
		ext := filepath.Ext(path)
		if !extensions[ext] {
			return nil
		}

		// Skip generated files (common Go convention)
		if strings.HasSuffix(path, "_gen.go") || strings.HasSuffix(path, ".generated.ts") {
			return nil
		}

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		results = append(results, ScanResult{
			Path:    relPath,
			Content: content,
		})
		return nil
	})

	return results, err
}
