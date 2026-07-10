package experience

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"private-buddy-server/internal/database"
	applogger "private-buddy-server/internal/logger"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service"
	"private-buddy-server/internal/service/llm"
	"private-buddy-server/internal/service/workspace"
)

// reflectOutput is the structured output from the LLM during reflection.
type reflectOutput struct {
	Title       string `json:"title" jsonschema:"description=Transferable lesson stated as a general principle"`
	Description string `json:"description" jsonschema:"description=One sentence stating what this teaches — used for semantic matching. State the insight, not what was done."`
	WhenToUse   string `json:"when_to_use" jsonschema:"description=What task signatures, trigger phrases, or problem patterns indicate this experience applies. Each on its own line. Helps distinguish similar but inapplicable situations from different but applicable ones."`
	Guidelines  string `json:"guidelines" jsonschema:"description=Actionable advice with rationale. What to do, why, and in what order. Decision heuristics, sequencing rules, proven patterns."`
	Pitfalls    string `json:"pitfalls" jsonschema:"description=Known failure modes. What can go wrong, early warning signs, and how to prevent or recover."`
	Procedure   string `json:"procedure" jsonschema:"description=Numbered steps. Only include if a repeatable, cross-project workflow emerged. Leave empty if none."`
	UpdateExpID int64  `json:"update_exp_id" jsonschema:"description=If this experience refines or overlaps with an existing experience you already have, set this to that experience's id. Leave 0 to create a new experience."`
	Skip        bool   `json:"skip" jsonschema:"description=Set to true if nothing worth extracting"`
}

// CheckReflection iterates all sessions owned by the agent and triggers
// reflection for sessions whose notes.md has changed since the last
// successful reflection.
//
// Dedup is file-based: <workspace>/.meta/fingerprint.txt stores the SHA-256
// hash of notes.md as it was at the end of the last reflection. If the
// current notes.md hash matches, the session is skipped. If fingerprint.txt
// is missing (first reflection) or differs, reflection runs.
//
// This is the public entry point called from the agent heartbeat.
// Safe to call when the embedding service is not configured — does nothing.
func CheckReflection(ctx context.Context, agentID int64) {
	if embeddingSvc == nil {
		return
	}

	var sessions []model.Session
	if err := database.DB.Where("agent_id = ?", agentID).Find(&sessions).Error; err != nil {
		applogger.Error("CheckReflection: failed to list sessions", "agent_id", agentID, "error", err)
		return
	}

	for _, sess := range sessions {
		metaDir := workspace.GetMetaDir(sess.AgentID, sess.ID)
		fpFile := filepath.Join(metaDir, "fingerprint.txt")
		notesFile := filepath.Join(metaDir, "notes.md")

		notesBytes, err := os.ReadFile(notesFile)
		if err != nil {
			if os.IsNotExist(err) {
				// Common case: session hasn't run a task yet, so notes.md
				// doesn't exist. Info-level (not silent) to keep heartbeat
				// behavior observable without flooding the log.
				applogger.Info("CheckReflection: notes file not exist",
					"file", notesFile,
				)
			} else {
				// Unexpected read error — not a normal heartbeat path.
				applogger.Error("CheckReflection: failed to read notes file",
					"file", notesFile,
					"error", err,
				)
			}
			continue
		}
		if len(notesBytes) == 0 {
			continue
		}
		notesContent := string(notesBytes)
		currentFingerprint := sha256Hex(notesContent)

		// Compare against the last reflection's fingerprint. Missing file or
		// differing content means reflection should run.
		lastFingerprintBytes, err := os.ReadFile(fpFile)
		if err != nil {
			if os.IsNotExist(err) {
				// File missing (first reflection for this session) — fall
				// through to trigger reflection.
				applogger.Info("CheckReflection: fingerprint file not exist",
					"file", fpFile,
				)
			} else {
				// Unexpected read error — log and skip this session. Using
				// continue (not break) so other sessions are still processed.
				applogger.Error("CheckReflection: failed to read fingerprint file",
					"file", fpFile,
					"error", err,
				)
				continue
			}
		} else if string(lastFingerprintBytes) == currentFingerprint {
			// No change since the last reflection — skip.
			continue
		}

		go reflectSession(ctx, agentID, sess.ID, notesContent, currentFingerprint, fpFile)
	}
}

// reflectSession runs the LLM reflection for a single session's notes and
// writes the new fingerprint to fpFile on success (including skip), so the
// next heartbeat will not re-trigger reflection for unchanged notes.
//
// Runs in a goroutine — errors are logged internally; callers need not
// handle return values.
//
// On LLM or parse failure, the fingerprint is NOT written, so the next
// heartbeat will retry.
func reflectSession(ctx context.Context, agentID, sessionID int64, notesContent, currentFingerprint, fpFile string) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// Load agent LLM config from DB
	ag, err := service.GetAgent(agentID)
	if err != nil {
		applogger.Error("Reflection: failed to load agent", "agent_id", agentID, "error", err)
		return
	}
	llmCfg, err := service.GetLLMConfig(ag.LLMConfigID)
	if err != nil {
		applogger.Error("Reflection: failed to load LLM config", "agent_id", agentID, "error", err)
		return
	}
	chatModel := llm.NewChatModelWithTemperature(
		llmCfg.BaseURL, llmCfg.APIKey, llmCfg.ModelID, llm.TemperatureControlled,
	)

	schema := llm.GenerateSchema[reflectOutput]()

	// Note: existing experiences are NOT loaded into the prompt. Loading them
	// would bloat the context as the experience library grows. Instead, the
	// LLM is given the option to return update_exp_id from its own knowledge
	// of exp_ids it has seen during task execution (via scan/recall tools).
	prompt := `Distill transferable experience from a completed task log.

The log below records what happened in one specific task. Extract only the abstract knowledge that could help with a completely different future task — do not summarize or reorganize the log itself.

Strip task-identifying details (project names, person names, specific file paths) and host-environment coupling (system-specific tools like write_notes/wake_me_when, internal APIs, system config) — these are not transferable. Keep concrete technical details (domain APIs like Canvas/fillText, library names, function signatures, algorithm steps) — they are the actionable value, not host coupling.

Fill each output field as follows:

title: The transferable lesson, stated as a general principle.

description: One sentence stating the core insight — used for semantic matching. State what this teaches, not what was done.

when_to_use: What task signatures, trigger phrases, or problem patterns indicate this experience applies. Helps distinguish "looks similar but isn't" from "looks different but is". Leave empty if the lesson applies broadly.

guidelines: Actionable advice with rationale. What to do, why, and in what order. Decision heuristics, sequencing rules, proven patterns.

pitfalls: Known failure modes. What can go wrong, early warning signs, and how to prevent or recover. Leave empty if no pitfalls were encountered.

procedure: Numbered steps. Only include if a repeatable workflow emerged. Leave empty if none.

update_exp_id: If this experience refines or overlaps with an existing experience you already have (e.g., an exp_id you saw via scan_my_experience / recall_my_experience during this task), set this to that experience's id. Otherwise, leave it as 0 to create a new experience.

skip: true only if the log contains nothing transferable.

## Task log
` + notesContent

	messages := []llm.Message{
		{Role: "user", Content: prompt},
	}

	schemaDef := llm.JSONSchemaDefinition{
		Name:   "reflect_output",
		Schema: json.RawMessage(schema),
		Strict: true,
	}

	response, err := chatModel.ChatWithJSONSchema(ctx, messages, schemaDef)
	if err != nil {
		applogger.Error("Reflection: LLM call failed", "agent_id", agentID, "error", err)
		return
	}

	var output reflectOutput
	if err := json.Unmarshal([]byte(response), &output); err != nil {
		applogger.Error("Reflection: failed to parse LLM output", "agent_id", agentID, "error", err)
		return
	}

	if output.Skip {
		applogger.Info("Reflection: nothing worth extracting", "agent_id", agentID, "session_id", sessionID)
		writeFingerprint(fpFile, currentFingerprint)
		return
	}

	if output.Title == "" || output.Description == "" {
		applogger.Error("Reflection: LLM output missing required fields",
			"agent_id", agentID,
			"has_title", output.Title != "",
			"has_description", output.Description != "",
		)
		return
	}

	// Branch: update an existing experience or create a new one.
	// The LLM returns update_exp_id when it recognizes (from exp_ids it saw
	// during task execution) that this lesson refines an existing one.
	if output.UpdateExpID > 0 {
		if err := updateExperience(ctx, output.UpdateExpID, agentID,
			output.Title, output.Description, output.WhenToUse,
			output.Guidelines, output.Pitfalls, output.Procedure); err != nil {
			applogger.Error("Reflection: failed to update experience",
				"agent_id", agentID,
				"exp_id", output.UpdateExpID,
				"error", err,
			)
			return
		}
		applogger.Info("Reflection: experience updated",
			"agent_id", agentID,
			"exp_id", output.UpdateExpID,
			"session_id", sessionID,
		)
		writeFingerprint(fpFile, currentFingerprint)
		return
	}

	// source_id = session_id: the reflection pipeline can only pinpoint
	// provenance down to the session granularity.
	if _, err := createExperience(ctx, agentID, model.AgentExperienceSourceReflection, sessionID,
		output.Title, output.Description, output.WhenToUse, output.Guidelines, output.Pitfalls, output.Procedure); err != nil {
		applogger.Error("Reflection: failed to save experience", "agent_id", agentID, "error", err)
		return
	}

	applogger.Info("Reflection: experience created",
		"agent_id", agentID,
		"session_id", sessionID,
	)
	writeFingerprint(fpFile, currentFingerprint)
}

// writeFingerprint persists the given fingerprint to fpFile so the next
// heartbeat can detect whether notes.md has changed since this reflection.
// Failures are logged but do not abort the caller — a missed write only
// causes a redundant re-reflection on the next heartbeat, which is safe.
func writeFingerprint(fpFile, fingerprint string) {
	if err := os.WriteFile(fpFile, []byte(fingerprint), 0644); err != nil {
		applogger.Error("Reflection: failed to write fingerprint file",
			"file", fpFile,
			"error", err,
		)
	}
}

// sha256Hex returns the hex-encoded SHA-256 hash of s.
func sha256Hex(s string) string {
	hash := sha256.Sum256([]byte(s))
	return hex.EncodeToString(hash[:])
}
