// Package workspace manages agent session workspace paths and directory lifecycle.
//
// This file handles AAC (agent access control) directory cleanup for sandbox
// policy files. AAC directories live under {DATA_ROOT}/aac/{agentID}/{sessionID}/
// and are separate from workspace directories — they must be cleaned up independently
// when a session is deleted.
package workspace

import (
	"os"
	"path/filepath"
	"strconv"

	"private-buddy-server/internal/config"

	applogger "private-buddy-server/internal/logger"
)

// RemoveAac removes the AAC policy directory for the given session.
// This should be called alongside RemoveWorkspace when a session is deleted
// to prevent orphaned sandbox policy files from accumulating.
func RemoveAac(agentID, sessionID int64) {
	aacDir := filepath.Join(config.Get().GetDataRoot(), "aac",
		strconv.FormatInt(agentID, 10), strconv.FormatInt(sessionID, 10))
	if err := os.RemoveAll(aacDir); err != nil {
		applogger.Warn("failed to remove AAC directory",
			"agent_id", agentID, "session_id", sessionID,
			"path", aacDir, "error", err)
	} else {
		applogger.Info("AAC directory removed",
			"agent_id", agentID, "session_id", sessionID,
			"path", aacDir)
	}
}
