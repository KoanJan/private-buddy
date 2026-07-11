// Package checker: comment.go implements the missing English comment checker.
// It uses Go AST to detect exported symbols without doc comments.
// For TypeScript files, regex-based JSDoc detection is used.
package checker

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// CommentChecker detects exported Go symbols (types, functions, methods, variables,
// constants) that lack an English doc comment. Constitution Principle II requires
// English comments on all exported symbols.
type CommentChecker struct{}

// NewCommentChecker creates a CommentChecker.
func NewCommentChecker() *CommentChecker {
	return &CommentChecker{}
}

// Name returns the checker identifier.
func (c *CommentChecker) Name() string { return "comment" }

// Type returns CheckMissingComment.
func (c *CommentChecker) Type() CheckType { return CheckMissingComment }

// Accept handles .go, .ts, and .tsx files.
func (c *CommentChecker) Accept(ext string) bool {
	return ext == ".go" || ext == ".ts" || ext == ".tsx"
}

// Check scans a source file for exported symbols without English doc comments.
func (c *CommentChecker) Check(filePath string, content []byte) ([]Finding, error) {
	if strings.HasSuffix(filePath, ".ts") || strings.HasSuffix(filePath, ".tsx") {
		return c.checkTS(filePath, content), nil
	}
	return c.checkGo(filePath, content)
}

// checkGo uses Go AST to detect undocumented exported symbols.
func (c *CommentChecker) checkGo(filePath string, content []byte) ([]Finding, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filePath, content, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	var findings []Finding
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			c.checkGenDecl(fset, filePath, d, &findings)
		case *ast.FuncDecl:
			c.checkFuncDecl(fset, filePath, d, &findings)
		}
	}
	return findings, nil
}

// isExported returns true if the name starts with an uppercase letter.
func isExported(name string) bool {
	return len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z'
}

// checkGenDecl checks a general declaration (type, const, var) for documentation.
// A group-level doc comment on the GenDecl covers all individual specs in the block.
func (c *CommentChecker) checkGenDecl(fset *token.FileSet, filePath string, d *ast.GenDecl, findings *[]Finding) {
	if d.Doc != nil && len(d.Doc.List) > 0 {
		// Group-level doc comment present — covers all specs
		return
	}

	for _, spec := range d.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			// Check for spec-level doc comment
			if s.Doc != nil && len(s.Doc.List) > 0 {
				continue
			}
			if isExported(s.Name.Name) {
				line := fset.Position(s.Pos()).Line
				*findings = append(*findings, Finding{
					File:       filePath,
					Line:       line,
					Check:      CheckMissingComment,
					Severity:   SevError,
					Symbol:     s.Name.Name,
					Message:    fmt.Sprintf("exported type '%s' missing English doc comment", s.Name.Name),
					Suggestion: fmt.Sprintf("add a doc comment starting with '// %s ...'", s.Name.Name),
				})
			}
		case *ast.ValueSpec:
			// Check for spec-level doc comment
			if s.Doc != nil && len(s.Doc.List) > 0 {
				continue
			}
			for _, name := range s.Names {
				if isExported(name.Name) {
					line := fset.Position(name.Pos()).Line
					*findings = append(*findings, Finding{
						File:       filePath,
						Line:       line,
						Check:      CheckMissingComment,
						Severity:   SevError,
						Symbol:     name.Name,
						Message:    fmt.Sprintf("exported var/const '%s' missing English doc comment", name.Name),
						Suggestion: fmt.Sprintf("add a doc comment starting with '// %s ...'", name.Name),
					})
				}
			}
		}
	}
}

// checkFuncDecl checks a function declaration for documentation.
func (c *CommentChecker) checkFuncDecl(fset *token.FileSet, filePath string, d *ast.FuncDecl, findings *[]Finding) {
	if d.Doc != nil && len(d.Doc.List) > 0 {
		return
	}

	// Only check exported functions and methods
	name := d.Name.Name
	if !isExported(name) {
		return
	}

	// Skip init() and main() functions
	if name == "init" || name == "main" {
		return
	}

	line := fset.Position(d.Pos()).Line

	// Format the symbol to show whether it's a function or method
	symbol := name
	if d.Recv != nil && len(d.Recv.List) > 0 {
		recvType := receiverTypeName(d.Recv.List[0])
		if recvType != "" {
			symbol = fmt.Sprintf("%s.%s", recvType, name)
		}
	}

	*findings = append(*findings, Finding{
		File:       filePath,
		Line:       line,
		Check:      CheckMissingComment,
		Severity:   SevError,
		Symbol:     symbol,
		Message:    fmt.Sprintf("exported function '%s' missing English doc comment", symbol),
		Suggestion: fmt.Sprintf("add a doc comment starting with '// %s ...'", name),
	})
}

// receiverTypeName extracts the type name from a receiver field list.
func receiverTypeName(field *ast.Field) string {
	if field == nil || field.Type == nil {
		return ""
	}
	switch t := field.Type.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		if ident, ok := t.X.(*ast.Ident); ok {
			return ident.Name
		}
	}
	return ""
}

// checkTS detects TypeScript exported functions/components without JSDoc comments.
// Uses regex-based heuristics since we don't have a full TS AST parser.
func (c *CommentChecker) checkTS(filePath string, content []byte) []Finding {
	var findings []Finding
	lines := strings.Split(string(content), "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Detect export const/function/class patterns
		if strings.HasPrefix(trimmed, "export const ") ||
			strings.HasPrefix(trimmed, "export function ") ||
			strings.HasPrefix(trimmed, "export class ") ||
			strings.HasPrefix(trimmed, "export default function ") ||
			strings.HasPrefix(trimmed, "export default class ") {
			// Check if the previous line(s) contain a JSDoc comment
			if !c.hasPrecedingComment(lines, i) {
				// Extract the symbol name
				symbol := c.extractTSExportName(trimmed)
				findings = append(findings, Finding{
					File:       filePath,
					Line:       i + 1,
					Check:      CheckMissingComment,
					Severity:   SevWarning,
					Symbol:     symbol,
					Message:    fmt.Sprintf("exported TS symbol '%s' missing JSDoc comment", symbol),
					Suggestion: fmt.Sprintf("add a JSDoc comment block: /** ... */ before the export"),
				})
			}
		}
	}

	return findings
}

// hasPrecedingComment checks if lineIndex-1 or lineIndex-2 contains a JSDoc comment.
func (c *CommentChecker) hasPrecedingComment(lines []string, lineIndex int) bool {
	maxCheck := 3 // Check up to 3 lines before the export
	for offset := 1; offset <= maxCheck && lineIndex-offset >= 0; offset++ {
		prev := strings.TrimSpace(lines[lineIndex-offset])
		if prev == "" {
			continue // Skip blank lines
		}
		// JSDoc starts with /** and ends with */
		return strings.HasPrefix(prev, "/**") || strings.HasPrefix(prev, "*") || strings.HasPrefix(prev, "*/")
	}
	return false
}

// extractTSExportName extracts the symbol name from a TypeScript export statement.
func (c *CommentChecker) extractTSExportName(line string) string {
	line = strings.TrimPrefix(line, "export default ")
	line = strings.TrimPrefix(line, "export ")

	// Split by type keyword
	for _, kw := range []string{"const ", "function ", "class ", "interface ", "type "} {
		if strings.HasPrefix(line, kw) {
			rest := strings.TrimPrefix(line, kw)
			// Get the identifier (up to first space, (, <, or :)
			for i, ch := range rest {
				if ch == ' ' || ch == '(' || ch == '<' || ch == '{' || ch == ':' {
					return rest[:i]
				}
			}
			return rest
		}
	}
	return line
}
