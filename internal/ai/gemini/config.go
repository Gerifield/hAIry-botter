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
	geminiModelOptions := (*ai.ModelOptions)(nil)
	if modelName == "" {
		modelName = "gemini-flash-latest" // Always use the latest flash model by default
		geminiModelOptions = &ai.ModelOptions{
			Label:    "Gemini Flash Latest",
			Versions: []string{},
			Supports: &googlegenai.Multimodal,
			Stage:    ai.ModelStageUnstable,
		}
	}

	model, err := ga.DefineModel(g, modelName, geminiModelOptions)
	if err != nil {
		return nil, err
	}

	return model, nil
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

// CustomConfig .
func CustomConfig(searchEnable bool) any {
	geminiSpecConfig := &genai.GenerateContentConfig{
		ThinkingConfig: &genai.ThinkingConfig{
			// ThinkingBudget: genai.Ptr[int32](0),
			ThinkingLevel: genai.ThinkingLevelMinimal, // This is for the Gemini 3, Pro doesn't support it, just flash: https://ai.google.dev/gemini-api/docs/thinking#thinking-levels
		},
	}
	// If the search is enabled, add this as a custom config, it is GEMINI ONLY!
	if searchEnable {
		ist := true
		geminiSpecConfig.Tools = []*genai.Tool{
			{GoogleSearch: &genai.GoogleSearch{}},
		}
		geminiSpecConfig.ToolConfig = &genai.ToolConfig{
			IncludeServerSideToolInvocations: &ist,
		}
	}

	return geminiSpecConfig
}
