// Package memory implements the agent-level long-term memory system.
//
// This is NOT the session-level conversation summary (Summary).
// Summary provides per-session compressed text — non-retrievable,
// non-updatable per fact, non-reusable across sessions. The memory system
// provides cross-session, retrievable, score-driven persistent memory at
// the agent level: an agent in session B can recall what happened in session A.
//
// # Core paradigm: forgetting model
//
// By default, nothing is remembered. Retention is use-driven:
//   - Every external event is mechanically mirrored as an observation
//     (no LLM, importance=0.5).
//   - When an observation is retrieved and injected into context, its
//     importance increases (asymptote toward 1.0) with anti-hot protection.
//   - All observations undergo daily multiplicative decay: importance *= 0.98.
//     This ensures unused observations slowly fade, asymptotically approaching
//     but never reaching zero. Actively retrieved observations can outpace
//     decay through retrieval boosts and relevance propagation.
//
// # Layers
//
//   - observation: mechanical record of "what happened" — zero LLM.
//     Event content is loaded on demand via event_id, not cached here.
//   - EntityProfile: LLM-generated reflection triggered by density detection:
//     when >= 5 observations point to the same entity (user/agent/session),
//     the top 50 are selected by importance for LLM reflection.
//
// # Scoring
//
// Only one continuous dimension: importance (0.0–1.0).
// Updated on retrieval hit with anti-hot protection (cooldown: 10 minutes).
// Composite retrieval score: 0.7*similarity + 0.2*importance + 0.1*recency.
// Key moments are flagged at importance >= 0.85.
//
// Relevance propagation: when an observation is hit, a fraction of the
// importance delta spreads to temporally adjacent, semantically similar,
// and same-session observations (one-way, increase only).
//
// # File layout
//
//   - memory.go: package API (Init/Start/OnRetrievalHit/CheckProfileDensity),
//     ingestion, retrieval hit processing.
//   - event_vector.go: SubmitVectorization entry point and background
//     goroutine for event → embedding → observation creation.
//   - ingester.go: event table writes, embedding storage, observation creation.
//   - score.go: importance algorithm, relevance propagation, cosine similarity.
//   - cron.go: daily cleanup of stale observations (importance zeroing).
//   - entity_profile.go: EntityProfile density detection and LLM-generated
//     narrative reflection.
//
// # API surface
//
//   - Init(embeddingSvc): sets the embedding service reference. Idempotent.
//   - Start(ctx): launches background goroutines (vectorization, daily cron)
//     bound to ctx. Cancelling ctx shuts them down gracefully.
//   - SubmitVectorization(task): enqueues a message for event vectorization.
//     Non-blocking; drops if queue is full.
//   - OnRetrievalHit(agentConfigID, messageIDs): applies retrieval hits from
//     context engineering to the memory system's observations.
//   - CheckProfileDensity(ctx, agentConfigID): scans long-term observations and
//     triggers EntityProfile generation when density thresholds are met.
//
// # Background services
//
//   - Vectorization goroutine: serialises event → vector work on a dedicated
//     goroutine. Callers submit tasks; the goroutine owns the queue and lifecycle.
//   - Daily cron: runs immediately on startup, then every 24 hours.
//     Applies multiplicative importance decay (importance *= 0.98) to all
//     active observations.
//
// # Interaction with other modules
//
//   - cmd/main.go: Init + Start during application startup.
//   - api/handler/*.go: SubmitVectorization for user/agent messages.
//   - service/runtime/agent_runtime.go: SubmitVectorization for agent messages.
//   - service/runtime/heartbeat_checks.go: CheckProfileDensity during heartbeat.
//   - service/chat/chat_service.go: OnRetrievalHit from session-level retrieval.
package memory
