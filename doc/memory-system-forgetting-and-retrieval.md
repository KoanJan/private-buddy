# Memory System: Forgetting, Retrieval, and Narrative Understanding

How we design a long-term memory system for LLM-based agents — a system that, by default, remembers nothing, and only retains what repeated use demands.

---

## The Problem

An LLM-based agent can only know what is placed in its prompt. A chat session's conversation history is a finite resource — the earlier messages are compressed into summaries, the later ones held verbatim; everything sits in a single prompt window. When a session ends, the window closes. If the agent interacts with the same user in a new session, it starts fresh — no recollection of prior conversations, no accumulated understanding of the person, no memory of its own previous actions.

The same applies within a session: the agent processes one message at a time. Each API call is a new instance with no shared state. Without explicit memory infrastructure, the agent cannot deliberately recall relevant past events when forming a response — it must rely entirely on whatever context the framing layer happens to provide.

This is not just a recall problem. It is a knowledge integration problem. The agent produces observations every time it processes a message — not for the user (the reply handles that), but for itself. These observations constitute its experience. Without memory, the agent throws away its experience after each invocation.

The question is not whether to build memory, but what kind of memory: a system designed around purposeful forgetting, or one that accumulates indiscriminately.

---

## Part 1: Theoretical Foundation

### The Forgetting Model: Why Memory Must Be Lossy

The intuitive approach to machine memory is preservation: record everything, then search it. This approach has two problems.

**First, the signal-to-noise ratio decays with scale.** Most conversational events are routine — greetings, small talk, transient coordination. A memory system that records everything is a system whose retrieval mechanism must filter through noise to find signal. The cost of filtering grows with the corpus; relevance degrades as unrelated events saturate the search space.

**Second, indiscriminate preservation overfits to the past.** An agent that remembers every interaction treats its entire history as equally relevant to the present. But past conversations reflect past contexts — moods, tasks, environments that may no longer apply. A memory that cannot fade is a memory that cannot adapt.

The alternative: **a forgetting-first model.** By default, nothing is remembered. Retention is use-driven:

- Every new event is mechanically recorded as an observation — a passive trace, not an active memory.
- Only when an observation is *retrieved and used* (i.e., injected into a prompt to assist a response) does it begin to accumulate importance.
- Importance is the sole measure of retention value. It rises on use and decays continuously with disuse — every observation fades a little each day, but actively retrieved ones can outpace the decay through retrieval boosts.
- Observations that are never retrieved drift asymptotically toward zero — slow enough to avoid premature loss, but inexorable enough to ensure genuinely unused content fades.

This model is grounded in two well-established cognitive phenomena. **Use-dependent retention** (Anderson & Schooler, 1991) demonstrates that human memory strength follows the pattern of environmental demand — items needed more frequently are retained better, and the retention function closely tracks actual usage probability. **Forgetting as adaptive regulation** (Bjork & Bjork, 1992) reframes forgetting not as a failure of recall but as a functional mechanism: by reducing interference from outdated information, forgetting improves retrieval of currently relevant content. An agent that remembers a user's obsolete preferences is worse at serving their current needs than an agent that has let those preferences fade.

An important distinction: this is **not** recency-only decay. Pure recency-based forgetting (e.g., simple exponential decay) assumes time alone determines relevance — older items fade regardless of their importance. Use-dependent retention preserves items that are repeatedly accessed even if they are old. A core user preference revealed months ago and referenced frequently stays strong; a transient chat from last week disappears without reinforcement. The system forgets *what is not used*, not *what is old*.

### The Two-Way Relationship with Retrieval

In most memory systems, retrieval reads and storage writes. They are separate phases with a unidirectional flow. In our design, retrieval is also a write operation: every retrieval hit modifies the memory it touches.

When an observation is retrieved and injected into the agent's context:

```
importance += α × (1 - importance)         # asymptote toward 1.0
last_accessed_at = NOW()
```

The importance update follows an asymptotic curve: the first hit moves it from 0.5 to 0.55, the next to 0.595, then 0.6355 — each successive hit contributes less. This saturating gain means no observation can reach 1.0 from hits alone, and the first few retrievals carry the most information about what deserves retention.

Retrieval also triggers **relevance propagation**: a fraction of the importance gain spreads to related observations — those temporally adjacent in the conversation, those semantically similar (cosine > 0.8), and those in the same session. Propagation is one-way (increase only) and independently applies anti-hot cooldowns to prevent feedback loops. This captures a subtler aspect of use-dependent retention: retrieving one memory often reactivates associated memories, a process with empirical support in the spreading activation literature (Collins & Loftus, 1975).

### Importance as Retention Signal

The `importance` field serves as the system's sole retention signal. An observation starts at importance=0.5. When retrieved or reached by relevance propagation, importance rises above 0.5 — this increment is the system's behavioral signal that the observation matters.

**Decay**: Every 24 hours, all active observations undergo multiplicative decay: `importance *= 0.98`. This is a continuous, uniform process — no binary gates, no special conditions. An observation at 0.85 decays to ~0.48 after 30 days without reinforcement. One at 0.55 (retrieved once) drops to ~0.50 after 5 days and ~0.30 after 30 days. A never-retrieved observation at 0.50 drops to ~0.27 after 30 days.

**Boost outpaces decay for active content**: An observation retrieved roughly once every 5 days will hover around steady state, because a single boost (+0.05 at 0.5) roughly compensates 5 days of decay (×0.98^5 ≈ ×0.904, i.e., ~5% loss from 0.5). More frequent retrievals drive importance upward despite decay. Observations that are never retrieved drift toward zero asymptotically — slow enough to avoid premature loss of potentially relevant content (30+ days before a 0.5 observation falls below 0.3), but inexorable enough that genuinely unused content eventually vanishes from retrieval results.

**Relevance propagation also counters decay**: An observation that received a propagated delta can have importance > 0.5 even without direct retrieval. The propagation path provides a second defense against decay — semantically or temporally related observations are protected by association.

**The continuous nature matters**: Continuous decay means every observation is slowly losing strength. Once-useful but now-obsolete content will eventually fade, regardless of whether it was retrieved in the distant past. The system has genuine forgetting, not just a one-time filter.

Note that decay is multiplicative, so importance never reaches exactly zero (barring floating-point underflow after thousands of days). Observations with importance below a practical floor (≤ 1e-6) are excluded from daily decay updates and from the retrieval set via the `importance > 0` filter — they have been effectively forgotten.

### Memory vs. Notes: Orthogonal Permanence Mechanisms

This system operates alongside the existing notes mechanism (task-execution-level shared context as described in the task execution document). They serve distinct, non-overlapping functions:

| Aspect | Notes (`notes.md`) | Memory (observations) |
|--------|-------------------|----------------------|
| **Scope** | Single task execution | Cross-session, agent-wide |
| **Purpose** | Task continuity (what-to-do-next) | Experience accumulation (what-I've-seen) |
| **Writer** | Agent during task execution (deliberate) | System (mechanical recording) |
| **Reader** | Next LLM instance in the same task | Retrieval engine during context assembly |
| **Lifetime** | One task; discarded on completion | Potentially indefinite, decay-driven |
| **Content** | Deliberate reasoning, decisions, progress | Raw event content, loaded on demand |
| **Selection** | Agent chooses what to write | System records everything; retrieval selects |

Notes are a communication channel across instances of the same task — a deliberate, agent-controlled artifact. Memory is a passive, system-controlled record that the agent may draw from but does not directly manage. The agent writes notes to its future self within a task; the system builds memory from the agent's experience across all tasks.

---

## Part 2: Architecture

### Two-Layer Design: Observation → Reflection

The memory system operates in two distinct layers:

```
Layer 1 (Observation): Mechanical recording of events
    ↓
Layer 2 (EntityProfile): LLM-generated reflection on accumulated observations
```

**Layer 1 — Observation** is fully automated and zero-LLM. When a message is created (user or agent), the system:
1. Creates an `Event` record (event_type=message, ref_id=message_id)
2. Generates an embedding vector for the message content (via the configured embedding service) and stores it as an `EventVector`
3. Creates an `AgentObservation` for each agent participating in the message's session, with default scores (importance=0.5)

This mechanical recording ensures nothing is lost before it can be evaluated. The observation contains no content — event content is loaded on demand via `event_id → events → (event_type, ref_id) → originating table`. This separation keeps the observation table lightweight and avoids content duplication.

**Layer 2 — EntityProfile** is LLM-driven and triggered by density. When an agent accumulates sufficient observations pointing to a specific entity (user, agent, or session), a reflection is triggered:

1. Check density during heartbeat (adaptive interval, performed periodically)
2. Count observations per entity direction (each observation can point to multiple: session + user + agent)
3. When count >= threshold for a direction, select top-N by importance for LLM reflection
4. Generate a fresh narrative (no prior narrative is fed to the LLM) describing the agent's understanding of the entity
5. MD5 dedup: if the evidence text is unchanged from the last generation, skip

EntityProfile is deliberately not retrieval-focused. It is a synthesis layer — the agent forms an impression. This impression can be loaded into the agent's system prompt as background understanding, complementing the point retrieval of specific observations during context assembly.

### Retrieval: Semantic Search with Composite Scoring

When the agent needs to recall relevant past events (e.g., during context assembly for a new message), the Search function performs semantic retrieval:

```
query = current_message + recent_context  (concatenated text)
query_embedding = EmbeddingService(query)

For each eligible observation (importance > 0):
    similarity = cosine_similarity(query_embedding, event_vector)
    recency = 1 / (1 + days_since_last_access)
    composite = 0.7 × similarity + 0.2 × importance + 0.1 × recency

Return top-K sorted by composite score
```

The composite score balances three signals:

- **Semantic relevance (0.7)**: how well the observation's content matches the current query context. This is the dominant factor — memory retrieval should primarily serve the present need. The system retrieves what is relevant, not what is important.
- **Importance (0.2)**: how significant the observation has proven to be over time. This provides a mild boost to reinforced content, preventing a superficial semantic match from completely dominating. But relevance to the query always carries more weight than historical importance.
- **Recency (0.1)**: a minimal temporal bias toward recently accessed content. Freshly reinforced memories are slightly preferred over stale ones with similar semantic+importance scores, but recency alone does not determine ranking.

After retrieval, every top-K result receives a retrieval boost (importance + last_accessed_at updated) — retrieval reinforces memory across the entire result set. Relevance propagation is triggered only from the top result: a single importance delta spreads to related observations, forming a closed loop — retrieval reinforces memory, which improves future retrieval.

### Relevance Propagation: Spreading Activation

When an observation receives an importance delta (from retrieval hit or RAG hit), a fraction spreads to related observations:

| Propagation Rule | Factor | Rationale |
|-----------------|--------|-----------|
| Temporally adjacent (±1 event) | 0.5 | Immediate conversational neighbors share strong topical continuity |
| Temporally near (±2 events) | 0.2 | Weaker continuity with a two-step gap |
| Semantically similar (cosine > 0.8) | 0.2 | Topical association across time and sessions |
| Same session | 0.15 | All events in the same session share a contextual frame |

Each target independently applies an anti-hot cooldown (10 minutes) — if a target was scored within the cooldown window, the propagation is ignored for that target. This prevents a single retrieval from triggering cascading updates that artificially inflate scores across the observation population.

The factors form a hierarchy: temporal adjacency is weighted most heavily because conversational flow provides the strongest evidence of relatedness. Semantic similarity is weighted lower to prevent overfitting to embedding proximity (embeddings can produce false-positives for lexically similar but semantically distinct content). Same-session propagation is lightest — it captures the broad contextual connection without over-weighting it.

### Daily Maintenance: The Decay Cycle

Every 24 hours, a cron process applies multiplicative decay to all active observations:

```
importance *= 0.98   (applied to every observation with importance > 1e-6)
```

This is a uniform, continuous operation — every observation loses 2% of its importance per day, regardless of its current value.

The decay factor of 0.98 was chosen to balance two concerns:

- **Too fast** (e.g., 0.90): observations would vanish within weeks, losing content before it has a chance to prove useful. A message from last month's conversation that suddenly becomes relevant today would already be gone.
- **Too slow** (e.g., 0.995): decay would be negligible — an observation at 0.85 would take 200 days to drop to ~0.31. Obsolete content would linger indefinitely, and the system would behave like a binary gate in practice.

At 0.98, the decay provides a meaningful gradient: actively used content stays strong (retrieval boost > decay loss), occasionally used content slowly fades, and never-used content sinks below practical relevance within 1-3 months.

Observations with importance below 1e-6 are excluded from decay updates — at this point they are effectively zero and do not meaningfully participate in retrieval.

The row is never deleted — it remains in the table as an archival trace. If a propagated relevance wave from a related observation reaches it, the importance reactivates above zero and it re-enters the active set. This is a soft fade, not a hard delete — the cost of preserving the row (a few bytes) is negligible compared to the benefit of recoverability.

---

## Part 3: Design Decisions and Dialectical Analysis

### Why No Automated Semantic Clustering

Many memory systems group observations into topic clusters automatically (e.g., via embedding-based clustering or topic modeling). We deliberately avoid this.

**Arguments for clustering**: Clustering could provide structure — grouping related observations for batch retrieval, summarization, or navigation. It could also serve as a compression mechanism, replacing multiple similar observations with a single cluster summary.

**Arguments against**: Automated clustering overfits to embedding space geometry. Two observations with high cosine similarity may be related (both discuss technical architecture) or may be false-positives (both use similar vocabulary about different topics). The embedding space does not encode semantic truth — it encodes distributional proximity. Relying on it for structural organization introduces a layer of misrepresentation that propagates downstream.

More fundamentally, clustering creates a maintenance burden without clear retrieval benefit. Cluster boundaries shift as new observations arrive; cluster summaries become stale; cluster labels require regeneration. Each maintenance cycle introduces churn. A flat, scored table with semantic search at query time avoids all of this — it defers structure to the moment of retrieval, where it is driven by the specific query context rather than predetermined categories.

**Our choice**: Flat scoring + query-time retrieval. EntityProfile provides a separate synthesis path — LLM-generated narrative that is explicitly about understanding, not categorization. The two mechanisms are complementary: retrieval handles "what is relevant right now," EntityProfile handles "what do I think about this entity overall."

### Why Event Content Is Not Cached in Observations

Observations store only a reference (`event_id`). Content is loaded on demand when the observation is retrieved. This introduces a join on every retrieval — a cost that could be avoided by caching content in the observation row.

**Why we accept this cost**: Content duplication creates a consistency problem. If a message is edited (unlikely in the current system but architecturally possible) or if content is normalized post-hoc, cached copies diverge. The event_id reference guarantees a single source of truth.

More importantly, observations belong to agents — multiple agents in the same session each get an observation record for the same event. Caching content in every observation would multiply storage for no retrieval benefit, since content is identical across agents. The join cost is a one-time query overhead; the duplication cost is permanent.

### Why a Single Importance Dimension

A single `importance` field serves as the system's sole retention signal, rather than multiple scoring dimensions (e.g., intensity, surprise, importance).

**Why a single dimension suffices**: Intensity — higher for longer, denser messages — correlates strongly with what retrieval naturally surfaces: important messages tend to be substantive. Surprise — measured as 1 minus cosine similarity with recent messaging — captures deviations that are already handled by the semantic search: unusual messages produce distinctive embeddings.

The marginal benefit of separate dimensions did not justify the implementation complexity. Each additional dimension requires its own update logic, decay function, and tuning parameters. More importantly, the agent has no cognitive model of intensity or surprise — these are system-level judgments imposed on the agent's experience. Importance, in contrast, is determined by the agent's own behavior (retrieval frequency), making it a behavioral rather than prescribed metric.

**The honest limitation**: A single dimension means the system cannot distinguish between "frequently retrieved because important" and "frequently retrieved because the agent keeps encountering similar situations." A topic that recurs frequently in conversation (e.g., daily status updates) will accumulate high importance even if no single occurrence was particularly significant. We accept this because retrieval frequency in this system serves as the operational definition of importance — if the agent consistently needs to recall information about a topic, that information is important by definition.

### Rate Limiting vs. Continuous Profile Regeneration

EntityProfile generation is rate-limited to once per 6 hours per entity direction. An alternative would be continuous regeneration — every heartbeat triggering a new profile if the density threshold is met.

**Why we rate-limit**: Continuous regeneration would create unnecessary LLM cost without proportional benefit. EntityProfile reflects patterns accumulated across many observations — a pattern does not meaningfully change hour by hour. Six hours provides a window long enough for meaningful new evidence to accumulate while keeping LLM cost bounded.

**The trade-off**: Rate limiting means the profile can lag behind the most recent observations by up to 6 hours. This is acceptable because EntityProfile is a synthesis layer, not a real-time reflection. It captures patterns, not latest messages. A 6-hour lag in pattern recognition is negligible; a 6-hour lag in a real-time response is not.

### Why No Prior Narrative in Profile Generation

When generating a new EntityProfile, we do not feed the prior narrative to the LLM. Each generation is fresh — the LLM sees only the current top evidence and forms a new impression.

**Arguments for including prior narrative**: Continuity — the old narrative could provide a starting point, reducing LLM cost and ensuring consistency across regenerations.

**Arguments against**: Including prior narrative creates a self-reinforcing loop. The LLM, presented with its own previous impression, is biased toward confirming it — even when new evidence might suggest a revision. The system would converge toward a stable but potentially inaccurate self-image, resistant to contradictory observations.

More subtly, prior narrative introduces temporal anchoring. A profile formed early in the agent's interaction with a user would persist through subsequent regenerations, each generation built on the last. The profile becomes a tradition, not a reflection.

**Our choice**: Fresh generation with MD5 dedup. The MD5 check prevents redundant LLM calls when evidence is unchanged, but when evidence changes, the generation starts from scratch. This prioritizes accuracy over continuity, accepting that profiles may shift between regenerations — which is appropriate for a system whose purpose is understanding, not storytelling.

### Identity-Driven Memory: Why the Agent Remembers as Itself

A subtler design choice runs through the entire memory system: the agent never encounters the label "Assistant," "AI," "user," or "agent" in its own memory records. All evidence labels use real names — the agent's own name and the person's name. The EntityProfile reflection prompt addresses the agent in the second person: "You are Alice, reflecting on your accumulated observations about Patrik."

This is not cosmetic. It is a deliberate stance on agency and embodiment.

#### The Sycophancy Problem

When an LLM is told it is "an AI assistant," it activates training patterns associated with reinforcement learning from human feedback (RLHF) alignment — specifically the helpful-harmless-honest (HHH) objective. A well-documented side effect of HHH training is sycophancy: the model agrees with the user, avoids disagreement, and tailors responses to what it believes the user wants to hear (Perez et al., 2022; Sharma et al., 2023). An "assistant" has no independent perspective — its role is to serve.

By contrast, when the system prompt and memory records use the agent's character name ("Alice"), the pre-training associations with HHH alignment are bypassed. The agent is not "an assistant reflecting on its interactions with a user" — it is "Alice reflecting on its conversations with Patrik." The shift from role-archetype to named identity breaks the sycophancy affordance: a person with a name can hold opinions, disagree, and form independent judgments in a way an "assistant" cannot.

Empirically, this matters for memory quality. An agent that remembers itself as "the assistant" produces self-referential narratives that orbit its functional role: "I helped the user with X, I clarified Y for the user." An agent that remembers itself by name produces narratives about relationships: "Patrik tends to approach technical problems methodically. I've noticed he values concrete examples over abstract explanations." The latter is memory; the former is a service log.

#### First-Person Observation, Not Third-Person Logging

Conventional dialogue systems log conversations in third person: "User said X. Assistant replied Y." This framing positions the system as an external observer of an interaction between two parties — neither of which is the system itself. Memory in this frame is a transcript.

Our memory system uses first-person framing. The agent is not observing "User said X, Assistant said Y" — it is observing "[Patrik] said X, [Alice] said Y." Both parties are named persons in a shared conversation. The agent's own messages are tagged with its own name, not "Assistant." The person's messages are tagged with their configured name, not "User."

This distinction has a precise cognitive parallel: episodic memory (Tulving, 1972) is memory for personally experienced events, encoded from the first-person perspective. Semantic memory is memory for facts, encoded independently of personal experience. An assistant reading a transcript exercises semantic memory — it knows what happened. A person recalling a conversation exercises episodic memory — it remembers experiencing it.

Our design pushes toward the episodic. The agent's observations are its own experiences. The agent's EntityProfile is its own reflection on those experiences — "What do I think about X?" — not a system-generated summary of "What happened between the user and the assistant about X?"

#### Evidence Labels as Embodiment Reinforcement

The EntityProfile prompt illustrates this concretely:

```
You are Alice, reflecting on your accumulated observations about Patrik.
Below are the key observations you've recorded (your messages are labeled with your name).

Key observations:
- [Patrik] I've been thinking about switching to Rust for the backend...
- [Alice] That's an interesting direction. What aspects of Rust appeal to you?
- [Patrik] Memory safety without garbage collection, mostly...
```

Every evidence line carries a name. The LLM reading this prompt is not reasoning about "user messages versus assistant messages" — it is reasoning about what Alice has learned about Patrik, and what Alice has said to Patrik. The labels anchor the agent in its identity and the other party in theirs.

This extends beyond EntityProfile. The same naming convention propagates through all memory-adjacent prompt construction — summaries, preprocessing, user state inference, task rewriting. The entire system is built on the premise that conversations happen between named individuals, not generic roles.

#### Why Not Use the Agent's Own Name for Self-Reference in Retrieval Context?

A legitimate counterargument: if the agent is always referred to by its real name, wouldn't a retrieved memory injected into context read unnaturally when the agent encounters "Alice said X" in what it perceives as its own current conversation?

This is precisely the point. The agent *should* encounter its own name when reading retrieved memories, because retrieved memories are its own past experiences. When a person recalls a past conversation, they remember themselves as a participant — "I said X" — not as a disembodied observer. The name label makes the retrieval explicit: this is something I experienced, not something that happened to someone else.

#### The Boundary: Where We Do Use "assistant"

This design is consistent but not total. The LLM API protocol layer uses `role: "assistant"` because this is an OpenAI-compatible API requirement — the model's training expects this token. But the role label never reaches the agent's prompt content. The protocol layer's "assistant" is a transport encoding; it does not define the agent's identity.

Similarly, the `messages` table uses `role = 2` (integer enum for assistant) for storage efficiency. This is a database encoding, not a semantic label. The agent never sees it.

---

## Part 4: System Integration

### Ingestion Hooks

Memory ingestion is triggered at the message creation boundary:

- **User messages**: The API handler calls `SubmitVectorization` after persisting the message
- **Agent messages**: The runtime calls `SubmitVectorization` after the agent generates a reply

The `SubmitVectorization` function is non-blocking — it enqueues the message for background processing (event creation → embedding generation → observation creation). If the queue is full, the task is silently dropped. This design ensures that memory ingestion never blocks the primary message flow.

### Retrieval Integration Points

Memory retrieval is not yet wired into context assembly. The architecture defines two integration points:

1. **RAG hit processing** (`OnRAGHit`): The existing session-level RAG system retrieves historically relevant message segments for the prompt. `OnRAGHit` bridges these RAG-selected messages into the memory system, applying retrieval boosts to the corresponding observations. This is currently operational.

2. **Direct semantic search** (`Search`): Context assembly would call Search with the current query + recent conversation as the search text, retrieving top-K memories for injection into the system prompt. This is architecturally defined and implemented but not yet integrated into the prompt assembly pipeline.

### Heartbeat-Driven Density Check

EntityProfile density checks are driven by the agent heartbeat system (adaptive interval: 5 minutes active, 30 minutes steady, 2 hours dormant). During each heartbeat, the runtime calls `CheckProfileDensity` for the agent, which scans observations and triggers profile generation for eligible entity directions.

This integration is intentionally lightweight — density checks are read-only scans; profile generation is spawned asynchronously. The heartbeat continues uninterrupted regardless of profile generation outcome.

---

## References

- Anderson, J. R., & Schooler, L. J. (1991). Reflections of the environment in memory. *Psychological Science, 2*(6), 396–408.
- Bjork, R. A., & Bjork, E. L. (1992). A new theory of disuse and an old theory of stimulus fluctuation. In A. Healy, S. Kosslyn, & R. Shiffrin (Eds.), *From Learning Processes to Cognitive Processes: Essays in Honor of William K. Estes, Vol. 2* (pp. 35–67). Erlbaum.
- Collins, A. M., & Loftus, E. F. (1975). A spreading-activation theory of semantic processing. *Psychological Review, 82*(6), 407–428.
- Perez, E., Ringer, S., Lukošiūtė, K., et al. (2022). Discovering language model behaviors with model-written evaluations. *arXiv preprint arXiv:2212.09251*.
- Sharma, M., Tong, M., Korbak, T., et al. (2023). Towards understanding sycophancy in language models. *arXiv preprint arXiv:2310.13548*.
- Tulving, E. (1972). Episodic and semantic memory. In E. Tulving & W. Donaldson (Eds.), *Organization of Memory* (pp. 381–403). Academic Press.
- Wegner, D. M. (1987). Transactive memory: A contemporary analysis of the group mind. In B. Mullen & G. R. Goethals (Eds.), *Theories of Group Behavior* (pp. 185–208). Springer.
