package memory

import (
	"context"
	"crypto/md5"
	"fmt"
	"strings"
	"time"

	"private-buddy-server/internal/database"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service/llm"

	applogger "private-buddy-server/internal/logger"
)

// Entity direction constants for density detection grouping.
// Each observation can contribute to multiple entity directions (e.g., a
// message belongs to both a session and a user, so it counts toward both
// the session profile and the user profile).
const (
	entityDirSession = iota + 1
	entityDirUser
	entityDirAgent
)

// Profile generation constants.
const (
	profileTriggerMin   = 10  // 10 Minimum observations in one direction to trigger generation
	profileTopK         = 50  // Top K observations selected for LLM reflection (by importance DESC, id DESC as tiebreaker)
	profileRateLimitMin = 360 // 360 Minutes between profile regenerations for the same entity
)

// entityDirection is a composite key for grouping observations by entity.
type entityDirection struct {
	EntityType int
	EntityID   int64
}

// CheckDensity scans an agent's observations and identifies entity directions
// with enough observations to warrant EntityProfile generation. For each
// eligible direction, it triggers LLM reflection asynchronously.
//
// Evidence selection is done by top-K importance (with id DESC as tiebreaker),
// not survival_count. See loadProfileEvidences.
//
// Returns the number of profiles triggered.
func checkDensity(ctx context.Context, agentID int64) int {
	// Load all observations for this agent
	var observations []model.AgentObservation
	if err := database.DB.Where("agent_id = ?", agentID).
		Order("id").Find(&observations).Error; err != nil {
		applogger.Warn("EntityProfile density check: failed to load observations",
			"agent_id", agentID, "error", err)
		return 0
	}

	if len(observations) < profileTriggerMin {
		applogger.Debug("EntityProfile density check: insufficient observations",
			"agent_id", agentID,
			"qualified", len(observations),
			"required", profileTriggerMin,
		)
		return 0
	}

	// Build entity direction counts from observations
	directionCounts := resolveEntityDirections(observations)

	triggered := 0
	for dir, count := range directionCounts {
		if count < profileTriggerMin {
			continue
		}
		if isRateLimited(dir.EntityType, dir.EntityID, agentID) {
			applogger.Debug("EntityProfile rate limited",
				"agent_id", agentID,
				"entity_type", dir.EntityType,
				"entity_id", dir.EntityID,
			)
			continue
		}
		go generateProfile(ctx, agentID, dir.EntityType, dir.EntityID)
		triggered++
	}

	return triggered
}

// resolveEntityDirections maps observations to entity directions by walking
// the event → message → session → participants chain.
//
// Each observation can map to multiple entity directions. For example, a
// user message in a session contributes to both the user profile and the
// session profile.
func resolveEntityDirections(observations []model.AgentObservation) map[entityDirection]int {
	// Collect unique event IDs
	eventIDs := make(map[int64]bool)
	for _, o := range observations {
		eventIDs[o.EventID] = true
	}

	// Batch-load events
	var events []model.Event
	ids := make([]int64, 0, len(eventIDs))
	for id := range eventIDs {
		ids = append(ids, id)
	}
	if len(ids) > 0 {
		if err := database.DB.Where("id IN ?", ids).Find(&events).Error; err != nil {
			applogger.Warn("resolveEntityDirections: failed to load events", "error", err)
		}
	}

	// Map event_id → event for quick lookup
	eventMap := make(map[int64]model.Event)
	for _, e := range events {
		eventMap[e.ID] = e
	}

	// Collect unique message ref_ids (only for message-type events)
	msgIDs := make(map[int64]bool)
	for _, e := range events {
		if e.EventType == model.EventTypeMessage {
			msgIDs[e.RefID] = true
		}
	}

	// Batch-load messages
	var messages []model.Message
	mids := make([]int64, 0, len(msgIDs))
	for id := range msgIDs {
		mids = append(mids, id)
	}
	if len(mids) == 0 {
		return nil
	}
	if err := database.DB.Where("id IN ?", mids).Find(&messages).Error; err != nil {
		applogger.Warn("resolveEntityDirections: failed to load messages", "error", err)
		return nil
	}
	msgMap := make(map[int64]model.Message)
	sessionIDs := make(map[int64]bool)
	for _, m := range messages {
		msgMap[m.ID] = m
		sessionIDs[m.SessionID] = true
	}

	// Batch-load sessions
	var sessions []model.Session
	sids := make([]int64, 0, len(sessionIDs))
	for id := range sessionIDs {
		sids = append(sids, id)
	}
	if len(sids) > 0 {
		if err := database.DB.Where("id IN ?", sids).Find(&sessions).Error; err != nil {
			applogger.Warn("resolveEntityDirections: failed to load sessions", "error", err)
		}
	}
	sessionMap := make(map[int64]model.Session)
	for _, s := range sessions {
		sessionMap[s.ID] = s
	}

	// Batch-load participant sessions to find users
	participantMap := make(map[int64]int64) // session_id → first user_id
	var participants []model.ParticipantSession
	if len(sids) > 0 {
		if err := database.DB.Where("session_id IN ? AND participant_type = ?", sids, model.ParticipantTypeUser).
			Find(&participants).Error; err != nil {
			applogger.Warn("resolveEntityDirections: failed to load participants", "error", err)
		}
	}
	seenUsers := make(map[int64]bool)
	for _, p := range participants {
		if !seenUsers[p.SessionID] {
			participantMap[p.SessionID] = p.ParticipantID
			seenUsers[p.SessionID] = true
		}
	}

	// Count observations per entity direction
	counts := make(map[entityDirection]int)
	dedup := make(map[entityDirection]map[int64]bool) // dir → set of observation IDs

	for _, o := range observations {
		ev, ok := eventMap[o.EventID]
		if !ok || ev.EventType != model.EventTypeMessage {
			continue
		}

		msg, ok := msgMap[ev.RefID]
		if !ok {
			continue
		}

		// Direction 1: Session
		{
			dir := entityDirection{EntityType: model.EntityTypeSession, EntityID: msg.SessionID}
			if dedup[dir] == nil {
				dedup[dir] = make(map[int64]bool)
			}
			if !dedup[dir][o.ID] {
				dedup[dir][o.ID] = true
				counts[dir]++
			}
		}

		// Direction 2: User (who sent the message, if role=user)
		if msg.Role == model.MessageRoleUser {
			userID, ok := participantMap[msg.SessionID]
			if ok {
				dir := entityDirection{EntityType: model.EntityTypeUser, EntityID: userID}
				if dedup[dir] == nil {
					dedup[dir] = make(map[int64]bool)
				}
				if !dedup[dir][o.ID] {
					dedup[dir][o.ID] = true
					counts[dir]++
				}
			}
		}

		// Direction 3: Agent (the agent who owns the session)
		if sess, ok := sessionMap[msg.SessionID]; ok {
			dir := entityDirection{EntityType: model.EntityTypeAgent, EntityID: sess.AgentID}
			if dedup[dir] == nil {
				dedup[dir] = make(map[int64]bool)
			}
			if !dedup[dir][o.ID] {
				dedup[dir][o.ID] = true
				counts[dir]++
			}
		}
	}

	return counts
}

// isRateLimited checks whether a profile was recently updated (within the
// rate limit window). Returns true if the profile should be skipped.
func isRateLimited(entityType int, entityID, agentID int64) bool {
	var profile model.EntityProfile
	err := database.DB.Where("agent_id = ? AND entity_type = ? AND entity_id = ?",
		agentID, entityType, entityID).First(&profile).Error
	if err != nil {
		return false // No existing profile, not rate limited
	}
	return time.Since(profile.LastUpdatedAt) < profileRateLimitMin*time.Minute
}

// generateProfile triggers LLM reflection to produce an EntityProfile
// narrative for the given agent/entity combination.
//
// It loads the top-K observations (by importance) for the entity direction,
// formats them as evidence, and asks the LLM to synthesize a fresh narrative
// (no prior narrative is fed). MD5 of the evidence text is compared with the
// existing profile's input_md5 — if unchanged, generation is skipped.
func generateProfile(ctx context.Context, agentID int64, entityType int, entityID int64) {
	applogger.Info("Generating EntityProfile",
		"agent_id", agentID,
		"entity_type", entityType,
		"entity_id", entityID,
	)

	// Resolve entity label for the prompt
	entityName, ok := entityLabel(entityType, entityID)
	if !ok {
		applogger.Error("EntityProfile: failed to resolve entity label",
			"entity_type", entityType, "entity_id", entityID)
		return
	}

	// Resolve agent name for self-referencing in the prompt
	agent := getAgent(agentID)
	if agent == nil {
		applogger.Error("EntityProfile: agent not found", "agent_id", agentID)
		return
	}
	agentName := agent.Name

	// Load relevant observations with event content
	evidences := loadProfileEvidences(agentID, entityType, entityID, agentName)

	// Build the LLM prompt
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`You are %s, reflecting on your accumulated observations about %s.
Below are the key observations you've recorded (your messages are labeled with your name).
Based on these, write a concise narrative describing your understanding and impression of %s.

Focus on patterns, traits, preferences, or notable characteristics. Be honest about what
you know and what remains uncertain. Do not fabricate observations.

Key observations:
`, agentName, entityName, entityName))

	for i, ev := range evidences {
		sb.WriteString(fmt.Sprintf("- [Observation %d] %s\n", i+1, ev))
	}

	sb.WriteString("\nOutput: a single paragraph of 2-6 sentences.")

	promptText := sb.String()

	// Compute MD5 of the evidence text to detect unchanged input
	inputHash := fmt.Sprintf("%x", md5.Sum([]byte(promptText)))

	var existingProfile model.EntityProfile
	if err := database.DB.Where("agent_id = ? AND entity_type = ? AND entity_id = ?",
		agentID, entityType, entityID).First(&existingProfile).Error; err != nil {
		applogger.Warn("EntityProfile: failed to check existing profile before generation",
			"agent_id", agentID, "entity_type", entityType, "entity_id", entityID, "error", err)
	}

	if existingProfile.ID != 0 && existingProfile.InputMD5 == inputHash && existingProfile.InputMD5 != "" {
		applogger.Info("EntityProfile input unchanged, skipping regeneration",
			"agent_id", agentID,
			"entity_type", entityType,
			"entity_id", entityID,
			"md5", inputHash,
		)
		return
	}

	llmConfig := getLLMConfig(agent.LLMConfigID)
	if llmConfig == nil {
		applogger.Error("EntityProfile: LLM config not found", "agent_id", agentID)
		return
	}

	chatModel := llm.NewChatModelWithTemperature(
		llmConfig.BaseURL, llmConfig.APIKey, llmConfig.ModelID, llm.TemperatureDeterministic,
	)

	narrative, err := chatModel.Chat(ctx, []llm.Message{
		{Role: "user", Content: promptText},
	})

	if err != nil {
		applogger.Error("EntityProfile LLM call failed",
			"agent_id", agentID,
			"entity_type", entityType,
			"entity_id", entityID,
			"error", err,
		)
		return
	}

	narrative = strings.TrimSpace(narrative)
	if narrative == "" {
		applogger.Warn("EntityProfile LLM returned empty narrative",
			"agent_id", agentID,
		)
		return
	}

	// Upsert profile
	evidenceCount := len(evidences)
	if existingProfile.ID != 0 {
		if err := database.DB.Model(&existingProfile).Updates(map[string]interface{}{
			"narrative":       narrative,
			"evidence_count":  evidenceCount,
			"input_md5":       inputHash,
			"last_updated_at": time.Now(),
		}).Error; err != nil {
			applogger.Error("EntityProfile: failed to update profile",
				"agent_id", agentID, "entity_type", entityType, "error", err)
			return
		}
	} else {
		profile := model.EntityProfile{
			AgentID:       agentID,
			EntityType:    entityType,
			EntityID:      entityID,
			Narrative:     narrative,
			EvidenceCount: evidenceCount,
			InputMD5:      inputHash,
		}
		if err := database.DB.Create(&profile).Error; err != nil {
			applogger.Error("EntityProfile: failed to create profile", "agent_id", agentID, "entity_type", entityType, "error", err)
			return
		}
	}

	applogger.Info("EntityProfile generated",
		"agent_id", agentID,
		"entity_type", entityType,
		"entity_id", entityID,
		"evidence_count", evidenceCount,
		"narrative_len", len(narrative),
	)
}

// entityLabel returns a human-readable label for the entity type/ID combination.
// Returns (label, true) on success, ("", false) if the entity name cannot be resolved.
func entityLabel(entityType int, entityID int64) (string, bool) {
	switch entityType {
	case model.EntityTypeUser:
		var user model.User
		if err := database.DB.Where("id = ?", entityID).Select("name").First(&user).Error; err != nil {
			applogger.Error("entityLabel: failed to load user name", "entity_id", entityID, "error", err)
			return "", false
		}
		return user.Name, true
	case model.EntityTypeAgent:
		var agent model.Agent
		if err := database.DB.Where("id = ?", entityID).Select("name").First(&agent).Error; err != nil {
			applogger.Error("entityLabel: failed to load agent name", "entity_id", entityID, "error", err)
			return "", false
		}
		return agent.Name, true
	case model.EntityTypeSession:
		return fmt.Sprintf("session #%d", entityID), true
	default:
		return fmt.Sprintf("entity (type=%d, id=%d)", entityType, entityID), true
	}
}

// loadProfileEvidences loads message content for observations relevant to the
// given entity direction, formatted for inclusion in the LLM reflection prompt.
//
// Selection: top profileTopK observations sorted by importance DESC (id DESC as
// tiebreaker for equal importance). No survival_count gate.
func loadProfileEvidences(agentID int64, entityType int, entityID int64, agentName string) []string {
	// Load all observations ordered by importance DESC, then id DESC for recency tiebreaking.
	var observations []model.AgentObservation
	if err := database.DB.Where("agent_id = ?", agentID).
		Order("importance DESC, id DESC").
		Find(&observations).Error; err != nil {
		applogger.Error("loadProfileEvidences: failed to load observations",
			"agent_id", agentID, "error", err)
		return nil
	}

	if len(observations) == 0 {
		return nil
	}

	// Collect event IDs
	eventIDs := make([]int64, 0, len(observations))
	for _, o := range observations {
		eventIDs = append(eventIDs, o.EventID)
	}

	// Load events (only message-type)
	var events []model.Event
	if err := database.DB.Where("id IN ? AND event_type = ?", eventIDs, model.EventTypeMessage).Find(&events).Error; err != nil {
		applogger.Warn("loadProfileEvidences: failed to load events", "error", err)
	}
	eventMap := make(map[int64]model.Event)
	refIDs := make([]int64, 0, len(events))
	for _, e := range events {
		eventMap[e.ID] = e
		refIDs = append(refIDs, e.RefID)
	}
	if len(refIDs) == 0 {
		return nil
	}

	// Load messages
	var messages []model.Message
	if err := database.DB.Where("id IN ?", refIDs).Find(&messages).Error; err != nil {
		applogger.Warn("loadProfileEvidences: failed to load messages", "error", err)
		return nil
	}
	msgMap := make(map[int64]model.Message)
	sessionIDs := make(map[int64]bool)
	for _, m := range messages {
		msgMap[m.ID] = m
		sessionIDs[m.SessionID] = true
	}

	sids := make([]int64, 0, len(sessionIDs))
	for id := range sessionIDs {
		sids = append(sids, id)
	}

	// Load sessions
	var sessions []model.Session
	if len(sids) > 0 {
		if err := database.DB.Where("id IN ?", sids).Find(&sessions).Error; err != nil {
			applogger.Warn("loadProfileEvidences: failed to load sessions", "error", err)
		}
	}
	sessionMap := make(map[int64]model.Session)
	for _, s := range sessions {
		sessionMap[s.ID] = s
	}

	// Map session_id → user_id for resolving user names in evidence labels.
	var psList []model.ParticipantSession
	participantMap := make(map[int64]int64)
	userIDSet := make(map[int64]bool)
	if len(sids) > 0 {
		if err := database.DB.Where("session_id IN ? AND participant_type = ?", sids, model.ParticipantTypeUser).
			Find(&psList).Error; err != nil {
			applogger.Warn("loadProfileEvidences: failed to load participants", "error", err)
		}
	}
	seen := make(map[int64]bool)
	for _, p := range psList {
		if !seen[p.SessionID] {
			participantMap[p.SessionID] = p.ParticipantID
			seen[p.SessionID] = true
			userIDSet[p.ParticipantID] = true
		}
	}

	// Map user_id → user name
	userNameMap := make(map[int64]string)
	if len(userIDSet) > 0 {
		uids := make([]int64, 0, len(userIDSet))
		for uid := range userIDSet {
			uids = append(uids, uid)
		}
		var users []model.User
		if err := database.DB.Where("id IN ?", uids).Select("id, name").Find(&users).Error; err != nil {
			applogger.Warn("loadProfileEvidences: failed to load user names", "error", err)
		}
		for _, u := range users {
			userNameMap[u.ID] = u.Name
		}
	}

	// Walk observations in importance order, collect top K matching the entity direction.
	var evidences []string
	for _, o := range observations {
		ev, ok := eventMap[o.EventID]
		if !ok {
			continue
		}
		msg, ok := msgMap[ev.RefID]
		if !ok {
			continue
		}

		matches := false
		switch entityType {
		case model.EntityTypeSession:
			matches = msg.SessionID == entityID
		case model.EntityTypeUser:
			userID, ok := participantMap[msg.SessionID]
			matches = ok && userID == entityID
		case model.EntityTypeAgent:
			sess, ok := sessionMap[msg.SessionID]
			matches = ok && sess.AgentID == entityID
		}

		if !matches {
			continue
		}

		// Resolve the label for this message based on role.
		roleLabel := agentName // default: this agent's message
		if msg.Role == model.MessageRoleUser {
			userID, hasUser := participantMap[msg.SessionID]
			if hasUser {
				roleLabel = userNameMap[userID]
			}
		}
		evidences = append(evidences, fmt.Sprintf("[%s] %s", roleLabel, msg.Content))

		if len(evidences) >= profileTopK {
			break
		}
	}

	return evidences
}

// getAgent retrieves the agent model by ID.
func getAgent(agentID int64) *model.Agent {
	var agent model.Agent
	if err := database.DB.First(&agent, agentID).Error; err != nil {
		return nil
	}
	return &agent
}

// getLLMConfig retrieves the LLM config by ID.
func getLLMConfig(configID int64) *model.LLMConfig {
	var cfg model.LLMConfig
	if err := database.DB.First(&cfg, configID).Error; err != nil {
		return nil
	}
	return &cfg
}

// LoadProfileForEntity returns the narrative from an agent's EntityProfile for
// a specific entity (user/agent/session). Returns empty string if no profile exists.
func LoadProfileForEntity(agentID int64, entityType int, entityID int64) string {
	var profile model.EntityProfile
	err := database.DB.Where("agent_id = ? AND entity_type = ? AND entity_id = ?",
		agentID, entityType, entityID).First(&profile).Error
	if err != nil {
		return ""
	}
	return profile.Narrative
}
