package adapters

import (
	"context"
	"errors"

	"hairy-botter/internal/history"
	"hairy-botter/internal/rag"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
)

// NewEmbedder returns a rag.EmbeddingFunc backed by the given genkit embedder.
func NewEmbedder(g *genkit.Genkit, embedder ai.Embedder) rag.EmbeddingFunc {
	return func(ctx context.Context, text string) ([]float32, error) {
		resp, err := genkit.Embed(ctx, g, ai.WithEmbedder(embedder), ai.WithTextDocs(text))
		if err != nil {
			return nil, err
		}
		if len(resp.Embeddings) == 0 {
			return nil, errors.New("no embeddings returned")
		}
		return resp.Embeddings[0].Embedding, nil
	}
}

// NewSummarizer returns a history.Summarizer backed by the given genkit model.
func NewSummarizer(g *genkit.Genkit, model ai.Model) history.Summarizer {
	return &summarizer{g: g, model: model}
}

type summarizer struct {
	g     *genkit.Genkit
	model ai.Model
}

func (s *summarizer) Summarize(ctx context.Context, systemPrompt, text string) (string, error) {
	resp, err := genkit.Generate(ctx, s.g,
		ai.WithModel(s.model),
		ai.WithSystem(systemPrompt),
		ai.WithMessages(ai.NewUserTextMessage(text)),
	)
	if err != nil {
		return "", err
	}
	return resp.Text(), nil
}
