package providers

import (
	"strings"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/core/api"
	"github.com/firebase/genkit/go/genkit"
	"github.com/firebase/genkit/go/plugins/googlegenai"
	"google.golang.org/genai"
)

type geminiProvider struct {
	plugin *googlegenai.GoogleAI
}

func newGemini(cfg Config) (Provider, error) {
	return &geminiProvider{
		plugin: &googlegenai.GoogleAI{APIKey: cfg.APIKey},
	}, nil
}

func (p *geminiProvider) Plugin() api.Plugin { return p.plugin }

func (p *geminiProvider) Model(g *genkit.Genkit, name string) (ai.Model, error) {
	if name == "" {
		name = "gemini-flash-latest"
	}
	model, err := p.plugin.DefineModel(g, name, nil)
	if err != nil {
		model, err = p.plugin.DefineModel(g, name, &ai.ModelOptions{
			Supports: &googlegenai.Multimodal,
			Stage:    ai.ModelStageUnstable,
		})
	}
	return model, err
}

func (p *geminiProvider) Embedder(g *genkit.Genkit, name string) (ai.Embedder, error) {
	if name == "" {
		name = "gemini-embedding-001"
	}
	return p.plugin.DefineEmbedder(g, name, &ai.EmbedderOptions{})
}

// GenerateOptions returns Gemini-specific generate options (thinking config, Google Search).
// Returns nil when neither feature is requested.
// MINIMAL thinking is only valid for Flash models; silently skipped for Pro.
func (p *geminiProvider) GenerateOptions(modelName string, searchEnable bool, thinkingLevel string) []ai.GenerateOption {
	cfg := &genai.GenerateContentConfig{}
	hasConfig := false

	if thinkingLevel != "" {
		isFlash := strings.Contains(strings.ToLower(modelName), "flash")
		var level genai.ThinkingLevel
		switch thinkingLevel {
		case "NONE", "MINIMAL":
			if isFlash {
				level = genai.ThinkingLevelMinimal
			}
		case "LOW":
			level = genai.ThinkingLevelLow
		case "MEDIUM":
			level = genai.ThinkingLevelMedium
		case "HIGH":
			level = genai.ThinkingLevelHigh
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
