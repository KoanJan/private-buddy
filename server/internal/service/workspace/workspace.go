// Package workspace manages agent session workspace paths and directory lifecycle.
//
// The workspace layout is:
//
//	{workspaceRoot}/{agent_id}/{session_id}/
//	  ├── .meta/    — system-managed files (notes.md, fingerprint.txt)
//	  └── output/   — agent's working output directory
//
// The {agent_id} layer provides structural preparation for future multi-agent
// isolation (Actor model). In single-agent mode it is purely organizational —
// read/write permissions and user visibility are unchanged.
package workspace

import (
	"os"
	"path/filepath"
	"strconv"

	"private-buddy-server/internal/config"
)

// getRoot returns the workspace root directory resolved to an absolute path,
// so that paths shown to the agent in prompts and used as bash CWD are unambiguous.
func getRoot() string {
	root := config.Get().GetWorkspaceRoot()
	if abs, err := filepath.Abs(root); err == nil {
		return abs
	}
	return root
}

// GetWorkspacePath returns the workspace directory for a specific agent and session.
// Path: {workspaceRoot}/{agent_id}/{session_id}
func GetWorkspacePath(agentID, sessionID int64) string {
	return filepath.Join(getRoot(), strconv.FormatInt(agentID, 10), strconv.FormatInt(sessionID, 10))
}

// GetMetaDir returns the .meta directory path for system-managed files.
// Path: {workspaceRoot}/{agent_id}/{session_id}/.meta
func GetMetaDir(agentID, sessionID int64) string {
	return filepath.Join(GetWorkspacePath(agentID, sessionID), ".meta")
}

// GetOutputDir returns the output directory path for agent working files.
// Path: {workspaceRoot}/{agent_id}/{session_id}/output
func GetOutputDir(agentID, sessionID int64) string {
	return filepath.Join(GetWorkspacePath(agentID, sessionID), "output")
}

// InitWorkspace creates the workspace directory structure for a session and
// initializes notes.md if it doesn't exist. Returns the workspace path.
func InitWorkspace(agentID, sessionID int64) string {
	ws := GetWorkspacePath(agentID, sessionID)
	metaDir := filepath.Join(ws, ".meta")
	os.MkdirAll(metaDir, 0755)

	notesFile := filepath.Join(metaDir, "notes.md")
	if _, err := os.Stat(notesFile); err != nil {
		os.WriteFile(notesFile, []byte("# Agent Notes\n\nStructured log of agent's work progress.\n\n"), 0644)
	}

	outputDir := GetOutputDir(agentID, sessionID)
	os.MkdirAll(outputDir, 0755)

	return ws
}

// RemoveWorkspace removes the entire workspace directory for a session.
func RemoveWorkspace(agentID, sessionID int64) {
	os.RemoveAll(GetWorkspacePath(agentID, sessionID))
}
