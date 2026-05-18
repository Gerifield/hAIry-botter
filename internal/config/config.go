package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config represents the root of the configuration structure.
type Config struct {
	RunMode              string             `yaml:"run_mode"`
	AgentConfig          AgentConfig        `yaml:"agent_config"`
	Provider             string             `yaml:"provider"`   // gemini (default) | openai
	Model                string             `yaml:"model"`
	GeminiSearchDisabled bool               `yaml:"gemini_search_disabled"`
	GeminiThinkingLevel  string             `yaml:"gemini_thinking_level"`
	LogLevel             string             `yaml:"log_level"`
	Personality          PersonalityConfig  `yaml:"personality"`
	Capabilities         CapabilitiesConfig `yaml:"capabilities"`
	Context              ContextConfig      `yaml:"context"`
	APIKeys              APIKeysConfig      `yaml:"api_keys"` // legacy; merged into Providers on load
	Providers            ProvidersConfig    `yaml:"providers"`
}

// AgentConfig holds settings specific to the 'agent' run mode.
type AgentConfig struct {
	EnableChatProxy    bool   `yaml:"enable_chat_proxy"`
	CORSAllowedOrigin  string `yaml:"cors_allowed_origin"`
	CORSAllowedMethods string `yaml:"cors_allowed_methods"`
	CORSAllowedHeaders string `yaml:"cors_allowed_headers"`
	HTTPPort           string `yaml:"http_port"`
	EnableMCPHTTP      bool   `yaml:"enable_mcp_http"`
	MCPPort            string `yaml:"mcp_port"`
	AgentName          string `yaml:"agent_name"`
	AgentDescription   string `yaml:"agent_description"`
}

// PersonalityConfig defines the behavior and persona of the agent.
type PersonalityConfig struct {
	Role         string `yaml:"role"`
	SystemPrompt string `yaml:"system_prompt"`
}

// CapabilitiesConfig toggles feature availability.
type CapabilitiesConfig struct {
	Rag            RagConfig            `yaml:"rag"`
	HistorySummary HistorySummaryConfig `yaml:"history_summary"`
	MCPServers     []MCPServerConfig    `yaml:"mcp_servers"`
}

// RagConfig specifies Retrieval-Augmented Generation settings.
type RagConfig struct {
	Enabled          bool   `yaml:"enabled"`
	Directory        string `yaml:"directory"`
	EmbedderProvider string `yaml:"embedder_provider"` // gemini | openai; defaults to top-level provider
	EmbedderBaseURL  string `yaml:"embedder_base_url"` // overrides provider base_url for the embedder
	EmbeddingModel   string `yaml:"embedding_model"`
}

// HistorySummaryConfig controls conversation history summarization.
type HistorySummaryConfig struct {
	Enabled      bool `yaml:"enabled"`
	MessageCount int  `yaml:"message_count"`
}

// MCPServerConfig defines an external Tool Provider.
type MCPServerConfig struct {
	Type string            `yaml:"type"`
	Path string            `yaml:"path"`
	Args []string          `yaml:"args"`
	Env  map[string]string `yaml:"env"`
}

// ContextConfig specifies files to inject automatically.
type ContextConfig struct {
	StaticInject []string            `yaml:"static_inject"`
	DynamicData  []DynamicDataConfig `yaml:"dynamic_data"`
}

// DynamicDataConfig .
type DynamicDataConfig struct {
	Name    string   `yaml:"name"`
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
}

// ProvidersConfig holds per-provider credentials and endpoints.
type ProvidersConfig struct {
	Gemini ProviderConfig `yaml:"gemini"`
	OpenAI ProviderConfig `yaml:"openai"`
}

// ProviderConfig holds the api key and optional base URL for a single provider.
type ProviderConfig struct {
	APIKey  string `yaml:"api_key"`
	BaseURL string `yaml:"base_url"`
}

// APIKeysConfig holds API keys to prioritize yaml over env (legacy).
type APIKeysConfig struct {
	Gemini string `yaml:"gemini"`
}

// Load reads and parses the configuration file.
// It also overrides specific fields like API keys with environment variables if they are not set in the file.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Migrate legacy api_keys.gemini into providers.gemini.api_key
	if cfg.APIKeys.Gemini != "" && cfg.Providers.Gemini.APIKey == "" {
		cfg.Providers.Gemini.APIKey = cfg.APIKeys.Gemini
	}

	// Apply environment variable fallbacks
	if cfg.Providers.Gemini.APIKey == "" {
		cfg.Providers.Gemini.APIKey = os.Getenv("GEMINI_API_KEY")
	}
	if cfg.Providers.OpenAI.APIKey == "" {
		cfg.Providers.OpenAI.APIKey = os.Getenv("OPENAI_API_KEY")
	}

	// Set defaults
	if cfg.Provider == "" {
		cfg.Provider = "gemini"
	}

	if cfg.Model == "" {
		if cfg.Provider == "openai" {
			cfg.Model = "gpt-4o"
		} else {
			cfg.Model = "gemini-flash-latest"
		}
	}

	if cfg.LogLevel == "" {
		cfg.LogLevel = "warning"
	}

	if cfg.Capabilities.Rag.EmbedderProvider == "" {
		cfg.Capabilities.Rag.EmbedderProvider = cfg.Provider
	}

	if cfg.Capabilities.Rag.EmbeddingModel == "" {
		if cfg.Capabilities.Rag.EmbedderProvider == "openai" {
			cfg.Capabilities.Rag.EmbeddingModel = "text-embedding-3-small"
		} else {
			cfg.Capabilities.Rag.EmbeddingModel = "gemini-embedding-001"
		}
	}

	return &cfg, nil
}
