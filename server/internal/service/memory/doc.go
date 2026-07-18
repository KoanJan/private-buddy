// Package memory implements the agent's long-term memory system.
//
// This is a package-level singleton service. Call Init(embeddingSvc) once at startup,
// then Start(ctx) to launch background services. Package-level functions are safe to
// call afterwards.
//
// # Event-Observation-EntityProfile pipeline
//
// The memory system transforms raw messages into structured knowledge in three stages:
//
//  1. Event creation (ingestMessage, doc.go): each message creates an Event record
//     with an embedding (EventVector) for semantic retrieval. Observations are
//     automatically created for every AI participant in the session, giving each
//     agent its own perspective on the same event.
//
//  2. Observation scoring (score.go): each observation has an importance score that
//     starts at 0.5 and evolves through three mechanisms:
//     - Retrieval hit: when a chat-history segment is retrieved and injected into
//     context, onRetrievalHit applies an anti-hot cooldown (10 minutes) and
//         pushes importance asymptotically toward 1.0 (delta = α × (1 - importance)).
//     - Relevance propagation (propagateRelevance): the delta spreads to related
//     observations via three rules — temporal adjacency (±1: 0.5x, ±2: 0.2x),
//     semantic similarity (cosine > 0.8: 0.2x), same-session (0.15x). This is
//     the v4 replacement for v2's Semantic layer clustering.
//     - Daily decay (runDailyMaintenance): all observations multiply by 0.98 daily,
//     providing a gradual forgetting mechanism. Observations near zero are skipped.
//
//  3. EntityProfile generation (entity_profile.go): when observations for a given
//     entity direction (session or person) exceed profileTriggerMin (10), an
//     asynchronous LLM reflection synthesizes a narrative. Rate-limited to one
//     generation per 6 hours per entity. MD5 of input text is compared to skip
//     unchanged inputs. Two entity directions are tracked:
//     - EntityTypeSession: what happened in this session
//     - EntityTypePerson: what we know about this person (user or other participant)
//
//     LoadProfileForEntity() exposes these narratives to the chat context engine.
//
// # Background services (Start)
//
//   - Event vectorization goroutine: queues embeddings for newly created events.
//   - Daily cron: runs maintenance immediately on start, then every 24 hours,
//     applying multiplicative importance decay to all active observations.
//
// # Thread safety
//
// Init/Start are guarded by sync.Once. Package-level read functions (LoadProfileForEntity,
// CheckProfileDensity, OnRetrievalHit) are safe to call concurrently. Relevance
// propagation runs in goroutines per hit.
//
// # OnRetrievalHit — the bridging function
//
// OnRetrievalHit is called by the chat context engine after chat-history retrieval.
// Given the personID and messageIDs, it:
//   - Finds corresponding events and observations
//   - Applies retrieval hit scoring to matching observations
//   - Launches background goroutines for relevance propagation (temporal + semantic + same-session)
//
// This is the point where the chat pipeline feeds back into the memory system.
package memory
