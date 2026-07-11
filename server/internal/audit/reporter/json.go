// Package reporter: json.go formats audit findings as structured JSON
// matching the AuditReport data model defined in the feature specification.
package reporter

import (
	"encoding/json"
	"fmt"
	"os"
)

// JSONReporter outputs audit findings as structured JSON to stdout.
type JSONReporter struct{}

// NewJSONReporter creates a JSONReporter.
func NewJSONReporter() *JSONReporter {
	return &JSONReporter{}
}

// Report serializes the audit report as indented JSON to stdout.
func (r *JSONReporter) Report(rep *Report) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(rep); err != nil {
		return fmt.Errorf("JSON serialization failed: %w", err)
	}
	return nil
}
