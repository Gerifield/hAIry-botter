package providers

import (
	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/core/api"
	"github.com/firebase/genkit/go/genkit"
	"github.com/firebase/genkit/go/plugins/compat_oai/openai"
	openaiopt "github.com/openai/openai-go/option"
)

type openaiProvider struct {
	plugin *openai.OpenAI
}

func newOpenAI(cfg Config) (Provider, error) {
	var extraOpts []openaiopt.RequestOption
	if cfg.BaseURL != "" {
		extraOpts = append(extraOpts, openaiopt.WithBaseURL(cfg.BaseURL))
	}
	return &openaiProvider{
		plugin: &openai.OpenAI{
			APIKey: cfg.APIKey,
			Opts:   extraOpts,
		},
	}, nil
}

func (p *openaiProvider) Plugin() api.Plugin { return p.plugin }

func (p *openaiProvider) Model(g *genkit.Genkit, name string) (ai.Model, error) {
	if name == "" {
		name = "gpt-4o"
	}
	model := p.plugin.Model(g, name)
	if model != nil {
		return model, nil
	}
	// Unknown model name — define it with generic multimodal capabilities.
	return p.plugin.DefineModel(name, ai.ModelOptions{
		Supports: &ai.ModelSupports{
			Multiturn:  true,
			Tools:      true,
			SystemRole: true,
			Media:      true,
		},
		Stage: ai.ModelStageUnstable,
	}), nil
}

func (p *openaiProvider) Embedder(g *genkit.Genkit, name string) (ai.Embedder, error) {
	if name == "" {
		name = "text-embedding-3-small"
	}
	embedder := p.plugin.Embedder(g, name)
	if embedder != nil {
		return embedder, nil
	}
	// Unknown embedder name — define it with generic options.
	return p.plugin.DefineEmbedder(name, &ai.EmbedderOptions{}), nil
}

// GenerateOptions returns nil — OpenAI has no Gemini-specific extras.
func (p *openaiProvider) GenerateOptions(_ string, _ bool, _ string) []ai.GenerateOption {
	return nil
}
