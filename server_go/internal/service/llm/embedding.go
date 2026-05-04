package llm

import (
	"context"
	"fmt"

	openai "github.com/sashabaranov/go-openai"
)

type EmbeddingService struct {
	client  *openai.Client
	modelID string
	dim     int
}

func NewEmbeddingService(baseURL, apiKey, modelID string, dim int) *EmbeddingService {
	cfg := openai.DefaultConfig(apiKey)
	if baseURL != "" {
		cfg.BaseURL = baseURL
	}
	return &EmbeddingService{
		client:  openai.NewClientWithConfig(cfg),
		modelID: modelID,
		dim:     dim,
	}
}

func (es *EmbeddingService) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	resp, err := es.client.CreateEmbeddings(ctx, openai.EmbeddingRequestStrings{
		Input: texts,
		Model: openai.EmbeddingModel(es.modelID),
	})
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}

	result := make([][]float32, 0, len(resp.Data))
	for _, d := range resp.Data {
		result = append(result, d.Embedding)
	}

	return result, nil
}

func (es *EmbeddingService) EmbedSingle(ctx context.Context, text string) ([]float32, error) {
	embeddings, err := es.Embed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return embeddings[0], nil
}

func (es *EmbeddingService) Dimension() int {
	return es.dim
}
