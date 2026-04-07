package genkit_summarizer

import (
	"context"

	"hairy-botter/internal/history"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
)

type summarizer struct {
	g     *genkit.Genkit
	model ai.Model
}

// New returns a history.Summarizer backed by the given genkit model.
func New(g *genkit.Genkit, model ai.Model) history.Summarizer {
	return &summarizer{g: g, model: model}
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
