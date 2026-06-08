package runtime

// WorkRouter determines whether an event should be routed to an active Work
// or start a new one. Returns nil to indicate a new Work should be created.
type WorkRouter interface {
	Route(event AgentEvent, activeWorks []*Work) *Work
}

// SemanticWorkRouter uses LLM to judge whether an event is semantically
// related to an active Work, based on Work descriptions.
//
// Direct semantic routing replaces heuristic rules — designing a good
// heuristic is itself complex, and LLM judgment with Work descriptions
// is both simpler and more accurate.
type SemanticWorkRouter struct {
	// llmClient will be injected when the LLM integration is complete.
	// Currently, routing falls back to session-based matching.
}

// NewSemanticWorkRouter creates a new semantic work router.
func NewSemanticWorkRouter() *SemanticWorkRouter {
	return &SemanticWorkRouter{}
}

// Route determines which active Work should handle the event.
// If no active Work is semantically related, returns nil to start a new Work.
//
// Current implementation: session-based routing as a simple first step.
// LLM-based semantic routing will be added in a follow-up iteration
// once the runtime is integrated and functional.
func (r *SemanticWorkRouter) Route(event AgentEvent, activeWorks []*Work) *Work {
	if len(activeWorks) == 0 {
		return nil
	}

	// Session-based routing: if there's an active work in the same session,
	// route the event to it. This is the simplest correct behavior for 1v1 chats.
	for _, w := range activeWorks {
		if w.sessionID == event.SessionID {
			return w
		}
	}

	// No active work in this session — start a new one
	return nil
}
