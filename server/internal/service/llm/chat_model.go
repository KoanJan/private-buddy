// Package llm provides LLM client wrappers for OpenAI-compatible chat completion APIs.
//
// This package implements the Go equivalent of Python's LLMService and TokenUsageLogger,
// providing utilities for creating chat model instances and tracking token usage/latency.
// All LLM calls automatically log token usage and latency in the same format as Python.
package llm

import (
	"context"
	"fmt"
	"io"
	"time"

	openai "github.com/sashabaranov/go-openai"
	"github.com/sashabaranov/go-openai/jsonschema"

	applogger "private-buddy-server/internal/logger"
)

// Temperature presets for different LLM service categories.
//
// These constants centralize all temperature values used across the application,
// replacing scattered magic numbers. Each preset corresponds to a specific
// trade-off between determinism and creativity:
//   - TemperatureDeterministic: structured output services that need consistent JSON
//   - TemperatureControlled: creative generation that should stay on-topic
//   - TemperatureCreative: general conversation and open-ended generation
const (
	TemperatureDeterministic float32 = 0.1
	TemperatureControlled    float32 = 0.3
	TemperatureCreative      float32 = 0.7
)

// ChatModel wraps an OpenAI client with model configuration for chat completions.
// It supports multiple call modes: plain chat, streaming, tool-calling, and JSON schema output.
// Temperature is only sent to the API when explicitly set (> 0), matching LangChain's behavior
// where unset temperature defaults to 0.7 on the server side.
type ChatModel struct {
	client      *openai.Client
	modelID     string
	temperature float32
}

// NewChatModel creates a ChatModel without explicit temperature.
// The API server's default temperature (typically 0.7) will be used.
func NewChatModel(baseURL, apiKey, modelID string) *ChatModel {
	cfg := openai.DefaultConfig(apiKey)
	if baseURL != "" {
		cfg.BaseURL = baseURL
	}
	return &ChatModel{
		client:  openai.NewClientWithConfig(cfg),
		modelID: modelID,
	}
}

// NewChatModelWithTemperature creates a ChatModel with explicit temperature control.
// Use the predefined constants (TemperatureDeterministic, TemperatureControlled, TemperatureCreative)
// instead of raw float values for consistency and readability.
func NewChatModelWithTemperature(baseURL, apiKey, modelID string, temperature float32) *ChatModel {
	cfg := openai.DefaultConfig(apiKey)
	if baseURL != "" {
		cfg.BaseURL = baseURL
	}
	return &ChatModel{
		client:      openai.NewClientWithConfig(cfg),
		modelID:     modelID,
		temperature: temperature,
	}
}

// ChatMessage represents a simple chat message with role and content.
// Used for plain chat, streaming, and JSON schema calls where tool_calls are not needed.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// buildRequest constructs a ChatCompletionRequest from ChatMessage slice.
// Temperature is only included when explicitly set (> 0).
func (cm *ChatModel) buildRequest(messages []ChatMessage) openai.ChatCompletionRequest {
	var reqMessages []openai.ChatCompletionMessage
	for _, m := range messages {
		reqMessages = append(reqMessages, openai.ChatCompletionMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	req := openai.ChatCompletionRequest{
		Model:    cm.modelID,
		Messages: reqMessages,
	}

	if cm.temperature > 0 {
		req.Temperature = cm.temperature
	}

	return req
}

// Chat sends a non-streaming chat completion request and returns the response content.
// Used by services that need a single complete response: summary, narrative, user state, etc.
func (cm *ChatModel) Chat(ctx context.Context, messages []ChatMessage) (string, error) {
	req := cm.buildRequest(messages)

	start := time.Now()
	resp, err := cm.client.CreateChatCompletion(ctx, req)
	latencyMs := float64(time.Since(start).Milliseconds())

	if err != nil {
		applogger.L.Error("llm call failed", "model", cm.modelID, "latency_ms", latencyMs, "error", err)
		return "", fmt.Errorf("chat completion failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response choices returned")
	}

	logTokenUsage(latencyMs, resp.Usage, cm.modelID)

	return resp.Choices[0].Message.Content, nil
}

// ChatStream initiates a streaming chat completion request.
// Returns a stream that must be consumed via ConsumeStream.
// Used for real-time SSE streaming of LLM responses to the frontend.
// StreamOptions.IncludeUsage is set to true to capture token usage in the final chunk.
func (cm *ChatModel) ChatStream(ctx context.Context, messages []ChatMessage) (*openai.ChatCompletionStream, error) {
	req := cm.buildRequest(messages)
	req.Stream = true
	req.StreamOptions = &openai.StreamOptions{
		IncludeUsage: true,
	}

	start := time.Now()
	stream, err := cm.client.CreateChatCompletionStream(ctx, req)
	latencyMs := float64(time.Since(start).Milliseconds())

	if err != nil {
		applogger.L.Error("llm stream call failed", "model", cm.modelID, "latency_ms", latencyMs, "error", err)
		return nil, fmt.Errorf("chat completion stream failed: %w", err)
	}

	applogger.L.Debug("llm stream started", "model", cm.modelID, "connect_latency_ms", latencyMs)

	return stream, nil
}

// ChatWithTools sends a non-streaming chat completion request with tool definitions.
// Used by the task loop's ReAct pattern where the LLM decides which tools to call.
// This is the Go equivalent of Python's TaskLLMClient.invoke(), using the OpenAI Tools API
// (not the deprecated Functions API) for proper tool_calls support.
func (cm *ChatModel) ChatWithTools(ctx context.Context, messages []openai.ChatCompletionMessage, toolDefs []openai.Tool) (*openai.ChatCompletionResponse, error) {
	req := openai.ChatCompletionRequest{
		Model:    cm.modelID,
		Messages: messages,
		Tools:    toolDefs,
	}

	if cm.temperature > 0 {
		req.Temperature = cm.temperature
	}

	start := time.Now()
	resp, err := cm.client.CreateChatCompletion(ctx, req)
	latencyMs := float64(time.Since(start).Milliseconds())

	if err != nil {
		applogger.L.Error("llm call with tools failed", "model", cm.modelID, "latency_ms", latencyMs, "error", err)
		return nil, fmt.Errorf("chat completion with tools failed: %w", err)
	}

	logTokenUsage(latencyMs, resp.Usage, cm.modelID)

	return &resp, nil
}

// JSONSchemaDefinition defines a JSON Schema for structured LLM output.
// Used with ChatWithJSONSchema to force the LLM to respond in a specific JSON format.
// This is the Go equivalent of Python's with_structured_output() / response_format.
type JSONSchemaDefinition struct {
	Name        string                `json:"name"`
	Description string                `json:"description,omitempty"`
	Schema      jsonschema.Definition `json:"schema"`
	Strict      bool                  `json:"strict"`
}

// ChatWithJSONSchema sends a chat completion request with JSON Schema response format.
// Forces the LLM to output valid JSON conforming to the provided schema.
// Used by services that need structured output: user state inference, query routing, requirement rewriting.
// This matches Python's pattern of using with_structured_output() for deterministic JSON responses.
func (cm *ChatModel) ChatWithJSONSchema(ctx context.Context, messages []ChatMessage, schemaDef JSONSchemaDefinition) (string, error) {
	req := cm.buildRequest(messages)

	req.ResponseFormat = &openai.ChatCompletionResponseFormat{
		Type: openai.ChatCompletionResponseFormatTypeJSONSchema,
		JSONSchema: &openai.ChatCompletionResponseFormatJSONSchema{
			Name:        schemaDef.Name,
			Description: schemaDef.Description,
			Schema:      &schemaDef.Schema,
			Strict:      schemaDef.Strict,
		},
	}

	start := time.Now()
	resp, err := cm.client.CreateChatCompletion(ctx, req)
	latencyMs := float64(time.Since(start).Milliseconds())

	if err != nil {
		applogger.L.Error("llm call with json_schema failed", "model", cm.modelID, "latency_ms", latencyMs, "error", err)
		return "", fmt.Errorf("chat completion with json_schema failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response choices returned")
	}

	logTokenUsage(latencyMs, resp.Usage, cm.modelID)

	return resp.Choices[0].Message.Content, nil
}

// StreamHandler is a callback function invoked for each chunk received from a streaming response.
type StreamHandler func(chunk string) error

// logTokenUsage logs LLM token usage in the same format as Python's TokenUsageLogger:
// "llm usage | latency=XXXms | prompt_tokens: XXX | completion_tokens: XXX | total_tokens: XXX | model=XXX"
func logTokenUsage(latencyMs float64, usage openai.Usage, model string) {
	args := []interface{}{
		"latency_ms", latencyMs,
		"prompt_tokens", usage.PromptTokens,
		"completion_tokens", usage.CompletionTokens,
		"total_tokens", usage.TotalTokens,
	}
	if model != "" {
		args = append(args, "model", model)
	}
	applogger.L.Debug("llm usage", args...)
}

// ConsumeStream reads all chunks from a streaming response, invoking the handler for each chunk.
// Returns the full accumulated content when the stream completes.
// Token usage is logged if available in the final stream chunk (via StreamOptions.IncludeUsage).
func (cm *ChatModel) ConsumeStream(stream *openai.ChatCompletionStream, handler StreamHandler) (string, error) {
	defer stream.Close()

	start := time.Now()
	var fullContent string
	var streamUsage openai.Usage
	hasUsage := false

	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fullContent, fmt.Errorf("stream recv error: %w", err)
		}

		if resp.Usage != nil {
			streamUsage = *resp.Usage
			hasUsage = true
		}

		if len(resp.Choices) > 0 {
			delta := resp.Choices[0].Delta.Content
			if delta != "" {
				fullContent += delta
				if handler != nil {
					if err := handler(delta); err != nil {
						return fullContent, err
					}
				}
			}
		}
	}

	latencyMs := float64(time.Since(start).Milliseconds())
	if hasUsage {
		logTokenUsage(latencyMs, streamUsage, cm.modelID)
	} else {
		applogger.L.Debug("llm stream completed", "model", cm.modelID, "latency_ms", latencyMs)
	}

	return fullContent, nil
}
