// Package tools provides the tool abstractions and implementations for the task agent system.
//
// All tools must implement the Tool interface, which requires a unique name,
// an OpenAI-compatible function definition schema, and an execute method.
// Tools are registered by name and provide their schema for LLM tool calling.
//
// Available tools:
//   - BashTool: Execute shell commands within a workspace
//   - WriteNotesTool: Append structured entries to agent's notes
//   - WebSearchTool: Search the web for information (Tavily provider)
package tools

import "github.com/sashabaranov/go-openai"

// Tool is the interface that all agent tools must implement.
// Each tool has a unique name, an OpenAI-compatible function definition schema,
// and an execute method that performs the actual work.
type Tool interface {
	// Name returns the unique identifier for this tool.
	Name() string
	// Schema returns the OpenAI function calling schema for this tool.
	Schema() openai.FunctionDefinition
	// Execute runs the tool with the given arguments and returns the result as a string.
	Execute(args map[string]interface{}) (string, error)
}
