// Package checker: string_enum.go detects enum-like types defined with
// string underlying type. Constitution Principle III requires all enums
// to use int, not string.
package checker

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
)

// StringEnumChecker detects Go type declarations where the underlying type
// is string and the type name suggests enum-like usage (used in model structs).
type StringEnumChecker struct{}

// NewStringEnumChecker creates a new StringEnumChecker.
func NewStringEnumChecker() *StringEnumChecker {
	return &StringEnumChecker{}
}

// Name returns the checker identifier.
func (c *StringEnumChecker) Name() string { return "string_enum" }

// Type returns CheckStringEnum.
func (c *StringEnumChecker) Type() CheckType { return CheckStringEnum }

// Accept handles .go files only.
func (c *StringEnumChecker) Accept(ext string) bool { return ext == ".go" }

// Check scans a Go source file for string-based enum type declarations.
func (c *StringEnumChecker) Check(filePath string, content []byte) ([]Finding, error) {
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

			// Check if the underlying type is string
			ident, ok := typeSpec.Type.(*ast.Ident)
			if !ok || ident.Name != "string" {
				continue
			}

			// Report the violation
			line := fset.Position(typeSpec.Pos()).Line
			findings = append(findings, Finding{
				File:       filePath,
				Line:       line,
				Check:      CheckStringEnum,
				Severity:   SevError,
				Symbol:     typeSpec.Name.Name,
				Message:    fmt.Sprintf("enum-like type '%s' defined as string — enums must use int per constitution", typeSpec.Name.Name),
				Suggestion: fmt.Sprintf("change '%s' to int type and use iota for values", typeSpec.Name.Name),
			})
		}
	}

	return findings, nil
}
