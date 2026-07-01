package experience

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"private-buddy-server/internal/config"
	"private-buddy-server/internal/database"
	applogger "private-buddy-server/internal/logger"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service"
	"private-buddy-server/internal/service/llm"
)

// reflectOutput is the structured output from the LLM during reflection.
type reflectOutput struct {
	Title       string `json:"title" jsonschema:"description=Transferable lesson stated as a general principle"`
	Description string `json:"description" jsonschema:"description=One sentence stating what this teaches — used for semantic matching. State the insight, not what was done."`
	WhenToUse   string `json:"when_to_use" jsonschema:"description=What task signatures, trigger phrases, or problem patterns indicate this experience applies. Each on its own line. Helps distinguish similar but inapplicable situations from different but applicable ones."`
	Guidelines  string `json:"guidelines" jsonschema:"description=Actionable advice with rationale. What to do, why, and in what order. Decision heuristics, sequencing rules, proven patterns."`
	Pitfalls    string `json:"pitfalls" jsonschema:"description=Known failure modes. What can go wrong, early warning signs, and how to prevent or recover."`
	Procedure   string `json:"procedure" jsonschema:"description=Numbered steps. Only include if a repeatable, cross-project workflow emerged. Leave empty if none."`
	Skip        bool   `json:"skip" jsonschema:"description=Set to true if nothing worth extracting"`
}

// CheckReflection iterates all sessions owned by the agent, reads
// fingerprint.txt + notes.md from each, and triggers reflection for
// sessions whose notes have changed since the last reflection.
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

	workspaceRoot := config.Get().GetWorkspaceRoot()

	for _, sess := range sessions {
		workspace := filepath.Join(workspaceRoot, strconv.FormatInt(sess.ID, 10))
		fpFile := filepath.Join(workspace, ".meta", "fingerprint.txt")
		notesFile := filepath.Join(workspace, ".meta", "notes.md")

		fpBytes, err := os.ReadFile(fpFile)
		if err != nil {
			continue
		}
		sourceFingerprint := strings.TrimSpace(string(fpBytes))
		if sourceFingerprint == "" {
			continue
		}

		notesBytes, err := os.ReadFile(notesFile)
		if err != nil || len(notesBytes) == 0 {
			continue
		}

		go reflectSession(ctx, agentID, string(notesBytes), sourceFingerprint)
	}
}

// reflectSession runs the LLM reflection for a single session's notes.
// Runs in a goroutine — errors are logged internally; callers need not
// handle return values.
func reflectSession(ctx context.Context, agentID int64, notesContent, sourceFingerprint string) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// Dedup: skip if this session has already been reflected
	exists, err := existsBySourceFingerprint(ctx, agentID, model.AgentExperienceSourceReflection, sourceFingerprint)
	if err != nil {
		applogger.Error("Reflection dedup check failed", "agent_id", agentID, "error", err)
		return
	}
	if exists {
		applogger.Debug("Reflection: session already reflected, skipping", "agent_id", agentID)
		return
	}

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

	prompt := `Distill transferable experience from a completed task log.

The log below records what happened in one specific task. Extract only the abstract knowledge that could help with a completely different future task — do not summarize or reorganize the log itself.

Strip anything tied to the identity of this task: names, formats, concrete identifiers. If a sentence only makes sense with "which project" or "which tool" as context, it does not belong.

Fill each output field as follows:

title: The transferable lesson, stated as a general principle.

description: One sentence stating the core insight — used for semantic matching. State what this teaches, not what was done.

when_to_use: What task signatures, trigger phrases, or problem patterns indicate this experience applies. Helps distinguish "looks similar but isn't" from "looks different but is". Leave empty if the lesson applies broadly.

guidelines: Actionable advice with rationale. What to do, why, and in what order. Decision heuristics, sequencing rules, proven patterns.

pitfalls: Known failure modes. What can go wrong, early warning signs, and how to prevent or recover. Leave empty if no pitfalls were encountered.

procedure: Numbered steps. Only include if a repeatable workflow emerged. Leave empty if none.

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
		applogger.Info("Reflection: nothing worth extracting", "agent_id", agentID)
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

	if _, err := createExperience(ctx, agentID, model.AgentExperienceSourceReflection, sourceFingerprint,
		output.Title, output.Description, output.WhenToUse, output.Guidelines, output.Pitfalls, output.Procedure); err != nil {
		applogger.Error("Reflection: failed to save experience", "agent_id", agentID, "error", err)
		return
	}

	applogger.Info("Reflection: experience created", "agent_id", agentID)
}
