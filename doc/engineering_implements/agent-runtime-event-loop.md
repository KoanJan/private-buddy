# Agent Runtime: Event Loop & Work Lifecycle — Engineering Implementation

This document describes how the agent runtime processes incoming events, transitions between Comprehend→Decide→Execute phases, and manages the lifecycle of Work (Chat/Task) that produces agent responses.

## Architecture Overview

```mermaid
graph TB
    subgraph "External Producers"
        Handler["HTTP Handler<br/>user messages, events"]
        TaskTools["Task Tools<br/>wake_me_when, etc."]
        Scheduler["Scheduler<br/>timed alarms"]
    end

    subgraph "Event Transport"
        EQ["eventqueue<br/>per-agent buffered channel<br/>(buffer=64)"]
    end

    subgraph "Agent Runtime (one goroutine per agent)"
        Loop["Event Loop<br/>for-select over eventCh + heartbeatTimer"]
        Comprehend["Comprehend Phase<br/>parallel: preprocessing + person_state + KB retrieval"]
        Decide["Decide Phase<br/>LLM-driven decision → Create/Route/Cancel"]
        Execute["Execute Phase<br/>start ChatWork or TaskWork"]
        ActiveWorks["activeWorks<br/>running works, keyed by session_id"]
    end

    subgraph "Work Execution"
        ChatWork["ChatWork<br/>one-shot LLM → draft → commit"]
        TaskWork["TaskWork<br/>ReAct loop → TaskLoop"]
        DraftCh["draftCommitCh<br/>serialized draft → message"]
    end

    subgraph "Heartbeat"
        HB["Three-phase Tickless<br/>Active(5m) → Steady(30m) → Dormant(2h)"]
        MemCheck["Memory density check"]
        RefCheck["Reflection check"]
        LearnCheck["Learning check"]
    end

    Handler --> EQ
    TaskTools --> EQ
    Scheduler --> EQ
    EQ --> Loop
    Loop --> Comprehend
    Comprehend --> Decide
    Decide --> Execute
    Execute --> ChatWork
    Execute --> TaskWork
    ChatWork --> DraftCh
    Loop -.heartbeat ticks.-> HB
    HB --> MemCheck
    HB --> RefCheck
    HB --> LearnCheck
```

The runtime is a global singleton (`globalRuntimeManager` in manager.go). Each agent gets one `agentRuntime` with a dedicated goroutine running an event loop. The event loop processes one event at a time, serializing all agent work.

## Event Loop

The event loop is the heart of the runtime, running in a dedicated goroutine per agent:

```mermaid
flowchart TD
    Start["Event loop running"] --> Select{"select"}
    
    Select -->|event from eventCh| Process["Process event"]
    Process --> PreRead["mark last_read_message_id"]
    PreRead --> FastPath{"Fast path?<br/>(ScheduledEvent + ActionSendMessage)"}
    FastPath -->|yes| SkipLLM["Compose response directly<br/>→ commit draft → push SSE"]
    FastPath -->|no| ComprehendPhase["Run Comprehend Phase"]
    SkipLLM --> LoopContinue["Continue loop"]
    ComprehendPhase --> DecidePhase["Run Decide Phase"]
    DecidePhase --> ExecActions["Execute Actions<br/>(Create / Route / Cancel)"]
    ExecActions --> LoopContinue
    
    Select -->|heartbeat tick| HBPhase["Run heartbeat phase<br/>(check memory/reflection/learning)"]
    HBPhase --> LoopContinue
    
    Select -->|ctx.Done| Drain["Drain all works<br/>→ shutdown"]
```

### Event handling flow

1. **Pre-processing**: Mark `last_read_message_id` on the participant session. Detect fast-path events (scheduled events with `ActionSendMessage`) that can bypass LLM entirely.

2. **Comprehend**: Runs the three-part parallel comprehension phase (preprocessing, person state inference, KB retrieval). This produces a `ComprehensionResult` with query classification, rewritten query, keywords, and retrieved segments. See [context-engineering-pipeline.md](./context-engineering-pipeline.md) for details.

3. **Decide**: The LLM receives the comprehension context and produces a `DecisionResult` with zero or more `Actions`. The decision schema is deterministic (TemperatureDeterministic) and structured via JSON Schema. See [Decision Phase](#decision-phase) below.

4. **Execute**: Each action is executed:
   - `ActionCreate`: creates a new Work (Chat or Task), adds it to `activeWorks`
   - `ActionRoute`: routes to an existing running TaskWork (via guidance channel)
   - `ActionCancel`: sends an appealable cancel directive to an existing TaskWork

### Event type routing

| Event Type | Source | Handling |
|---|---|---|
| `NewPrivateChatMessage` | User sends a message | Mark read → Comprehend → Decide → Execute |
| `WorkCompleted` | Work finishes (task loop or chat) | Remove from activeWorks → Rule-based Decide → Execute |
| `Scheduled` | Alarm fires (heartbeat + alarm goroutine) | Fast-path check → Comprehend → Rule-based Decide |
| `AlarmCreated` | `wake_me_when` tool result | AlarmRegistry registers goroutine |
| `GroupChatJoined` / `GroupChatLeft` / `SystemNotification` | System events | Direct return (no action) |

## Comprehend Phase

The Comprehend phase runs three parallel tasks to understand the incoming event context. For full details, see [context-engineering-pipeline.md](./context-engineering-pipeline.md). The phase is structured as:

```mermaid
flowchart LR
    subgraph "Parallel Tasks"
        A["Preprocessing<br/>query classification, rewriting<br/>keyword extraction"]
        B["Person State Inference<br/>emotion, purpose, situation<br/>→ NeedsWorldInteraction"]
    end
    subgraph "Sequential (depends on Preprocessing)"
        C["KB Retrieval<br/>search agent's linked KBs<br/>with processed query"]
    end
    A -->|wg.Wait| C
    B -->|wg.Wait| C
```

The preprocessing step classifies whether the user message contains a query (`clear`, `ambiguous`, `vague`, `no_query`), rewrites contextual queries into standalone form, and extracts keywords. Person state inference uses the last N messages to infer emotion, purpose, and situation, producing a `NeedsWorldInteraction` boolean that influences the Decide phase.

## Decide Phase

The Decide phase determines what action to take. It uses LLM-driven decision for `NewPrivateChatMessage` events, and rule-based decision for `WorkCompleted` and non-fast-path `Scheduled` events (which always trigger a ChatWork response):

```mermaid
flowchart TD
    Input["ComprehensionResult<br/>+ active works<br/>+ agent context"] --> BuildPrompt["Build decision prompt<br/>system rules + comprehension + works status"]
    BuildPrompt --> CallLLM["Call LLM<br/>TemperatureDeterministic<br/>JSON Schema strict"]
    CallLLM --> Validate["Validate output<br/>filterValidActions"]
    
    Validate -->|empty actions| Idle["No action<br/>(don't interrupt existing work)"]
    Validate -->|valid actions| Dispatch["Dispatch each action"]
    
    Dispatch --> Create{"ActionCreate?"}
    Create -->|chat work| NewChat["newChatWork<br/>setup context → stream LLM → draft → commit"]
    Create -->|task work| NewTask["newTaskWork<br/>setup executor → TaskLoop<br/>→ on complete: draft → commit"]
    
    Dispatch --> Route{"ActionRoute?"}
    Route -->|session has running TaskWork| Guidance["Send directive<br/>via guidanceCh → TaskLoop"]
    
    Dispatch --> Cancel{"ActionCancel?"}
    Cancel --> CancelWork["Send cancel directive<br/>via guidanceCh → TaskLoop<br/>(appealable — agent decides)"]
```

### Action types

| Action | Semantics | Implementation |
|---|---|---|
| `ActionCreate` | Start new work. Chat work for one-shot response; Task work for ReAct loop. | `newWork()` creates Work + Draft in a single transaction, then starts execution in a goroutine. |
| `ActionRoute` | Direct an existing running TaskWork toward a new objective. | Only routes to TaskWork (ChatWork has no loop to absorb the directive). Directive is sent via `guidanceCh`. |
| `ActionCancel` | Ask a running TaskWork to stop. | Appealable — not a forceful kill. Directive is sent via `guidanceCh`; the LLM decides how to wrap up. |

### Decision validation (`filterValidActions`)

The LLM output is validated defensively:
- Actions referencing non-existent or completed works are dropped
- Route/Cancel without a matching active TaskWork are dropped
- If all actions are filtered out, the agent takes no action (silent no-op, not an error)

## Work Lifecycle

Work is the unit of agent execution. There are two types:

### ChatWork

A one-shot LLM call that produces an agent response:

```mermaid
flowchart LR
    Create["Create Work + Draft<br/>(single DB transaction)"] --> Assemble["Assemble chat context<br/>(summary + narrative + segments)"]
    Assemble --> Stream["Stream LLM response<br/>(full response collected)"]
    Stream --> DraftCommit["draftCommitCh<br/>→ serialize → write messages"]
    DraftCommit --> Done["WorkCompleted event<br/>→ eventCh"]
```

- Creates one Draft record for the response
- Uses `chat.ExecuteChat` to build context and stream the LLM
- On completion, sends the response content to `draftCommitCh` for serialized commit
- Fires `EventTypeWorkCompleted` back to the event loop

### TaskWork

A ReAct loop that runs tools to accomplish a goal:

```mermaid
flowchart TD
    Create["Create Work<br/>(single DB transaction)"] --> Setup["Setup executor<br/>ContextManager + tools + TaskLoop"]
    Setup --> Loop["TaskLoop runs<br/>ReAct iterations<br/>(see task-loop-context-management.md)"]
    Loop --> Complete{"Completion?"}
    Complete -->|success or failure| Deferred["Deferred cleanup"]
    Complete -->|cancelled| HandleCancel["Handle cancellation<br/>(appealable)"]
    Deferred --> Done["WorkCompleted event<br/>→ eventCh"]
    HandleCancel --> Done
```

- Uses TaskLoop for ReAct execution (see [task-loop-context-management.md](./task-loop-context-management.md))
- Supports Guidance (directive injection from Decide) and Cancel (appealable, ChatWork falls back to abandon)

### Work ↔ Draft relationship

| Work Type | Draft created? | Draft committed? | Final output |
|---|---|---|---|
| ChatWork | Yes (at creation) | Yes (on completion) | Message in messages table + SSE push |
| TaskWork | **No** | — | Notes in notes.jsonl, files in workspace |

### Draft-based commit architecture

The `draftCommitCh` channel ensures serialized message commits:

```mermaid
flowchart LR
    ChatWorkDone["ChatWork completes<br/>with response text"] --> DraftCh["draftCommitCh<br/>(buffer=16, one per agent)"]
    DraftCh --> CommitGoroutine["commit goroutine<br/>(one per agent)"]
    CommitGoroutine --> WriteMsg["Write messages table<br/>(draft_id on message)"]
    WriteMsg --> PushSSE["Push SSE to session<br/>(connectionManager)"]
    WriteMsg --> Ingest["Submit for memory ingestion<br/>(event + observations)"]
```

This design avoids the "placeholder message" anti-pattern (writing an empty placeholder, then updating it), and guarantees that messages across multiple concurrent ChatWorks are written in a predictable order.

### Active works management

- `activeWorks` is a slice of running works; works are identified by ID via `findActiveWorkByID`
- `hasActiveWorkInSession(sessionID)` checks whether any active work targets the given session
- Works are removed from `activeWorks` when their `WorkCompleted` event is processed

## Heartbeat System

The heartbeat mimics Linux's tickless kernel — the interval elongates as the agent becomes idle:

```mermaid
flowchart TD
    Active["Active phase<br/>5 min interval"] -->|idleTicks >= 4| Steady["Steady phase<br/>30 min interval"]
    Steady -->|idleTicks >= 7| Dormant["Dormant phase<br/>2 hour interval"]
    Steady -.->|any event| Active
    Dormant -.->|any event| Active
    
    Active -.-> Every6Ticks["Every 6 ticks:<br/>CheckMemoryDensity"]
    Active -.-> EveryTick["Every tick:<br/>CheckReflection"]
    Active -.-> Every30Ticks["Every tick (with in-progress guard):<br/>CheckLearning (async)"]
```

### Heartbeat checks

| Check | Frequency | What it does |
|---|---|---|
| `checkMemoryDensity` | Every 6 ticks | Calls `memory.CheckProfileDensity()` to trigger EntityProfile generation when observation density crosses threshold |
| `checkReflection` | Every tick | Calls `experience.CheckReflection()` to scan notes.jsonl for new insights |
| `checkLearning` | Every tick (with in-progress guard) | Calls `experience.CheckLearning()` to discover public experiences worth adopting |

The check functions are lightweight — they enqueue work asynchronously and return immediately. The heartbeat tick itself is fast, ensuring the event loop is never blocked by long-running reflection.

## Alarm System

Alarms implement the `wake_me_when` tool contract — the agent asks to be woken at a specific time:

```mermaid
flowchart TD
    AgentCall["Agent calls wake_me_when<br/>with trigger_at + message"] --> Persist["Persist to scheduled_events<br/>status=pending"]
    Persist --> FireEvent["Fire AlarmCreated event<br/>→ eventCh"]
    FireEvent --> Runtime["Runtime creates alarm goroutine"]
    Runtime --> Wait["Goroutine sleeps until trigger_at"]
    Wait --> Recheck["Re-check DB status<br/>(not yet triggered?)"]
    Recheck -->|still pending| MarkTriggered["Mark status=triggered<br/>in DB"]
    Recheck -->|already triggered/expired| Skip["Skip (idempotent)"]
    MarkTriggered --> PushEvent["Push Scheduled event<br/>→ eventCh"]
    Skip --> End["Goroutine exits"]
```

- Each alarm is a goroutine tracked in `alarmRegistry` keyed by `scheduledEventID`
- On runtime restart, `recoverOrphanAlarms` restores all pending alarms
- The goroutine uses `time.Until(triggerAT)` to sleep, then double-checks DB status before firing (prevents duplicate fires after restart + crash)

## Startup & Recovery

On application startup, the runtime manager performs recovery before starting agent goroutines:

1. **Reset participant sessions**: all AI `participant_sessions` with `status=working` are reset to `idle` (crashed while working)
2. **Recover active works**: `recoverActiveWorks` marks running works as `Abandoned`, discards their drafts, resets participant status
3. **Start agent runtimes**: one `agentRuntime` per `agent_config`
4. **Recover scheduled events**: `recoverScheduledEvents` restores all `scheduled_events` with `status=pending` and `trigger_at` in the future

### Work recovery

```mermaid
flowchart TD
    Query["Find works with status=running"] --> EachWork["For each abandoned work"]
    EachWork --> DiscardDraft["Discard associated draft"]
    DiscardDraft --> MarkAbandoned["Mark work status=abandoned"]
    MarkAbandoned --> ResetParticipant["Reset participant_session status=idle"]
```

Works are not resumable after a crash. The design assumes:
- ChatWorks produce ephemeral responses (a new response is generated on next user message)
- TaskWorks leave their state in notes.jsonl and workspace files (the agent re-reads notes on next task)

## Configuration

| Config | Description | Relationship |
|---|---|---|
| `HeartbeatActiveInterval` | Tick interval when agent recently had events | Default 5 min |
| `HeartbeatSteadyAfter` | Consecutive empty ticks to enter steady phase | Default 3 |
| `HeartbeatSteadyInterval` | Tick interval in steady phase | Default 30 min |
| `HeartbeatDormantAfter` | Additional empty ticks to enter dormant phase | Default 6 |
| `HeartbeatDormantInterval` | Tick interval in dormant phase | Default 2 h |

## Shutdown

```mermaid
flowchart LR
    Signal["SIGTERM/SIGINT"] --> CancelCtx["Cancel runtime context"]
    CancelCtx --> WaitWorks["Wait 10s for works to finish"]
    WaitWorks --> CancelMemory["Cancel memory background services"]
    CancelMemory --> ShutdownSSE["Shutdown SSE connections"]
    ShutdownSSE --> ShutdownHTTP["Shutdown HTTP server (3s)"]
```

The 10-second grace period allows in-progress ChatWorks and TaskWorks to reach a natural stopping point. Works that don't finish in time are abandoned (state persists in DB for next startup recovery).
