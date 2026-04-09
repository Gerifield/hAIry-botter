package genkit_embedding

import (
	"context"
	"errors"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
)

// Func is a function that embeds text into a float32 vector.
// It matches the chromem.EmbeddingFunc signature so it can be used directly with chromem.
type Func func(ctx context.Context, text string) ([]float32, error)

// New returns an embedding Func backed by the given genkit embedder.
func New(g *genkit.Genkit, embedder ai.Embedder) Func {
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
