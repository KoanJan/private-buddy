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

type ChatModel struct {
	client      *openai.Client
	modelID     string
	maxTokens   int
	temperature float32
}

func NewChatModel(baseURL, apiKey, modelID string, maxTokens int) *ChatModel {
	cfg := openai.DefaultConfig(apiKey)
	if baseURL != "" {
		cfg.BaseURL = baseURL
	}
	return &ChatModel{
		client:    openai.NewClientWithConfig(cfg),
		modelID:   modelID,
		maxTokens: maxTokens,
	}
}

func NewChatModelWithTemperature(baseURL, apiKey, modelID string, maxTokens int, temperature float32) *ChatModel {
	cfg := openai.DefaultConfig(apiKey)
	if baseURL != "" {
		cfg.BaseURL = baseURL
	}
	return &ChatModel{
		client:      openai.NewClientWithConfig(cfg),
		modelID:     modelID,
		maxTokens:   maxTokens,
		temperature: temperature,
	}
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

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

type JSONSchemaDefinition struct {
	Name        string                `json:"name"`
	Description string                `json:"description,omitempty"`
	Schema      jsonschema.Definition `json:"schema"`
	Strict      bool                  `json:"strict"`
}

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
