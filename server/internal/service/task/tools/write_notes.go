package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"
)

// WriteNotesTool implements an append-only, structured notes system for persisting agent's working memory.
//
// Each note entry has:
//   - Timestamp: when the entry was written
//   - Type: observation/decision/finding/correction/progress
//   - Content: the main note text
//   - References: optional links to workspace files
//   - Conflict marker: optional note about conflicting with a previous entry
//
// Design principles:
//   - Append-only: never overwrite, always add new entries
//   - Structured: consistent format for traceability
//   - Traceable: each entry can reference files and mark conflicts
//   - LLM stateless: each LLM call is independent, notes bridge the gap
//
// The notes are stored in a system-managed location (.meta/notes.md) that the
// agent should not directly access. Use this tool to interact with notes.
type WriteNotesTool struct {
	sessionID     int64 // Session ID for determining the workspace path
	workspaceRoot string
	notesMaxChars int // Maximum character limit for notes file
}

// NewWriteNotesTool creates a WriteNotesTool for the given session.
func NewWriteNotesTool(sessionID int64, workspaceRoot string, notesMaxChars int) *WriteNotesTool {
	return &WriteNotesTool{
		sessionID:     sessionID,
		workspaceRoot: workspaceRoot,
		notesMaxChars: notesMaxChars,
	}
}

func (w *WriteNotesTool) Name() string { return "write_notes" }

func (w *WriteNotesTool) Schema() openai.FunctionDefinition {
	return openai.FunctionDefinition{
		Name: "write_notes",
		Description: "Append a structured entry to your NOTES. " +
			"This ADDS a new entry, it does NOT overwrite. " +
			"Use this to persist important information for future steps. " +
			"\n\n" +
			"IMPORTANT: Notes have a size limit. Only write IMPORTANT entries. " +
			"Skip trivial or obvious information. " +
			"Focus on key facts that future steps MUST know — " +
			"critical discoveries, important decisions, and essential state. " +
			"When in doubt, ask: would losing this information hurt the task? " +
			"If not, skip it." +
			"\n\n" +
			"Entry types:\n" +
			"- observation: Something you discovered or noticed\n" +
			"- decision: A choice you made and why\n" +
			"- finding: A key result or conclusion\n" +
			"- correction: A fix or change to a previous entry (use conflicts_with)\n" +
			"- progress: Current status and next steps\n" +
			"\n" +
			"Always include:\n" +
			"- Concise, self-contained content\n" +
			"- File references when relevant (paths relative to your working directory)\n" +
			"- Conflict markers when correcting earlier decisions",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"entry_type": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"observation", "decision", "finding", "correction", "progress"},
					"description": "The type of this note entry",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "The main content of this note. Be CONCISE — only include information that is IMPORTANT to preserve for future steps.",
				},
				"references": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Optional list of file paths this note relates to. Use paths relative to your working directory. Example: ['result.json', 'src/main.py']",
				},
				"conflicts_with": map[string]interface{}{
					"type":        "string",
					"description": "Optional timestamp or description of a previous entry that this entry corrects or supersedes. Example: '2024-05-20 14:30:00' or 'the decision about X'",
				},
			},
			"required": []string{"entry_type", "content"},
		},
	}
}

// Execute appends a structured entry to the agent's notes.
// Validates required fields (entry_type, content) and returns a confirmation message.
func (w *WriteNotesTool) Execute(args map[string]interface{}) (string, error) {
	entryType, _ := args["entry_type"].(string)
	content, _ := args["content"].(string)

	var references []string
	if refs, ok := args["references"].([]interface{}); ok {
		for _, r := range refs {
			if s, ok := r.(string); ok {
				references = append(references, s)
			}
		}
	}

	conflictsWith, _ := args["conflicts_with"].(string)

	if entryType == "" || content == "" {
		return "Error: entry_type and content are required", nil
	}

	w.appendNote(entryType, content, references, conflictsWith)

	refCount := len(references)
	conflictMarker := ""
	if conflictsWith != "" {
		conflictMarker = " (with conflict marker)"
	}

	return fmt.Sprintf("Successfully appended %s entry to your NOTES. Content: %d chars, References: %d%s",
		entryType, len(content), refCount, conflictMarker), nil
}

// getMetaDir returns the .meta directory path for this session's workspace.
func (w *WriteNotesTool) getMetaDir() string {
	return filepath.Join(w.workspaceRoot, strconv.FormatInt(w.sessionID, 10), ".meta")
}

// appendNote appends a structured entry to the notes.md file.
// Format: "## [TIMESTAMP] TYPE\n\ncontent\n\n---\n\n"
func (w *WriteNotesTool) appendNote(entryType, content string, references []string, conflictsWith string) {
	notesFile := filepath.Join(w.getMetaDir(), "notes.md")
	timestamp := time.Now().Format("2006-01-02 15:04:05")

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## [%s] %s\n\n", timestamp, strings.ToUpper(entryType)))
	sb.WriteString(content)
	sb.WriteString("\n")

	if len(references) > 0 {
		sb.WriteString("\n**References:**\n")
		for _, ref := range references {
			sb.WriteString(fmt.Sprintf("- `%s`\n", ref))
		}
	}

	if conflictsWith != "" {
		sb.WriteString(fmt.Sprintf("\n⚠️ **Conflicts with:** %s\n", conflictsWith))
		sb.WriteString("_See above for the previous entry that this corrects or supersedes._\n")
	}

	sb.WriteString("\n---\n\n")

	f, err := os.OpenFile(notesFile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(sb.String())
}

// ReadNotes reads the full content of the notes.md file.
// Returns empty string if the file doesn't exist.
func (w *WriteNotesTool) ReadNotes() string {
	notesFile := filepath.Join(w.getMetaDir(), "notes.md")
	data, err := os.ReadFile(notesFile)
	if err != nil {
		return ""
	}
	return string(data)
}

// TrimNotes truncates the notes file if it exceeds the maximum character limit.
// Trims from the beginning, preserving the most recent entries.
// Attempts to align to entry boundaries (## [timestamp]) to avoid partial entries.
func (w *WriteNotesTool) TrimNotes() {
	notesFile := filepath.Join(w.getMetaDir(), "notes.md")
	data, err := os.ReadFile(notesFile)
	if err != nil {
		return
	}
	content := string(data)
	if len(content) <= w.notesMaxChars {
		return
	}

	trimmed := content[len(content)-w.notesMaxChars:]
	entryBoundary := strings.Index(trimmed, "\n## [")
	if entryBoundary > 0 {
		trimmed = trimmed[entryBoundary+1:]
	}

	os.WriteFile(notesFile, []byte("[notes.md trimmed: older entries discarded]\n\n"+trimmed), 0644)
}
