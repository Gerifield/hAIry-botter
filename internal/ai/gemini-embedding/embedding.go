package gemini_embedding

import (
	"context"
	"errors"

	"github.com/philippgille/chromem-go"
	"google.golang.org/genai"
)

// GeminiEmbeddingFunc .
func GeminiEmbeddingFunc(aiClient *genai.Client, embeddingModel string) chromem.EmbeddingFunc {
	return func(ctx context.Context, text string) ([]float32, error) {
		contents := []*genai.Content{
			genai.NewContentFromText(text, genai.RoleUser),
		}
		res, err := aiClient.Models.EmbedContent(ctx, embeddingModel, contents, &genai.EmbedContentConfig{
			TaskType: "RETRIEVAL_QUERY",
		})
		if err != nil {
			return nil, err
		}
		if len(res.Embeddings) == 0 {
			return nil, errors.New("no embeddings returned")
		}

		return res.Embeddings[0].Values, nil
	}
}
