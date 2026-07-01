package experience

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"private-buddy-server/internal/database"
	applogger "private-buddy-server/internal/logger"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service"
	"private-buddy-server/internal/service/llm"
)

// ErrDuplicateSkill is returned when a skill with the same content has already been ingested.
var ErrDuplicateSkill = fmt.Errorf("duplicate_skill: this skill has already been ingested")

// IngestSkillParams carries the raw external skill content to be refined.
type IngestSkillParams struct {
	SourceName string // Skill file name
	RawContent string // Raw SKILL.md content
}

// ingestOutput is the structured output from the LLM during skill ingestion.
// Mirrors reflectOutput so that public and private experiences share the same structure.
type ingestOutput struct {
	Title       string `json:"title" jsonschema:"description=Transferable lesson stated as a general principle"`
	Description string `json:"description" jsonschema:"description=One sentence stating what this teaches — used for semantic matching. State the insight, not what was done."`
	WhenToUse   string `json:"when_to_use" jsonschema:"description=What task signatures, trigger phrases, or problem patterns indicate this experience applies. Each on its own line."`
	Guidelines  string `json:"guidelines" jsonschema:"description=Actionable advice with rationale. What to do, why, and in what order. Decision heuristics, sequencing rules, proven patterns."`
	Pitfalls    string `json:"pitfalls" jsonschema:"description=Known failure modes. What can go wrong, early warning signs, and how to prevent or recover."`
	Procedure   string `json:"procedure" jsonschema:"description=Numbered steps. Only include if a repeatable, cross-project workflow emerged. Leave empty if none."`
	Skip        bool   `json:"skip" jsonschema:"description=Set to true if nothing worth extracting (e.g., pure reference with no structural pattern)"`
}

// sourceFingerprint computes the SHA-256 hash of raw content for deduplication.
func sourceFingerprint(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}

// IngestSkill submits a skill for asynchronous refinement into a public experience.
// Deduplicates by fingerprint (unique index). Returns immediately after inserting
// the UploadedSkill record; a background goroutine handles LLM refinement.
func IngestSkill(ctx context.Context, params IngestSkillParams) (*model.UploadedSkill, error) {
	panicIfNotReady()

	fp := sourceFingerprint(params.RawContent)

	uploaded := &model.UploadedSkill{
		SourceName:  params.SourceName,
		RawContent:  params.RawContent,
		Fingerprint: fp,
		Status:      model.UploadedSkillStatusPending,
	}
	if err := database.DB.Create(uploaded).Error; err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			applogger.Info("IngestSkill: duplicate content skipped",
				"source_name", params.SourceName,
			)
			return nil, ErrDuplicateSkill
		}
		return nil, fmt.Errorf("save uploaded skill: %w", err)
	}

	applogger.Info("IngestSkill: submitted for async processing",
		"uploaded_skill_id", uploaded.ID,
		"source_name", params.SourceName,
	)

	go processIngestion(*uploaded, params)
	return uploaded, nil
}

// processIngestion runs the LLM refinement and creates the PublicExperience.
// Runs in a goroutine. Uses a CAS update to claim the pending record for
// concurrency safety. On failure, only logs the error — the record stays
// at processing status; fingerprint uniqueness prevents re-submission.
func processIngestion(uploaded model.UploadedSkill, params IngestSkillParams) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Claim this record: set processing only if currently pending (concurrency guard).
	result := database.DB.Model(&model.UploadedSkill{}).
		Where("id = ? AND status = ?", uploaded.ID, model.UploadedSkillStatusPending).
		Update("status", model.UploadedSkillStatusProcessing)
	if result.Error != nil {
		applogger.Error("processIngestion: failed to claim record",
			"uploaded_skill_id", uploaded.ID,
			"error", result.Error,
		)
		return
	}
	if result.RowsAffected == 0 {
		applogger.Info("processIngestion: record already claimed",
			"uploaded_skill_id", uploaded.ID,
		)
		return
	}

	sysCfg := service.GetSystemLLMConfig()
	if sysCfg == nil {
		applogger.Error("processIngestion: system LLM config not configured",
			"uploaded_skill_id", uploaded.ID,
		)
		return
	}

	chatModel := llm.NewChatModelWithTemperature(
		sysCfg.BaseURL, sysCfg.APIKey, sysCfg.ModelID, llm.TemperatureControlled,
	)

	schema := llm.GenerateSchema[ingestOutput]()

	prompt := `Refine an external skill document into a host-agnostic experience record.

The content below comes from a skill file (SKILL.md). Classify the skill first:

1. **Method skills** — teach a workflow, decision framework, or technique.
   - Keep: actionable advice, decision heuristics, workflow steps, failure modes, trigger conditions.
   - Discard: tool names, script paths, environment assumptions, instructions for humans.

2. **Reference skills** — primarily list specific values (color codes, font names, API endpoints, file paths, company/product names, environment-specific strings).
   - Extract the structural pattern only — not the values.
   - Use placeholder notation like <primary-color>, <heading-font>, <api-base-url> when describing the pattern.
   - If no structural pattern remains after removing values, output empty fields and set skip=true.

What to discard (applies to both types):
- Specific values: color hex codes, font names, API URLs, file paths, company/product names, tool names, script paths, environment assumptions.
- Instructions written for human developers.

Fill each output field as follows:

title: A short, transferable principle name derived from the skill's core method.

description: One sentence stating the core skill insight — used for semantic matching.

when_to_use: What task signatures, trigger phrases, or problem patterns indicate this skill applies. Leave empty if broadly applicable.

guidelines: Actionable advice with rationale. What to do, why, and in what order.

pitfalls: Known failure modes. What can go wrong, early warning signs, and how to prevent or recover.

procedure: Numbered steps. Only include if a repeatable workflow is described. Leave empty if none.

## Skill content
` + "```markdown\n" + params.RawContent + "\n```"

	messages := []llm.Message{
		{Role: "user", Content: prompt},
	}

	schemaDef := llm.JSONSchemaDefinition{
		Name:   "ingest_output",
		Schema: json.RawMessage(schema),
		Strict: true,
	}

	response, err := chatModel.ChatWithJSONSchema(ctx, messages, schemaDef)
	if err != nil {
		applogger.Error("processIngestion: LLM call failed",
			"uploaded_skill_id", uploaded.ID,
			"error", err,
		)
		return
	}

	var output ingestOutput
	if err := json.Unmarshal([]byte(response), &output); err != nil {
		applogger.Error("processIngestion: parse LLM output failed",
			"uploaded_skill_id", uploaded.ID,
			"error", err,
		)
		return
	}

	if output.Skip {
		applogger.Info("processIngestion: nothing worth extracting",
			"uploaded_skill_id", uploaded.ID,
			"source_name", params.SourceName,
		)
		return
	}

	if output.Title == "" || output.Description == "" {
		applogger.Error("processIngestion: LLM output missing required fields",
			"uploaded_skill_id", uploaded.ID,
			"title", output.Title,
			"description", output.Description,
		)
		return
	}

	dbCtx, dbCancel := context.WithTimeout(ctx, 30*time.Second)
	defer dbCancel()

	exp, err := createPublicExperience(dbCtx, output.Title, output.Description,
		output.WhenToUse, output.Guidelines, output.Pitfalls, output.Procedure,
		uploaded.ID, sourceFingerprint(params.RawContent))
	if err != nil {
		applogger.Error("processIngestion: create public experience failed",
			"uploaded_skill_id", uploaded.ID,
			"error", err,
		)
		return
	}

	if err := database.DB.Model(&uploaded).Updates(map[string]interface{}{
		"status": model.UploadedSkillStatusCompleted,
	}).Error; err != nil {
		applogger.Error("processIngestion: failed to set status=completed",
			"uploaded_skill_id", uploaded.ID,
			"public_experience_id", exp.ID,
			"error", err,
		)
		return
	}

	applogger.Info("processIngestion: public experience created",
		"uploaded_skill_id", uploaded.ID,
		"public_experience_id", exp.ID,
		"source_name", params.SourceName,
	)
}
