// Package checker: nullable.go detects database model struct fields that
// lack the NOT NULL constraint in their GORM tags. Constitution Principle III
// requires all database fields to be NOT NULL — nullability is prohibited.
package checker

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// NullableChecker detects GORM model struct fields missing the NOT NULL
// constraint in their gorm struct tag.
type NullableChecker struct{}

// NewNullableChecker creates a new NullableChecker.
func NewNullableChecker() *NullableChecker {
	return &NullableChecker{}
}

// Name returns the checker identifier.
func (c *NullableChecker) Name() string { return "nullable" }

// Type returns CheckNullableField.
func (c *NullableChecker) Type() CheckType { return CheckNullableField }

// Accept handles .go files only.
func (c *NullableChecker) Accept(ext string) bool { return ext == ".go" }

// Check scans a Go source file for model structs with nullable fields.
func (c *NullableChecker) Check(filePath string, content []byte) ([]Finding, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filePath, content, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	var findings []Finding

	for _, decl := range f.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}

		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}

			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}

			// Check each field in the struct
			for _, field := range structType.Fields.List {
				if field.Tag == nil {
					continue
				}

				tag := field.Tag.Value
				// Check if this field has a gorm tag
				if !strings.Contains(tag, "gorm:") {
					continue
				}

				// Extract gorm tag content and check for "not null"
				gormTag := extractGormTag(tag)
				if gormTag == "" {
					continue
				}

				hasNotNull := strings.Contains(strings.ToLower(gormTag), "not null")

				// Skip fields that already have NOT NULL
				if hasNotNull {
					continue
				}

				// Check if the field type is potentially nullable
				// Skip fields that are primary keys (they implicitly have NOT NULL in GORM)
				if strings.Contains(strings.ToLower(gormTag), "primarykey") {
					continue
				}

				// Skip pointer fields — they are explicitly nullable by design
				if isPointerField(field) {
					continue
				}

				// Report the violation
				for _, name := range field.Names {
					line := fset.Position(field.Pos()).Line
					findings = append(findings, Finding{
						File:       filePath,
						Line:       line,
						Check:      CheckNullableField,
						Severity:   SevError,
						Symbol:     fmt.Sprintf("%s.%s", typeSpec.Name.Name, name.Name),
						Message:    fmt.Sprintf("field '%s.%s' missing 'not null' in gorm tag", typeSpec.Name.Name, name.Name),
						Suggestion: fmt.Sprintf(`add "not null" to the gorm tag: gorm:"not null;..."`),
					})
				}
			}
		}
	}

	return findings, nil
}

// extractGormTag extracts the gorm tag value from a struct field tag.
// For example: `gorm:"column:name;not null"` → "column:name;not null"
func extractGormTag(tagStr string) string {
	// The tag string from go/ast includes backticks: `json:"name" gorm:"column:name"`
	// Strip the outer backticks first
	tagStr = strings.Trim(tagStr, "`")

	// Find the gorm:"..." segment
	gormIdx := strings.Index(tagStr, `gorm:"`)
	if gormIdx == -1 {
		return ""
	}

	// Extract everything between the quotes
	start := gormIdx + len(`gorm:"`)
	remaining := tagStr[start:]

	// Find the closing quote
	end := strings.Index(remaining, `"`)
	if end == -1 {
		return remaining
	}

	return remaining[:end]
}

// isPointerField checks if a struct field has a pointer type (e.g., *string).
// Pointer fields are intentionally nullable and should be excluded from NOT NULL checks.
func isPointerField(field *ast.Field) bool {
	_, isStar := field.Type.(*ast.StarExpr)
	return isStar
}
