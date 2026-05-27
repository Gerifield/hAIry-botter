package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"hairy-botter/internal/ai/adapters"
	"hairy-botter/internal/ai/agent"
	"hairy-botter/internal/ai/providers"
	"hairy-botter/internal/config"
	"hairy-botter/internal/history"
	"hairy-botter/internal/mcpserver"
	"hairy-botter/internal/rag"
	"hairy-botter/internal/server"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/core/api"
	"github.com/firebase/genkit/go/genkit"
)

// firstLine returns the first non-empty line of s, trimmed of whitespace.
func firstLine(s string) string {
	for _, line := range strings.SplitN(s, "\n", -1) {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return s
}

func parseLogLevel(levelStr string) slog.Level {
	switch strings.ToLower(levelStr) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Configure logging based on run mode
	logOut := os.Stdout
	if cfg.RunMode == "mcp_cli" {
		logOut = os.Stderr
	}

	logger := slog.New(slog.NewJSONHandler(logOut, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.LogLevel),
	}))
	slog.SetDefault(logger)

	if cfg.RunMode != "agent" && cfg.RunMode != "mcp_cli" {
		logger.Error("invalid run_mode in config", slog.String("run_mode", cfg.RunMode))
		return
	}

	// Build the main AI provider.
	mainProvider, err := providers.New(cfg.Provider, providerCfgFor(cfg.Provider, cfg))
	if err != nil {
		logger.Error("failed to create AI provider", slog.String("provider", cfg.Provider), slog.String("err", err.Error()))
		return
	}

	// Build the embedder provider (may differ from the main provider).
	embedderProviderName := cfg.Capabilities.Rag.EmbedderProvider
	embedderCfg := providerCfgFor(embedderProviderName, cfg)
	if cfg.Capabilities.Rag.EmbedderBaseURL != "" {
		embedderCfg.BaseURL = cfg.Capabilities.Rag.EmbedderBaseURL
	}
	embedderProvider, err := providers.New(embedderProviderName, embedderCfg)
	if err != nil {
		logger.Error("failed to create embedder provider", slog.String("provider", embedderProviderName), slog.String("err", err.Error()))
		return
	}

	// Collect distinct plugins for genkit.Init.
	g := genkit.Init(context.Background(), genkit.WithPlugins(distinctPlugins(mainProvider.Plugin(), embedderProvider.Plugin())...))

	model, err := mainProvider.Model(g, cfg.Model)
	if err != nil {
		logger.Error("failed to define model", slog.String("err", err.Error()))
		return
	}

	searchEnable := !cfg.GeminiSearchDisabled
	customModelConfig := mainProvider.GenerateOptions(cfg.Model, searchEnable, cfg.GeminiThinkingLevel)

	historySummary := 0
	if cfg.Capabilities.HistorySummary.Enabled {
		historySummary = 20
		if cfg.Capabilities.HistorySummary.MessageCount > 0 {
			historySummary = cfg.Capabilities.HistorySummary.MessageCount
		}
	}

	var mcpServers []agent.MCPServer
	for _, srv := range cfg.Capabilities.MCPServers {
		if srv.Path != "" {
			mcpServers = append(mcpServers, agent.MCPServer{
				Type: srv.Type,
				Path: srv.Path,
				Args: srv.Args,
				Env:  srv.Env,
			})
		}
	}
	logger.Info("MCP servers from config", slog.Int("count", len(mcpServers)))

	var ragL *rag.Logic
	if cfg.Capabilities.Rag.Enabled && cfg.Capabilities.Rag.Directory != "" {
		embedder, err := embedderProvider.Embedder(g, cfg.Capabilities.Rag.EmbeddingModel)
		if err != nil {
			logger.Error("failed to define embedder", slog.String("err", err.Error()))
			return
		}

		ragL, err = rag.New(logger, cfg.Capabilities.Rag.Directory, adapters.NewEmbedder(g, embedder))
		if err != nil {
			logger.Error("failed to create RAG logic", slog.String("err", err.Error()))
			return
		}
	}

	hist := history.New(logger, "history-gemini/", history.Config{
		HistorySummary: historySummary,
		Summarizer:     adapters.NewSummarizer(g, model),
	})

	if cfg.Capabilities.MaxTurns > 0 {
		customModelConfig = append(customModelConfig, ai.WithMaxTurns(cfg.Capabilities.MaxTurns))
	}

	systemPrompt := cfg.Personality.Role + "\n" + cfg.Personality.SystemPrompt
	aiLogic, err := agent.New(logger, g, model, hist, mcpServers, ragL, systemPrompt, customModelConfig, cfg.Context)
	if err != nil {
		logger.Error("failed to create AI logic", slog.String("err", err.Error()))
		return
	}

	var mcpSrv *mcpserver.Server
	if cfg.RunMode == "mcp_cli" {
		logger.Info("running in mcp_cli mode, awaiting MCP JSON-RPC over stdio")

		agentName := cfg.AgentConfig.AgentName
		if agentName == "" {
			agentName = "hairy-botter-agent"
		}
		agentDesc := cfg.AgentConfig.AgentDescription
		if agentDesc == "" {
			agentDesc = firstLine(aiLogic.SystemPrompt())
		}

		mcpSrv = mcpserver.New(logger, aiLogic, mcpserver.Config{
			Name:        agentName,
			Description: agentDesc,
			ToolNames:   aiLogic.ToolNames(),
		})

		if err := mcpSrv.StartStdio(); err != nil {
			logger.Error("MCP stdio server failed", slog.String("err", err.Error()))
			os.Exit(1)
		}
		return
	}

	// We are in "agent" run mode
	if cfg.AgentConfig.EnableMCPHTTP {
		agentName := cfg.AgentConfig.AgentName
		if agentName == "" {
			agentName = "hairy-botter-agent"
		}
		agentDesc := cfg.AgentConfig.AgentDescription
		if agentDesc == "" {
			agentDesc = firstLine(aiLogic.SystemPrompt())
		}

		mcpSrv = mcpserver.New(logger, aiLogic, mcpserver.Config{
			Name:        agentName,
			Description: agentDesc,
			ToolNames:   aiLogic.ToolNames(),
		})
		go func() {
			mcpAddr := cfg.AgentConfig.MCPPort
			if mcpAddr == "" {
				mcpAddr = ":8081"
			}
			logger.Info("starting MCP sub-agent server", slog.String("addr", mcpAddr))
			if err := mcpSrv.Start(mcpAddr); err != nil {
				logger.Error("MCP sub-agent server failed", slog.String("err", err.Error()))
			}
		}()
	}

	var srv *server.Server
	if cfg.AgentConfig.EnableChatProxy {
		corsOrigin := cfg.AgentConfig.CORSAllowedOrigin
		if corsOrigin == "" {
			corsOrigin = "*"
		}
		corsMethods := cfg.AgentConfig.CORSAllowedMethods
		if corsMethods == "" {
			corsMethods = "POST, OPTIONS"
		}
		corsHeaders := cfg.AgentConfig.CORSAllowedHeaders
		if corsHeaders == "" {
			corsHeaders = "Content-Type, X-User-ID"
		}

		addr := cfg.AgentConfig.HTTPPort
		if addr == "" {
			addr = ":8080"
		}

		srv = server.New(addr, aiLogic, server.Config{
			AllowedOrigin:  corsOrigin,
			AllowedMethods: corsMethods,
			AllowedHeaders: corsHeaders,
		})
	}

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, os.Interrupt, syscall.SIGTERM)
	finishedCh := make(chan struct{})
	go func() {
		<-stopCh
		logger.Info("shutting down server")
		if srv != nil {
			err := srv.Stop(context.Background())
			if err != nil {
				logger.Error("failed to stop server", slog.String("err", err.Error()))
			}
		}

		if mcpSrv != nil {
			err := mcpSrv.Stop(context.Background())
			if err != nil {
				logger.Error("failed to stop MCP server", slog.String("err", err.Error()))
			}
		}

		if ragL != nil {
			logger.Info("flushing RAG database")
			err = ragL.Close()
			if err != nil {
				logger.Error("failed to persist the database", slog.String("err", err.Error()))
			}
		}
		close(finishedCh)
	}()

	if srv != nil {
		logger.Info("starting server", slog.String("addr", cfg.AgentConfig.HTTPPort))
		if err := srv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed", slog.String("err", err.Error()))
		}
	} else {
		logger.Info("agent running without chat proxy")
	}
	<-finishedCh
}

// providerCfgFor returns the providers.Config for the named provider from cfg.
func providerCfgFor(name string, cfg *config.Config) providers.Config {
	switch name {
	case "openai":
		return providers.Config{
			APIKey:  cfg.Providers.OpenAI.APIKey,
			BaseURL: cfg.Providers.OpenAI.BaseURL,
		}
	default: // gemini
		return providers.Config{
			APIKey:  cfg.Providers.Gemini.APIKey,
			BaseURL: cfg.Providers.Gemini.BaseURL,
		}
	}
}

// distinctPlugins returns a deduplicated slice of api.Plugin by name.
func distinctPlugins(ps ...api.Plugin) []api.Plugin {
	seen := make(map[string]bool, len(ps))
	out := make([]api.Plugin, 0, len(ps))
	for _, p := range ps {
		if !seen[p.Name()] {
			seen[p.Name()] = true
			out = append(out, p)
		}
	}
	return out
}
