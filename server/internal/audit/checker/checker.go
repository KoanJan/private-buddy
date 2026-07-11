// Package checker defines the interface and shared types for audit checkers.
// Each checker implements the Checker interface to detect a specific type of
// constitution violation in source code.
package checker

// CheckType identifies the category of a code quality violation.
// Must be int per constitution principle III.
type CheckType int

const (
	// CheckMissingComment detects exported symbols without English doc comments.
	CheckMissingComment CheckType = 0
	// CheckSilentError detects errors consumed without logging or propagation.
	CheckSilentError CheckType = 1
	// CheckNullableField detects database model fields missing NOT NULL constraint.
	CheckNullableField CheckType = 2
	// CheckForeignKey detects foreign key constraints on database models.
	CheckForeignKey CheckType = 3
	// CheckStringEnum detects enum types defined as string instead of int.
	CheckStringEnum CheckType = 4
	// CheckDuplicateResource detects semantically identical resources defined in
	// multiple locations.
	CheckDuplicateResource CheckType = 5
)

// String returns a snake_case identifier for the check type, used in report output.
func (c CheckType) String() string {
	switch c {
	case CheckMissingComment:
		return "missing_comment"
	case CheckSilentError:
		return "silent_error"
	case CheckNullableField:
		return "nullable_field"
	case CheckForeignKey:
		return "foreign_key"
	case CheckStringEnum:
		return "string_enum"
	case CheckDuplicateResource:
		return "duplicate_resource"
	default:
		return "unknown"
	}
}

// Label returns a human-readable Chinese label for the check type.
func (c CheckType) Label() string {
	switch c {
	case CheckMissingComment:
		return "缺少英文注释"
	case CheckSilentError:
		return "静默错误"
	case CheckNullableField:
		return "缺少 NOT NULL"
	case CheckForeignKey:
		return "外键约束"
	case CheckStringEnum:
		return "字符串枚举"
	case CheckDuplicateResource:
		return "重复资源"
	default:
		return "未知"
	}
}

// Severity indicates the seriousness of a finding.
type Severity int

const (
	// SevError marks a finding that must be fixed (blocks quality gate).
	SevError Severity = 0
	// SevWarning marks a finding that should be fixed (advisory).
	SevWarning Severity = 1
)

// String returns a human-readable label for the severity.
func (s Severity) String() string {
	switch s {
	case SevError:
		return "error"
	case SevWarning:
		return "warning"
	default:
		return "unknown"
	}
}

// Finding represents a single violation detected during an audit scan.
type Finding struct {
	// File is the relative file path from the project root.
	File string `json:"file"`
	// Line is the starting line number (1-based).
	Line int `json:"line"`
	// EndLine is the ending line number for multi-line violations.
	EndLine int `json:"end_line"`
	// Check identifies the type of violation.
	Check CheckType `json:"check"`
	// Severity indicates whether this is an error or warning.
	Severity Severity `json:"severity"`
	// Symbol is the affected identifier name (empty if N/A).
	Symbol string `json:"symbol"`
	// Message is a human-readable description of the violation.
	Message string `json:"message"`
	// Suggestion is a recommended fix direction.
	Suggestion string `json:"suggestion"`
	// Fingerprint is a stable hash for baseline identity tracking.
	Fingerprint string `json:"fingerprint"`
}

// Checker defines the interface for a code quality auditor.
// Each checker scans source files for a specific type of violation.
type Checker interface {
	// Name returns the checker's identifier (used for reporting).
	Name() string

	// CheckType returns the CheckType this checker detects.
	Type() CheckType

	// Accept reports whether this checker handles the given file extension.
	Accept(ext string) bool

	// Check scans the file content and returns any findings.
	// filePath is relative to the project root, content is the raw file bytes.
	Check(filePath string, content []byte) ([]Finding, error)
}
