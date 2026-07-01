package experience

import (
	"fmt"
	"strings"

	applogger "private-buddy-server/internal/logger"
)

// FormatForSystemPrompt formats retrieved experiences into a section for the
// task system prompt. Returns empty string when no experiences found.
func FormatForSystemPrompt(experiences []SearchResult) string {
	if len(experiences) == 0 {
		return ""
	}

	var lines []string
	lines = append(lines,
		"[Past Experience]",
		"The following experience from your prior tasks may be relevant.",
		"",
	)

	totalChars := 0
	for i, sr := range experiences {
		e := sr.Experience
		var parts []string
		parts = append(parts, fmt.Sprintf("## %d. %s", i+1, e.Title))
		if e.WhenToUse != "" {
			parts = append(parts, "When to Use: "+e.WhenToUse)
		}
		if e.Guidelines != "" {
			parts = append(parts, "Guidelines: "+e.Guidelines)
		}
		if e.Pitfalls != "" {
			parts = append(parts, "Pitfalls: "+e.Pitfalls)
		}
		if e.Procedure != "" {
			parts = append(parts, "Procedure: "+e.Procedure)
		}
		block := strings.Join(parts, "\n")
		lines = append(lines, block)
		totalChars += len(block)
	}

	if totalChars > 4000 {
		applogger.Warn("Experience content injected into system prompt is large",
			"total_chars", totalChars,
			"experience_count", len(experiences),
		)
	}

	return strings.Join(lines, "\n")
}
