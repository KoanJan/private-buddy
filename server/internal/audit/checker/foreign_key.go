// Package checker: foreign_key.go detects database-level foreign key
// constraints in GORM struct tags. Constitution Principle III prohibits
// cross-table database-level constraints — referential integrity must be
// enforced at the application layer.
package checker

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// ForeignKeyChecker detects foreign key constraints in GORM struct field tags.
type ForeignKeyChecker struct{}

// NewForeignKeyChecker creates a new ForeignKeyChecker.
func NewForeignKeyChecker() *ForeignKeyChecker {
	return &ForeignKeyChecker{}
}

// Name returns the checker identifier.
func (c *ForeignKeyChecker) Name() string { return "foreign_key" }

// Type returns CheckForeignKey.
func (c *ForeignKeyChecker) Type() CheckType { return CheckForeignKey }

// Accept handles .go files only.
func (c *ForeignKeyChecker) Accept(ext string) bool { return ext == ".go" }

// Check scans a Go source file for GORM foreign key constraints.
func (c *ForeignKeyChecker) Check(filePath string, content []byte) ([]Finding, error) {
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

			for _, field := range structType.Fields.List {
				if field.Tag == nil {
					continue
				}

				tag := field.Tag.Value
				if !strings.Contains(tag, "gorm:") {
					continue
				}

				gormTag := extractGormTag(tag)
				if gormTag == "" {
					continue
				}

				// Check for foreign key constraints (case-insensitive)
				lowerTag := strings.ToLower(gormTag)
				hasFK := strings.Contains(lowerTag, "foreignkey") ||
					strings.Contains(lowerTag, "references") ||
					strings.Contains(lowerTag, "constraint") ||
					strings.Contains(lowerTag, "foreignkey:")

				if !hasFK {
					continue
				}

				for _, name := range field.Names {
					line := fset.Position(field.Pos()).Line
					findings = append(findings, Finding{
						File:       filePath,
						Line:       line,
						Check:      CheckForeignKey,
						Severity:   SevError,
						Symbol:     fmt.Sprintf("%s.%s", typeSpec.Name.Name, name.Name),
						Message:    fmt.Sprintf("field '%s.%s' has a foreign key constraint in gorm tag — prohibited by constitution", typeSpec.Name.Name, name.Name),
						Suggestion: fmt.Sprintf("remove the foreign key constraint and enforce referential integrity at the application layer"),
					})
				}
			}
		}
	}

	return findings, nil
}
