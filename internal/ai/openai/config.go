package openai

import (
	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/core/api"
	"github.com/firebase/genkit/go/plugins/compat_oai"
	"github.com/openai/openai-go/option"
)

// AgentConfigurator .
type AgentConfigurator interface {
	api.Plugin
	modelDefiner
}

type modelDefiner interface {
	DefineModel(provider string, name string, opts ai.ModelOptions) ai.Model
}

// ConfigPlugin .
func ConfigPlugin(apiKey string, baseURL string) AgentConfigurator {
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
	}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}

	return &compat_oai.OpenAICompatible{
		Provider: "openai",
		Opts:     opts,
	}
}

// ConfigModel .
func ConfigModel(ga modelDefiner, modelName string) ai.Model {
	if modelName == "" {
		modelName = "gpt-4o-mini" // Default to gpt-4o-mini
	}

	return ga.DefineModel("openai", modelName, ai.ModelOptions{
		Label:    "OpenAI Model",
		Versions: []string{},
		Stage:    ai.ModelStageUnstable,
	})
}
