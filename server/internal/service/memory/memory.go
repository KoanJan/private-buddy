package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"private-buddy-server/internal/database"
	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service"
	"private-buddy-server/internal/service/llm"
	"private-buddy-server/internal/service/vectorstore"

	applogger "private-buddy-server/internal/logger"
)

// Package-level state for the memory system singleton.
var (
	embeddingSvc *llm.EmbeddingService
	initOnce     sync.Once
	ready        atomic.Bool
)

// Init sets the embedding service reference for the memory system.
// Must be called once during application startup, before any memory operations.
// Idempotent: only the first call has effect.
// embeddingSvc may be nil if only profile generation is needed (Search requires it).
func Init(es *llm.EmbeddingService) {
	initOnce.Do(func() {
		embeddingSvc = es
		ready.Store(true)
		applogger.Info("Memory system initialized")
	})
}

func panicIfNotReady() {
	if !ready.Load() {
		panic("Memory system not initialized")
	}
}

// Start launches background services (event vectorization, daily maintenance
// cron) tied to ctx. When ctx is cancelled, goroutines drain remaining work
// and exit gracefully. Init must be called first.
func Start(ctx context.Context) {
	panicIfNotReady()
	go startEventVectorization(ctx)
	go runDailyCron(ctx)
	applogger.Info("Memory background services started")
}

// OnRAGHit is the package-level entry point for applying retrieval hits
// from context-engineering RAG to the memory system.  It is safe to call
// before Init(); the call is silently ignored when the memory service is
// not configured.
func OnRAGHit(agentID int64, messageIDs []int64) {
	panicIfNotReady()
	onRAGHit(agentID, messageIDs)
}

// CheckProfileDensity is the package-level entry point for dimension B
// profile-density checks.  Safe to call before Init().
func CheckProfileDensity(ctx context.Context, agentID int64) int {
	panicIfNotReady()
	return checkDensity(ctx, agentID)
}

// ingestMessage creates event + embedding + observations for a newly created
// message. This is the central ingestion hook called from the business layer
// (API handler for user messages, runtime for agent messages).
func ingestMessage(ctx context.Context, messageID, sessionID int64, content string) {
	// Create event + embedding in one step
	eventID, err := createEventWithEmbedding(ctx, model.EventTypeMessage, messageID, content)
	if err != nil {
		applogger.Error("Failed to ingest message event",
			"message_id", messageID, "error", err)
		return
	}

	// Create observations for all agents participating in this session.
	// Agents are identified by Person type=AI via join with persons table.
	var participants []model.ParticipantSession
	var partErr error
	participants, partErr = service.GetSessionParticipantsByPersonType(sessionID, model.PersonTypeAI)
	if partErr != nil {
		applogger.Error("failed to load participants for observation creation", "session_id", sessionID, "error", partErr)
		return
	}

	for _, p := range participants {
		if err := createObservation(ctx, p.ParticipantID, eventID); err != nil {
			applogger.Error("Failed to create observation for agent",
				"agent_id", p.ParticipantID,
				"event_id", eventID,
				"error", err,
			)
		}
	}

	applogger.Debug("Message ingested into memory system",
		"message_id", messageID,
		"event_id", eventID,
		"agent_count", len(participants),
	)
}

// onRAGHit applies a retrieval hit to observations associated with the given
// chat-history message IDs. Called when existing context engineering RAG
// retrieval fetches historical segments for the LLM prompt — these segments
// represent prior events the agent's memory system should recognise as
// having been recalled.
//
// Unlike Search (semantic search across ALL observations),
// this targets a specific set of message IDs that the RAG system already
// selected via vector similarity.
func onRAGHit(agentID int64, messageIDs []int64) {
	if len(messageIDs) == 0 {
		return
	}

	// Find events for these messages (event_type=1 = EventTypeMessage)
	var events []model.Event
	if err := database.DB.Where("event_type = ? AND ref_id IN ?", model.EventTypeMessage, messageIDs).
		Find(&events).Error; err != nil {
		applogger.Error("processRAGHit: failed to load events", "error", err)
		return
	}

	if len(events) == 0 {
		return
	}

	eventIDs := make([]int64, len(events))
	for i, e := range events {
		eventIDs[i] = e.ID
	}

	// Load observations for this agent that correspond to these events
	var observations []model.AgentObservation
	if err := database.DB.Where("agent_id = ? AND event_id IN ?", agentID, eventIDs).
		Find(&observations).Error; err != nil {
		applogger.Error("processRAGHit: failed to load observations", "error", err)
		return
	}

	hitCount := 0
	for i := range observations {
		obs := &observations[i]
		delta := onRetrievalHit(obs)
		if delta > 0 {
			hitCount++
		}

		// Persist the updated scores
		if err := database.DB.Model(&model.AgentObservation{}).Where("id = ?", obs.ID).Updates(map[string]interface{}{
			"importance":       obs.Importance,
			"last_accessed_at": obs.LastAccessedAt,
			"last_scored_at":   obs.LastScoredAt,
		}).Error; err != nil {
			applogger.Error("processRAGHit: failed to persist observation scores", "obs_id", obs.ID, "error", err)
		}

		// Propagate relevance (time-adjacent, similar, same-session)
		// for hits that moved the importance needle.
		if delta > 0 {
			go propagateRAGHit(obs, delta)
		}
	}

	if hitCount > 0 {
		applogger.Info("RAG hit applied to memory observations",
			"agent_id", agentID,
			"message_ids", len(messageIDs),
			"observation_count", len(observations),
			"hit_count", hitCount,
		)
	}
}

// propagateRAGHit runs relevance propagation for a RAG-retrieved observation.
// Uses a best-effort approach: loads observations from the same session and
// applies temporal adjacency propagation.
func propagateRAGHit(obs *model.AgentObservation, delta float64) {
	sessionID := getEventSessionID(obs.EventID)
	if sessionID == 0 {
		return
	}

	var sessionObservations []model.AgentObservation
	if err := database.DB.Where("agent_id = ? AND event_id IN (SELECT id FROM events WHERE event_type = ? AND ref_id IN (SELECT id FROM messages WHERE session_id = ?))",
		obs.AgentID, model.EventTypeMessage, sessionID).
		Find(&sessionObservations).Error; err != nil {
		applogger.Error("propagateRelevanceForHit: failed to load session observations", "agent_id", obs.AgentID, "session_id", sessionID, "error", err)
		return
	}

	// Build the context slice needed by PropagateRelevance
	obsWithCtx := make([]observationWithContext, len(sessionObservations))
	for i, o := range sessionObservations {
		obsWithCtx[i] = observationWithContext{
			ObservationID: o.ID,
			EventID:       o.EventID,
			SessionID:     sessionID,
		}
	}

	params := propagateParams{
		DeltaBase:         delta,
		HitEventID:        obs.EventID,
		HitSessionID:      sessionID,
		AllObservationIDs: obsWithCtx,
	}

	propagateRelevance(params)
}

// memoryResult represents a single retrieved memory for context injection.
type memoryResult struct {
	ObservationID int64   `json:"observation_id"`
	EventID       int64   `json:"event_id"`
	Content       string  `json:"content"`
	Role          string  `json:"role"`
	Score         float64 `json:"score"`
	Similarity    float64 `json:"similarity"`
	Importance    float64 `json:"importance"`
	Recency       float64 `json:"recency"`
	IsKeyMoment   bool    `json:"is_key_moment"`
}

// Search performs memory retrieval for an agent.
//
// The query is typically the current user message concatenated with recent
// conversation messages. The search:
//  1. Loads all eligible observations for the agent
//  2. Computes cosine similarity between query embedding and each event vector
//  3. Sorts by composite score
//  4. Applies survival boost to the hit observations
//  5. Returns top-k results with event content loaded
//
// Parameters:
//   - agentID: the agent whose memories to search
//   - query: the search query text
//   - k: maximum number of results to return
func Search(ctx context.Context, agentID int64, query string, k int) ([]memoryResult, error) {
	// Generate query embedding
	queryEmbedding, err := embeddingSvc.EmbedSingle(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to generate query embedding: %w", err)
	}

	// Load eligible observations (truth_confidence >= 0.3, scores not zeroed)
	observations, err := loadEligibleObservations(agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to load observations: %w", err)
	}

	if len(observations) == 0 {
		return nil, nil
	}

	// Collect observations with their event context for propagation
	obsWithContext := make([]observationWithContext, 0, len(observations))

	// Compute composite scores for each observation
	type scoredItem struct {
		obs     model.AgentObservation
		content string
		role    string
		score   float64
		sim     float64
		recency float64
	}

	var items []scoredItem
	for _, obs := range observations {
		// Get event vector
		var ev model.EventVector
		if err := database.DB.First(&ev, "event_id = ?", obs.EventID).Error; err != nil {
			applogger.Error("failed to find event vector for observation during importance scoring", "event_id", obs.EventID, "error", err)
			continue // Skip observations without vectors
		}
		if len(ev.Embedding) == 0 {
			continue
		}

		storedEmbedding := vectorstore.BlobToFloat32Slice(ev.Embedding)
		if storedEmbedding == nil {
			continue
		}

		// Cosine similarity
		similarity := vectorstore.CosineSimilarity(queryEmbedding, storedEmbedding)

		// Recency: 1 / (1 + days_since_last_access)
		daysSince := time.Since(obs.LastAccessedAt).Hours() / 24
		recency := 1.0 / (1.0 + daysSince)

		// Composite score
		composite := 0.7*similarity +
			0.2*obs.Importance +
			0.1*recency

		// Get event content
		content, role := loadEventContent(obs.EventID)

		items = append(items, scoredItem{
			obs:     obs,
			content: content,
			role:    role,
			score:   composite,
			sim:     similarity,
			recency: recency,
		})

		obsWithContext = append(obsWithContext, observationWithContext{
			ObservationID: obs.ID,
			EventID:       obs.EventID,
			SessionID:     getEventSessionID(obs.EventID),
		})
	}

	// Sort by composite score descending
	sort.Slice(items, func(i, j int) bool {
		return items[i].score > items[j].score
	})

	// Limit to k
	if k > len(items) {
		k = len(items)
	}

	// Build results and apply survival boost
	results := make([]memoryResult, 0, k)
	for i := 0; i < k; i++ {
		item := items[i]

		// Survival boost: update the observation in DB
		delta := applySurvivalBoost(item.obs)

		// Relevance propagation: spread activation from top hit to related observations
		if i == 0 && delta > 0 {
			runPropagation(item.obs.EventID, delta, obsWithContext, queryEmbedding)
		}

		results = append(results, memoryResult{
			ObservationID: item.obs.ID,
			EventID:       item.obs.EventID,
			Content:       item.content,
			Role:          item.role,
			Score:         item.score,
			Similarity:    item.sim,
			Importance:    item.obs.Importance,
			Recency:       item.recency,
			IsKeyMoment:   isKeyMoment(&item.obs),
		})
	}

	applogger.Info("Memory search completed",
		"agent_id", agentID,
		"total_observations", len(observations),
		"results", len(results),
	)

	return results, nil
}

// loadEligibleObservations loads all observations for an agent that are
// eligible for retrieval (importance > 0, not zeroed by cleanup).
func loadEligibleObservations(agentID int64) ([]model.AgentObservation, error) {
	var observations []model.AgentObservation
	err := database.DB.
		Where("agent_id = ? AND importance > 0", agentID).
		Order("event_id DESC").
		Find(&observations).Error
	return observations, err
}

// loadEventContent retrieves the text content and role for an event.
// The content is loaded on demand from the originating table.
func loadEventContent(eventID int64) (content, role string) {
	var event model.Event
	if err := database.DB.First(&event, eventID).Error; err != nil {
		return "", ""
	}

	if event.EventType == model.EventTypeMessage {
		var msg model.Message
		if err := database.DB.First(&msg, event.RefID).Error; err != nil {
			return "", ""
		}
		role = "user"
		userPersonID, err := service.GetCurrentUserPersonID()
		if err != nil {
			applogger.Error("loadEventContent: failed to get current user person ID", "error", err)
		} else if msg.PersonID != userPersonID {
			role = "assistant"
		}
		return msg.Content, role
	}

	return "", ""
}

// getEventSessionID returns the session_id for an event.
// For message events, this comes from the messages table.
func getEventSessionID(eventID int64) int64 {
	var event model.Event
	if err := database.DB.First(&event, eventID).Error; err != nil {
		return 0
	}

	if event.EventType == model.EventTypeMessage {
		var msg model.Message
		if err := database.DB.First(&msg, event.RefID).Error; err != nil {
			return 0
		}
		return msg.SessionID
	}

	return 0
}

// applySurvivalBoost updates an observation's scores when it is retrieved.
// Updates last_accessed_at and optionally importance.
// Returns the importance delta applied (0 if cooldown prevented update).
func applySurvivalBoost(obs model.AgentObservation) float64 {
	delta := onRetrievalHit(&obs)

	// Persist the updated scores
	updates := map[string]interface{}{
		"last_accessed_at": obs.LastAccessedAt,
	}

	if delta > 0 {
		updates["importance"] = obs.Importance
		updates["last_scored_at"] = obs.LastScoredAt
	}

	if err := database.DB.Model(&model.AgentObservation{}).Where("id = ?", obs.ID).Updates(updates).Error; err != nil {
		applogger.Error("Failed to persist survival boost",
			"obs_id", obs.ID, "error", err)
	}

	return delta
}

// runPropagation triggers relevance propagation from a hit observation.
// This spreads the importance gain to temporally adjacent, semantically
// similar, and same-session observations.
func runPropagation(eventID int64, delta float64, obsWithContext []observationWithContext, queryEmbedding []float32) {
	// Get the hit event's embedding for semantic propagation
	var hitEventVector model.EventVector
	if err := database.DB.First(&hitEventVector, "event_id = ?", eventID).Error; err != nil {
		applogger.Error("failed to find hit event vector for relevance propagation", "event_id", eventID, "error", err)
		hitEventVector.Embedding = nil
	}

	var hitEmbedding []float32
	if len(hitEventVector.Embedding) > 0 {
		hitEmbedding = vectorstore.BlobToFloat32Slice(hitEventVector.Embedding)
	}

	params := propagateParams{
		DeltaBase:         delta,
		HitEventID:        eventID,
		HitSessionID:      getEventSessionID(eventID),
		HitEmbedding:      hitEmbedding,
		AllObservationIDs: obsWithContext,
	}

	propagateRelevance(params)
}
