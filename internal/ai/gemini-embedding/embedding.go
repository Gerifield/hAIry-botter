package gemini_embedding

import (
	"context"
	"errors"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
	"github.com/philippgille/chromem-go"
)

// EmbeddingFunc .
func EmbeddingFunc(g *genkit.Genkit, embedder ai.Embedder) chromem.EmbeddingFunc {
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
