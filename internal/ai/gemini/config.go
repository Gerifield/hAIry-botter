package gemini

import (
	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
	"github.com/firebase/genkit/go/plugins/googlegenai"
	"google.golang.org/genai"
)

// ConfigModel .
func ConfigModel(g *genkit.Genkit, ga *googlegenai.GoogleAI, modelName string) (ai.Model, error) {
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
		geminiSpecConfig.Tools = []*genai.Tool{
			{GoogleSearch: &genai.GoogleSearch{}},
		}
	}

	return geminiSpecConfig
}
