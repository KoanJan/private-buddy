package experience

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"private-buddy-server/internal/database"
	applogger "private-buddy-server/internal/logger"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service"
	"private-buddy-server/internal/service/llm"
)

// IngestSkillParams carries the raw external skill content to be refined.
type IngestSkillParams struct {
	FileName   string // Skill file name
	RawContent string // Raw SKILL.md content
}

// skillNameRe matches "name: xxx" (case-insensitive) at the start of a line.
var skillNameRe = regexp.MustCompile(`(?i)^name:\s*(.+)`)

// extractSkillTitle derives a short human-readable title from raw skill content.
// If the 2nd line matches "name: xxx", the captured value is used.
// Otherwise, the first 100 chars of the raw content are used as a fallback.
func extractSkillTitle(rawContent string) string {
	lines := strings.Split(rawContent, "\n")
	if len(lines) >= 2 {
		secondLine := lines[1]
		if len(secondLine) > 100 {
			secondLine = secondLine[:100]
		}
		if m := skillNameRe.FindStringSubmatch(secondLine); len(m) >= 2 {
			return strings.TrimSpace(m[1])
		}
	}
	if len(rawContent) > 100 {
		return rawContent[:100]
	}
	return rawContent
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

// processingLocks prevents concurrent distillation of the same uploaded skill.
// Keys are uploaded_skill IDs. This is a runtime-only lock — the DB has no
// status field on UploadedSkill, so the lock is lost on process restart
// (which is acceptable: a restart means no goroutine is running).
var processingLocks sync.Map // key: int64, value: struct{}

// IngestSkill creates an UploadedSkill record and a pre-written PublicExperience
// (Status=Generating, empty content), then kicks off background LLM distillation.
// Returns immediately so the frontend can show the new record in the list.
func IngestSkill(ctx context.Context, params IngestSkillParams) (*model.UploadedSkill, error) {
	panicIfNotReady()

	uploaded := &model.UploadedSkill{
		FileName:   params.FileName,
		Title:      extractSkillTitle(params.RawContent),
		RawContent: params.RawContent,
	}
	if err := database.DB.Create(uploaded).Error; err != nil {
		return nil, fmt.Errorf("save uploaded skill: %w", err)
	}

	// Pre-write a PublicExperience with Status=Generating so the frontend can
	// show it immediately. Title is set from the uploaded skill's Title as a
	// placeholder; content fields are empty until distillation completes.
	exp := &model.PublicExperience{
		Title:       uploaded.Title,
		Description: "",
		SourceType:  model.PublicExperienceSourceIngestion,
		SourceID:    uploaded.ID,
		Status:      model.PublicExperienceStatusGenerating,
	}
	if err := database.DB.Create(exp).Error; err != nil {
		applogger.Error("IngestSkill: failed to pre-write public experience",
			"uploaded_skill_id", uploaded.ID,
			"error", err,
		)
		return nil, fmt.Errorf("pre-write public experience: %w", err)
	}

	applogger.Info("IngestSkill: submitted for async processing",
		"uploaded_skill_id", uploaded.ID,
		"public_experience_id", exp.ID,
		"file_name", params.FileName,
	)

	go processIngestion(*uploaded, exp.ID)
	return uploaded, nil
}

// RedistillPublicExperience re-triggers LLM distillation for an existing public
// experience that is in Error (or stuck in Generating) state. Resets the record
// to Generating with empty content, then starts a background goroutine.
func RedistillPublicExperience(ctx context.Context, expID int64) error {
	panicIfNotReady()

	var exp model.PublicExperience
	if err := database.DB.First(&exp, expID).Error; err != nil {
		return fmt.Errorf("public experience not found: %w", err)
	}

	if exp.SourceType != model.PublicExperienceSourceIngestion {
		return fmt.Errorf("only ingestion-sourced experiences can be re-distilled")
	}

	var uploaded model.UploadedSkill
	if err := database.DB.First(&uploaded, exp.SourceID).Error; err != nil {
		return fmt.Errorf("uploaded skill not found: %w", err)
	}

	// Reset to generating state. Title is restored from the uploaded skill's
	// Title as a placeholder; content fields are cleared.
	if err := database.DB.Model(&exp).Updates(map[string]interface{}{
		"title":       uploaded.Title,
		"description": "",
		"when_to_use": "",
		"guidelines":  "",
		"pitfalls":    "",
		"procedure":   "",
		"status":      model.PublicExperienceStatusGenerating,
	}).Error; err != nil {
		return fmt.Errorf("reset public experience: %w", err)
	}

	applogger.Info("RedistillPublicExperience: re-distilling",
		"public_experience_id", expID,
		"uploaded_skill_id", uploaded.ID,
	)

	go processIngestion(uploaded, expID)
	return nil
}

// processIngestion runs the LLM distillation and updates the pre-written
// PublicExperience. Runs in a goroutine. Uses a runtime sync.Map lock to
// prevent concurrent processing of the same uploaded skill.
func processIngestion(uploaded model.UploadedSkill, expID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Runtime lock — prevents concurrent distillation of the same skill.
	if _, loaded := processingLocks.LoadOrStore(uploaded.ID, struct{}{}); loaded {
		applogger.Info("processIngestion: already in progress",
			"uploaded_skill_id", uploaded.ID,
		)
		return
	}
	defer processingLocks.Delete(uploaded.ID)

	sysCfg := service.GetSystemLLMConfig()
	if sysCfg == nil {
		applogger.Error("processIngestion: system LLM config not configured",
			"uploaded_skill_id", uploaded.ID,
		)
		markPublicExperienceError(expID)
		return
	}

	chatModel := llm.NewChatModelWithTemperature(
		sysCfg.BaseURL, sysCfg.APIKey, sysCfg.ModelID, llm.TemperatureControlled,
	)

	schema := llm.GenerateSchema[ingestOutput]()

	prompt := `Refine an external skill document into a host-agnostic experience record.

The content below comes from a skill file (SKILL.md). Classify the skill first:

1. **Method skills** — teach a workflow, decision framework, or technique.
   - Keep: actionable advice, decision heuristics, workflow steps, failure modes, trigger conditions, and concrete technical details (domain APIs like Canvas/fillText, library names, function signatures, algorithm steps).
   - Discard: host-environment coupling (system-specific tools like write_notes/wake_me_when, internal APIs, system config, script paths like start.sh/stop.sh), instructions for humans.

2. **Reference skills** — primarily list specific values (color codes, font names, API endpoints, file paths, company/product names, environment-specific strings).
   - Extract the structural pattern only — not the values.
   - Use placeholder notation like <primary-color>, <heading-font>, <api-base-url> when describing the pattern.
   - If no structural pattern remains after removing values, output empty fields and set skip=true.

What to discard:
- Host-environment coupling (both types): system-specific tool names (e.g. write_notes, wake_me_when), internal APIs, system config, script paths (e.g. start.sh, stop.sh).
- Specific values (reference skills only): color hex codes, font names, API URLs, file paths, company/product names — replace with placeholder notation.
- Instructions written for human developers (both types).

What to keep:
- Domain technical details (both types): APIs like Canvas/fillText, library names, function signatures, algorithm steps — these are actionable knowledge that transfers across systems, not host coupling.

Fill each output field as follows:

title: A short, transferable principle name derived from the skill's core method.

description: One sentence stating the core skill insight — used for semantic matching.

when_to_use: What task signatures, trigger phrases, or problem patterns indicate this skill applies. Leave empty if broadly applicable.

guidelines: Actionable advice with rationale. What to do, why, and in what order.

pitfalls: Known failure modes. What can go wrong, early warning signs, and how to prevent or recover.

procedure: Numbered steps. Only include if a repeatable workflow is described. Leave empty if none.

## Skill content
` + "```markdown\n" + uploaded.RawContent + "\n```"

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
			"public_experience_id", expID,
			"error", err,
		)
		markPublicExperienceError(expID)
		return
	}

	var output ingestOutput
	if err := json.Unmarshal([]byte(response), &output); err != nil {
		applogger.Error("processIngestion: parse LLM output failed",
			"uploaded_skill_id", uploaded.ID,
			"public_experience_id", expID,
			"error", err,
		)
		markPublicExperienceError(expID)
		return
	}

	if output.Skip {
		applogger.Info("processIngestion: nothing worth extracting, deleting pre-written record",
			"uploaded_skill_id", uploaded.ID,
			"public_experience_id", expID,
			"file_name", uploaded.FileName,
		)
		deletePublicExperience(expID)
		return
	}

	if output.Title == "" || output.Description == "" {
		applogger.Error("processIngestion: LLM output missing required fields",
			"uploaded_skill_id", uploaded.ID,
			"public_experience_id", expID,
			"title", output.Title,
			"description", output.Description,
		)
		markPublicExperienceError(expID)
		return
	}

	if err := finalizePublicExperience(ctx, expID, output); err != nil {
		applogger.Error("processIngestion: finalize failed",
			"uploaded_skill_id", uploaded.ID,
			"public_experience_id", expID,
			"error", err,
		)
		markPublicExperienceError(expID)
		return
	}

	applogger.Info("processIngestion: public experience finalized",
		"uploaded_skill_id", uploaded.ID,
		"public_experience_id", expID,
		"file_name", uploaded.FileName,
	)
}
