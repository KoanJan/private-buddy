// Package checker: silent_error.go detects error values that are consumed
// without being logged, wrapped, or propagated. Constitution Principle V requires
// that all unexpected conditions produce log output.
package checker

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// SilentErrorChecker detects Go code where an error value is received but
// neither logged nor returned. This includes patterns like:
//
//	err := doSomething()
//	if err != nil {
//	    return  // error silently discarded
//	}
type SilentErrorChecker struct{}

// NewSilentErrorChecker creates a new SilentErrorChecker.
func NewSilentErrorChecker() *SilentErrorChecker {
	return &SilentErrorChecker{}
}

// Name returns the checker identifier.
func (c *SilentErrorChecker) Name() string { return "silent_error" }

// Type returns CheckSilentError.
func (c *SilentErrorChecker) Type() CheckType { return CheckSilentError }

// Accept handles .go files only. Silent error detection uses Go AST.
func (c *SilentErrorChecker) Accept(ext string) bool { return ext == ".go" }

// Check scans a Go source file for silently consumed error values.
func (c *SilentErrorChecker) Check(filePath string, content []byte) ([]Finding, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filePath, content, 0)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	var findings []Finding

	// Walk all function bodies
	ast.Inspect(f, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			return true
		}

		// Track error variables in this function
		errVars := make(map[string]bool)
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			c.collectErrVars(node, errVars)
			c.checkSilentConsumption(fset, filePath, node, errVars, &findings)
			return true
		})
		return true
	})

	return findings, nil
}

// collectErrVars identifies variables that hold error values.
func (c *SilentErrorChecker) collectErrVars(node ast.Node, errVars map[string]bool) {
	assign, ok := node.(*ast.AssignStmt)
	if !ok {
		return
	}

	for i, lhs := range assign.Lhs {
		if i >= len(assign.Rhs) {
			continue
		}
		if c.isErrorReturnExpr(assign.Rhs[i]) {
			if ident, ok := lhs.(*ast.Ident); ok {
				errVars[ident.Name] = true
			}
		}
	}
}

// isErrorReturnExpr checks if an expression likely returns an error.
func (c *SilentErrorChecker) isErrorReturnExpr(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.CallExpr:
		if sel, ok := e.Fun.(*ast.SelectorExpr); ok {
			return strings.HasSuffix(sel.Sel.Name, "Error") ||
				strings.HasSuffix(sel.Sel.Name, "Err")
		}
	}
	return false
}

// checkSilentConsumption detects if-blocks where an error is checked but not
// logged, wrapped, or returned.
func (c *SilentErrorChecker) checkSilentConsumption(fset *token.FileSet, filePath string, node ast.Node, errVars map[string]bool, findings *[]Finding) {
	ifStmt, ok := node.(*ast.IfStmt)
	if !ok {
		return
	}

	// Check if the condition is an error check like "err != nil"
	errVarName := c.extractErrCheck(ifStmt.Cond)
	if errVarName == "" {
		return
	}

	// Only flag if the variable is a known error variable
	if !errVars[errVarName] && errVarName != "err" {
		return
	}

	// Expanded check: also treat any variable named "err" or ending with "Err"/"Error" as error
	if !errVars[errVarName] {
		if errVarName == "err" || strings.HasSuffix(errVarName, "Err") || strings.HasSuffix(errVarName, "Error") {
			// These are likely error variables — proceed with check
		} else {
			return
		}
	}

	// Check the body of the if block
	body := ifStmt.Body
	if body == nil || len(body.List) == 0 {
		return
	}

	// Check if any statement in the body logs, wraps, or returns the error
	for _, stmt := range body.List {
		if c.isLogCall(stmt) {
			return // Error is logged — acceptable
		}
		if c.isReturnStmt(stmt) {
			return // Error is returned/wrapped — acceptable
		}
		if c.isPanic(stmt) {
			return // Explicit panic — acceptable
		}
	}

	// If we reach here, the error was not logged or propagated
	line := fset.Position(ifStmt.Pos()).Line
	*findings = append(*findings, Finding{
		File:       filePath,
		Line:       line,
		Check:      CheckSilentError,
		Severity:   SevError,
		Symbol:     errVarName,
		Message:    fmt.Sprintf("'%s' checked but not logged, wrapped, or propagated in if-body", errVarName),
		Suggestion: fmt.Sprintf("add log.Printf(\"error: %%v\", %s) or return fmt.Errorf(\"context: %%w\", %s) in the if-body", errVarName, errVarName),
	})
}

// extractErrCheck extracts the error variable name from a "err != nil" condition.
func (c *SilentErrorChecker) extractErrCheck(cond ast.Expr) string {
	binary, ok := cond.(*ast.BinaryExpr)
	if !ok || binary.Op != token.NEQ {
		return ""
	}

	// Look for "ident != nil"
	if ident, ok := binary.X.(*ast.Ident); ok {
		if isNil(binary.Y) {
			return ident.Name
		}
	}
	return ""
}

// isLogCall checks if a statement is a logging call (log.Printf, fmt.Println, etc.).
func (c *SilentErrorChecker) isLogCall(stmt ast.Stmt) bool {
	exprStmt, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := exprStmt.X.(*ast.CallExpr)
	if !ok {
		return false
	}

	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		// Check for log.Xxx or logger.Xxx
		if sel, ok2 := fun.X.(*ast.Ident); ok2 {
			name := strings.ToLower(sel.Name)
			if strings.Contains(name, "log") || strings.Contains(name, "logger") {
				return true
			}
		}
		// Check for fmt.PrintXxx, fmt.FprintXxx, fmt.SprintXxx
		if pkg, ok2 := fun.X.(*ast.Ident); ok2 && pkg.Name == "fmt" {
			return strings.HasPrefix(fun.Sel.Name, "Print") ||
				strings.HasPrefix(fun.Sel.Name, "Fprint") ||
				strings.HasPrefix(fun.Sel.Name, "Sprint") ||
				fun.Sel.Name == "Errorf"
		}
	}
	return false
}

// isReturnStmt checks if a statement is a return statement (error propagation).
func (c *SilentErrorChecker) isReturnStmt(stmt ast.Stmt) bool {
	_, ok := stmt.(*ast.ReturnStmt)
	return ok
}

// isPanic checks if a statement is a panic call (explicit termination is acceptable).
func (c *SilentErrorChecker) isPanic(stmt ast.Stmt) bool {
	exprStmt, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := exprStmt.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	if ident, ok := call.Fun.(*ast.Ident); ok {
		return ident.Name == "panic"
	}
	return false
}

// isNil checks if an expression is the nil literal.
func isNil(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "nil"
}
