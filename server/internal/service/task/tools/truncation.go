package tools

import (
	"fmt"
	"strings"
)

// DefaultTruncateBytes is the recommended threshold for tool-level semantic
// truncation. The system-level byte fallback threshold is defined separately
// in task_loop.go (hardOutputLimit) and must be greater than this value plus
// a buffer for JSON marshalling overhead.
const (
	DefaultTruncateBytes = 20 * 1024 // 20KB
	newlineAlignWindow   = 1024      // Line-boundary alignment window (1KB) for truncation
)

// TruncateHead keeps the beginning of s and truncates the tail.
// Used by read_file, grep, and similar tools where the beginning is more
// important. Aligns to a line boundary within a 1KB window near the cut point
// to avoid splitting a line in half.
//
// Returns the truncated content and a flag indicating whether truncation
// occurred. If len(s) <= maxBytes, s is returned unchanged with truncated=false.
//
// Note: byte-level slicing may split a multi-byte UTF-8 character at the cut
// point. This only affects the single truncated character and is acceptable
// for large-output truncation scenarios.
func TruncateHead(s string, maxBytes int) (string, bool) {
	if len(s) <= maxBytes {
		return s, false
	}
	cut := maxBytes
	// Find the last newline within maxBytes to avoid splitting a line.
	// Only effective when the last newline is within 1KB of the cut point;
	// otherwise the byte-level cut is used directly.
	if lastNL := strings.LastIndex(s[:maxBytes], "\n"); lastNL > maxBytes-newlineAlignWindow {
		cut = lastNL
	}
	return s[:cut], true
}

// TruncateTail keeps the end of s and truncates the head.
// Used by bash and similar tools where the end (error messages, recent output)
// is more important. Aligns to a line boundary within a 1KB window near the
// cut point to avoid splitting a line in half.
//
// Returns the truncated content and a flag indicating whether truncation
// occurred. If len(s) <= maxBytes, s is returned unchanged with truncated=false.
//
// Note: byte-level slicing may split a multi-byte UTF-8 character at the cut
// point. This only affects the single truncated character and is acceptable
// for large-output truncation scenarios.
func TruncateTail(s string, maxBytes int) (string, bool) {
	if len(s) <= maxBytes {
		return s, false
	}
	start := len(s) - maxBytes
	if start < 0 {
		start = 0
	}
	// Find the first newline after start to avoid splitting a line.
	// Only effective when the first newline is within 1KB of the cut point;
	// otherwise the byte-level cut is used directly.
	if firstNL := strings.Index(s[start:], "\n"); firstNL >= 0 && firstNL < newlineAlignWindow {
		start = start + firstNL + 1
	}
	return s[start:], true
}

// Hint generates a default truncation notice text showing how many bytes were
// retained out of the total. Tools may use this or provide a custom message.
func Hint(shown, total int) string {
	return fmt.Sprintf("[truncated: showed %d of %d bytes]", shown, total)
}
