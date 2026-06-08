package runtime

// AgentStateManager has been removed. Agent runtime status (idle/thinking/
// responding/interacting) is now managed on ParticipantSession.Status in
// the database. The AgentRuntime.setStatus method writes directly to the
// participant_sessions table and fires the SSE callback.
//
// This eliminates the dual-state problem where runtime status was stored
// in-memory while the canonical source of truth should be the session
// participation record.
