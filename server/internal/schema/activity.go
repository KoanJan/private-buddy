package schema

// ActivityEvent represents a single event in the agent's activity timeline.
type ActivityEvent struct {
	Time    string `json:"time"`             // Formatted timestamp (2006-01-02 15:04:05)
	Type    string `json:"type"`             // "thinking" | "tool_call" | "guidance"
	Content string `json:"content"`          // Raw content: thinking text, guidance text, or empty for tool_call
	Tool    string `json:"tool,omitempty"`   // Only for tool_call: tool name
	Target  string `json:"target,omitempty"` // Only for tool_call: extracted target from arguments
	AgentID int64  `json:"agent_id"`         // Agent that produced this event
}
