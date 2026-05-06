package chatctx

import (
	stdctx "context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sashabaranov/go-openai/jsonschema"

	"private-buddy-server/internal/model"
	"private-buddy-server/internal/service/llm"

	applogger "private-buddy-server/internal/logger"
)

// Query type constants for classification
const queryTypeClear = "clear"         // Query is complete and unambiguous
const queryTypeAmbiguous = "ambiguous" // Query contains references to previous context
const queryTypeVague = "vague"         // Query is too vague to understand intent
const queryTypeNoQuery = "no_query"    // Query doesn't need retrieval (greetings, etc.)

// routingPrompt is the LLM prompt template for query type classification.
// It takes two parameters: history (formatted conversation) and query (user message).
const routingPrompt = `Analyze the user query type and process accordingly.

Conversation history:
%s

Current user query: %s

Classify the query type and process:
1. "no_query" - No retrieval needed: greetings, chitchat, emotional expressions, simple responses, etc. that can be answered without retrieving historical information.
2. "clear" - Clear query: the query is complete and unambiguous, requiring relevant information to answer.
3. "ambiguous" - Ambiguous reference: the query contains pronouns (like "it", "that", "this") or references to previous content, requiring context to understand. For this type, you MUST rewrite the user's query into a complete, clear query that can be understood independently without relying on conversation history.
4. "vague" - Too vague: the query is too brief or ambiguous, making it difficult to determine user intent even with context. For this type, explain the reason for vagueness.`

// clarifyPrompt is the LLM prompt template for generating clarification questions.
// It takes three parameters: history, query, and reason.
const clarifyPrompt = `The user's query is too vague and needs clarification.

Conversation history:
%s

User query: %s

Reason for vagueness: %s

Generate a clarification question to help the user clarify their intent. The question should be concise, specific, and provide possible options.

IMPORTANT: The clarification question MUST be in the SAME LANGUAGE as the user's query.
- If the user query is in Chinese, respond in Chinese.
- If the user query is in English, respond in English.

Output only the clarification question, without any additional content.`

// QueryRoutingResult represents the structured output of query routing.
// Defines the expected format when the LLM classifies and processes a user query.
type QueryRoutingResult struct {
	Type           string  `json:"type"`
	RewrittenQuery *string `json:"rewritten_query"`
	Reason         *string `json:"reason"`
}

// PreprocessingResult represents the full output of query preprocessing,
// including the processed query, type classification, and clarification if needed.
type PreprocessingResult struct {
	OriginalQuery      string `json:"original_query"`
	ProcessedQuery     string `json:"processed_query"`
	QueryType          string `json:"query_type"`
	NeedsClarification bool   `json:"needs_clarification"`
	Clarification      string `json:"clarification"`
	SkipRetrieval      bool   `json:"skip_retrieval"`
}

// QueryPreprocessingService handles query classification and transformation.
//
// This service handles the preprocessing of user queries before they are sent to the
// retrieval or LLM systems. It includes:
//   - Query type classification (clear, ambiguous, vague, no_query)
//   - Query rewriting for ambiguous queries with context
//   - Clarification generation for vague queries
//
// The preprocessing pipeline ensures that queries are optimized for retrieval
// and LLM processing.
type QueryPreprocessingService struct{}

// NewQueryPreprocessingService creates a QueryPreprocessingService instance.
func NewQueryPreprocessingService() *QueryPreprocessingService {
	return &QueryPreprocessingService{}
}

// FormatHistoryForPreprocessing formats conversation history for preprocessing prompts.
// Limits to the most recent maxMessages if specified.
func (qps *QueryPreprocessingService) FormatHistoryForPreprocessing(history []map[string]string, maxMessages *int) string {
	if len(history) == 0 {
		return "(No conversation history)"
	}

	recent := history
	if maxMessages != nil && len(history) > *maxMessages {
		recent = history[len(history)-*maxMessages:]
	}

	var formatted []string
	for _, msg := range recent {
		role := "User"
		if msg["role"] != "user" {
			role = "Assistant"
		}
		formatted = append(formatted, fmt.Sprintf("%s: %s", role, msg["content"]))
	}
	return strings.Join(formatted, "\n")
}

// RouteQuery classifies the query type and rewrites if ambiguous.
// Uses JSON Schema structured output for deterministic classification.
// Uses TemperatureDeterministic for consistent, deterministic outputs.
func (qps *QueryPreprocessingService) RouteQuery(
	llmConfig *model.LLMConfig,
	query string,
	history []map[string]string,
	maxMessages *int,
) *QueryRoutingResult {
	chatModel := llm.NewChatModelWithTemperature(llmConfig.BaseURL, llmConfig.APIKey, llmConfig.ModelID, llm.TemperatureDeterministic)

	historyText := qps.FormatHistoryForPreprocessing(history, maxMessages)
	prompt := fmt.Sprintf(routingPrompt, historyText, query)

	result, err := chatModel.ChatWithJSONSchema(stdctx.Background(), []llm.ChatMessage{
		{Role: "user", Content: prompt},
	}, llm.JSONSchemaDefinition{
		Name:        "QueryRoutingResult",
		Description: "Classify and process the user query",
		Strict:      true,
		Schema: jsonschema.Definition{
			Type: jsonschema.Object,
			Properties: map[string]jsonschema.Definition{
				"type": {
					Type:        jsonschema.String,
					Enum:        []string{"no_query", "clear", "ambiguous", "vague"},
					Description: "Query type classification",
				},
				"rewritten_query": {
					Type:        jsonschema.String,
					Description: "Rewritten query that is self-contained and clear (required for ambiguous type)",
				},
				"reason": {
					Type:        jsonschema.String,
					Description: "Reason why the query is vague and needs clarification (required for vague type)",
				},
			},
			Required: []string{"type"},
		},
	})

	if err != nil {
		applogger.L.Error("Query routing failed", "error", err)
		return &QueryRoutingResult{Type: queryTypeClear}
	}

	if result != "" {
		var routing QueryRoutingResult
		if err := json.Unmarshal([]byte(result), &routing); err == nil {
			applogger.L.Info("Query routing result", "type", routing.Type)
			if routing.Type == queryTypeAmbiguous && routing.RewrittenQuery != nil {
				applogger.L.Info("Query rewritten", "original", query[:minLen(50, len(query))], "rewritten", (*routing.RewrittenQuery)[:minLen(50, len(*routing.RewrittenQuery))])
			}
			return &routing
		}
	}

	return &QueryRoutingResult{Type: queryTypeClear}
}

// GenerateClarification generates a clarification question for vague queries.
// If characterSettings is provided, it is prepended to the prompt for personality alignment.
// Uses TemperatureDeterministic for consistent outputs.
func (qps *QueryPreprocessingService) GenerateClarification(
	llmConfig *model.LLMConfig,
	query string,
	history []map[string]string,
	reason string,
	characterSettings *string,
	maxMessages *int,
) string {
	chatModel := llm.NewChatModelWithTemperature(llmConfig.BaseURL, llmConfig.APIKey, llmConfig.ModelID, llm.TemperatureDeterministic)

	historyText := qps.FormatHistoryForPreprocessing(history, maxMessages)
	prompt := fmt.Sprintf(clarifyPrompt, historyText, query, reason)

	// Prepend character settings for personality alignment
	if characterSettings != nil && *characterSettings != "" {
		prompt = fmt.Sprintf("[Your Character]\n%s\n\n%s", *characterSettings, prompt)
	}

	result, err := chatModel.Chat(stdctx.Background(), []llm.ChatMessage{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		applogger.L.Error("Clarification generation failed", "error", err)
		return "Your question is a bit vague. Could you please provide more details about your needs?"
	}

	applogger.L.Info("Generated clarification for query", "query", query[:minLen(50, len(query))])
	return result
}

// PreprocessQuery is the main entry point for query preprocessing.
// It classifies the query type and applies the appropriate transformation:
//   - no_query: skip retrieval, use original query
//   - clear: use original query with retrieval
//   - ambiguous: rewrite query with context for retrieval
//   - vague: generate clarification question, mark as needs_clarification
func (qps *QueryPreprocessingService) PreprocessQuery(
	llmConfig *model.LLMConfig,
	query string,
	history []map[string]string,
	characterSettings *string,
	maxMessages *int,
) *PreprocessingResult {
	result := &PreprocessingResult{
		OriginalQuery:  query,
		ProcessedQuery: query,
		QueryType:      queryTypeClear,
	}

	routing := qps.RouteQuery(llmConfig, query, history, maxMessages)
	queryType := routing.Type
	result.QueryType = queryType

	switch queryType {
	case queryTypeNoQuery:
		result.ProcessedQuery = query
		result.SkipRetrieval = true

	case queryTypeClear:
		result.ProcessedQuery = query
		result.SkipRetrieval = false

	case queryTypeAmbiguous:
		if routing.RewrittenQuery != nil {
			result.ProcessedQuery = *routing.RewrittenQuery
		} else {
			result.ProcessedQuery = query
		}

	case queryTypeVague:
		reason := "Query is too vague"
		if routing.Reason != nil {
			reason = *routing.Reason
		}
		clarification := qps.GenerateClarification(llmConfig, query, history, reason, characterSettings, maxMessages)
		result.NeedsClarification = true
		result.Clarification = clarification
	}

	applogger.L.Info("Query preprocessing complete",
		"type", queryType,
		"processed", result.ProcessedQuery[:minLen(50, len(result.ProcessedQuery))],
	)
	return result
}

func minLen(a, b int) int {
	if a < b {
		return a
	}
	return b
}
