// Package providers abstracts AI provider initialization behind a common interface.
// Supported providers: gemini, openai (and any OpenAI-compatible endpoint via base_url).
package providers

import (
	"fmt"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/core/api"
	"github.com/firebase/genkit/go/genkit"
)

// Provider is the common interface that every AI backend must satisfy.
type Provider interface {
	// Plugin returns the genkit plugin to register with genkit.Init.
	Plugin() api.Plugin
	// Model looks up or defines the named model on the given genkit instance.
	Model(g *genkit.Genkit, name string) (ai.Model, error)
	// Embedder looks up or defines the named embedder on the given genkit instance.
	Embedder(g *genkit.Genkit, name string) (ai.Embedder, error)
	// GenerateOptions returns provider-specific generation options (e.g. Google Search, thinking).
	// Returns nil when the provider has no extra options for the given parameters.
	GenerateOptions(modelName string, searchEnable bool, thinkingLevel string) []ai.GenerateOption
}

// Config holds credentials and endpoint for a single provider.
type Config struct {
	APIKey  string
	BaseURL string // optional; empty means provider default
}

// New returns the Provider implementation for the given name.
// name must be "gemini" or "openai".
func New(name string, cfg Config) (Provider, error) {
	switch name {
	case "gemini", "":
		return newGemini(cfg)
	case "openai":
		return newOpenAI(cfg)
	default:
		return nil, fmt.Errorf("unknown provider %q; supported: gemini, openai", name)
	}
}
