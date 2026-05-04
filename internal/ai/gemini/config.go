package gemini

import (
	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/core/api"
	"github.com/firebase/genkit/go/genkit"
	"github.com/firebase/genkit/go/plugins/googlegenai"
	"google.golang.org/genai"
)

// AgentConfigurator .
type AgentConfigurator interface {
	api.Plugin
	modelDefiner
	modelEmbedder
}
type modelDefiner interface {
	DefineModel(g *genkit.Genkit, name string, opts *ai.ModelOptions) (ai.Model, error)
}

type modelEmbedder interface {
	DefineEmbedder(g *genkit.Genkit, name string, embedOpts *ai.EmbedderOptions) (ai.Embedder, error)
}

func ConfigPlugin(apiKey string) AgentConfigurator {
	return &googlegenai.GoogleAI{APIKey: apiKey}
}

// ConfigModel .
func ConfigModel(g *genkit.Genkit, ga modelDefiner, modelName string) (ai.Model, error) {
	if modelName == "" {
		modelName = "gemini-flash-latest"
	}

	// Try the known-model path first (nil opts = look up from plugin's registry).
	// -latest aliases and next-gen model names are not in the registry, so fall back
	// to generic multimodal options so the caller still gets a usable model.
	model, err := ga.DefineModel(g, modelName, nil)
	if err != nil {
		model, err = ga.DefineModel(g, modelName, &ai.ModelOptions{
			Supports: &googlegenai.Multimodal,
			Stage:    ai.ModelStageUnstable,
		})
	}

	return model, err
}

// ConfigEmbedder .
func ConfigEmbedder(g *genkit.Genkit, ga modelEmbedder, modelName string) (ai.Embedder, error) {
	if modelName == "" {
		modelName = "gemini-embedding-001"
	}

	embedder, err := ga.DefineEmbedder(g, modelName, &ai.EmbedderOptions{})
	if err != nil {
		return nil, err
	}

	return embedder, nil
}

// GenerateOptions returns Gemini-specific generate options (thinking config, Google Search).
// Returns nil when neither feature is requested so no provider-specific config is sent
// to models that don't support it (e.g. older flash models without thinking support).
func GenerateOptions(searchEnable bool, thinkingLevel string) []ai.GenerateOption {
	cfg := &genai.GenerateContentConfig{}
	hasConfig := false

	if thinkingLevel != "" {
		var level genai.ThinkingLevel
		switch thinkingLevel {
		case "LOW":
			level = genai.ThinkingLevelLow
		case "MEDIUM":
			level = genai.ThinkingLevelMedium
		case "HIGH":
			level = genai.ThinkingLevelHigh
		case "MINIMAL":
			level = genai.ThinkingLevelMinimal
		}
		if level != "" {
			cfg.ThinkingConfig = &genai.ThinkingConfig{ThinkingLevel: level}
			hasConfig = true
		}
	}

	if searchEnable {
		ist := true
		cfg.Tools = []*genai.Tool{
			{GoogleSearch: &genai.GoogleSearch{}},
		}
		cfg.ToolConfig = &genai.ToolConfig{
			IncludeServerSideToolInvocations: &ist,
		}
		hasConfig = true
	}

	if !hasConfig {
		return nil
	}
	return []ai.GenerateOption{ai.WithConfig(cfg)}
}
