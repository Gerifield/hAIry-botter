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
	Model                string             `yaml:"model"`
	GeminiSearchDisabled bool               `yaml:"gemini_search_disabled"`
	LogLevel             string             `yaml:"log_level"`
	Personality          PersonalityConfig  `yaml:"personality"`
	Capabilities         CapabilitiesConfig `yaml:"capabilities"`
	Context              ContextConfig      `yaml:"context"`
	APIKeys              APIKeysConfig      `yaml:"api_keys"` // Keep this distinct for overriding
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
	Enabled   bool   `yaml:"enabled"`
	Directory string `yaml:"directory"`
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
	AutoInject []string `yaml:"auto_inject"`
}

// APIKeysConfig holds API keys to prioritize yaml over env
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

	// Apply environment variable fallbacks for API keys
	if cfg.APIKeys.Gemini == "" {
		cfg.APIKeys.Gemini = os.Getenv("GEMINI_API_KEY")
	}

	// Set defaults if some values are absent
	if cfg.Model == "" {
		cfg.Model = "gemini-pro-latest"
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "warning"
	}

	return &cfg, nil
}
