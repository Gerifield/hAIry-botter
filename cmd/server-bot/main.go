package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"hairy-botter/internal/ai/agent"
	"hairy-botter/internal/ai/gemini"
	genkit_embedding "hairy-botter/internal/ai/genkit-embedding"
	genkit_summarizer "hairy-botter/internal/ai/genkit-summarizer"
	"hairy-botter/internal/history"
	"hairy-botter/internal/rag"
	"hairy-botter/internal/server"

	"github.com/firebase/genkit/go/genkit"
)

func logLevelEnv() slog.Level {
	levelStr := os.Getenv("LOG_LEVEL")
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

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevelEnv(),
	}))

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}

	geminiKey := os.Getenv("GEMINI_API_KEY")
	if geminiKey == "" {
		logger.Error("GEMINI_API_KEY is not set")

		return
	}

	historySummaryEnv := os.Getenv("HISTORY_SUMMARY")
	historySummary := 20 // Default to 20
	if historySummaryEnv != "" {
		p, err := strconv.ParseInt(historySummaryEnv, 10, 32)
		if err != nil {
			logger.Error("failed to parse HISTORY_SUMMARY", slog.String("err", err.Error()))

			return
		}
		historySummary = int(p)
	}

	mcpServer := os.Getenv("MCP_SERVERS")
	mcpClientAddrs := make([]string, 0)
	if mcpServer != "" {
		// Parse the MCP server list
		servers := strings.Split(mcpServer, ",")
		for _, s := range servers {
			s = strings.TrimSpace(s)
			mcpClientAddrs = append(mcpClientAddrs, s)
		}
	}

	searchEnable := true
	searchDisabled := os.Getenv("GEMINI_SEARCH_DISABLED")
	if searchDisabled == "true" || searchDisabled == "1" {
		searchEnable = false
		logger.Info("Gemini search plugin is disabled")
	}

	aiProvider := os.Getenv("AI_PROVIDER")
	if aiProvider == "" {
		aiProvider = "gemini"
	}

	plugins := make([]api.Plugin, 0)

	// Initialize the Gemini AI logic (Always needed for embedding and summarization)
	ga := gemini.ConfigPlugin(geminiKey)
	plugins = append(plugins, ga)

	var oai openai.AgentConfigurator
	if aiProvider == "openai" {
		oaiKey := os.Getenv("OPENAI_API_KEY")
		if oaiKey == "" {
			logger.Error("OPENAI_API_KEY is not set but AI_PROVIDER is openai")
			return
		}
		oaiBaseURL := os.Getenv("OPENAI_BASE_URL")
		oai = openai.ConfigPlugin(oaiKey, oaiBaseURL)
		plugins = append(plugins, oai)
	}

	g := genkit.Init(context.Background(), genkit.WithPlugins(plugins...))

	geminiModel, err := gemini.ConfigModel(g, ga, os.Getenv("GEMINI_MODEL"))
	if err != nil {
		logger.Error("failed to define Gemini model", slog.String("err", err.Error()))

		return
	}

	var activeModel ai.Model
	var customModelConfig any

	if aiProvider == "openai" {
		activeModel = openai.ConfigModel(oai, os.Getenv("OPENAI_MODEL"))
		customModelConfig = nil // No custom config for OpenAI for now
	} else {
		activeModel = geminiModel
		customModelConfig = gemini.CustomConfig(searchEnable)
	}

	embedder, err := gemini.ConfigEmbedder(g, ga, "gemini-embedding-001")
	if err != nil {
		logger.Error("failed to define embedder", slog.String("err", err.Error()))

		return
	}

	ragL, err := rag.New(logger, "bot-context/", rag.EmbeddingFunc(genkit_embedding.New(g, embedder)))
	if err != nil {
		logger.Error("failed to create RAG logic", slog.String("err", err.Error()))

		return
	}

	hist := history.New(logger, "history-gemini/", history.Config{
		HistorySummary: historySummary,
		Summarizer:     genkit_summarizer.New(g, model),
	})

	aiLogic, err := agent.New(logger, g, activeModel, hist, mcpClientAddrs, ragL, customModelConfig)
	if err != nil {
		logger.Error("failed to create AI logic", slog.String("err", err.Error()))

		return
	}

	corsOrigin := os.Getenv("CORS_ALLOWED_ORIGIN")
	if corsOrigin == "" {
		corsOrigin = "*"
	}
	corsMethods := os.Getenv("CORS_ALLOWED_METHODS")
	if corsMethods == "" {
		corsMethods = "POST, OPTIONS"
	}
	corsHeaders := os.Getenv("CORS_ALLOWED_HEADERS")
	if corsHeaders == "" {
		corsHeaders = "Content-Type, X-User-ID"
	}

	srv := server.New(addr, aiLogic, server.Config{
		AllowedOrigin:  corsOrigin,
		AllowedMethods: corsMethods,
		AllowedHeaders: corsHeaders,
	})

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, os.Interrupt, syscall.SIGTERM)
	finishedCh := make(chan struct{}) // Signal the end of the graceful shutdown
	go func() {
		<-stopCh
		logger.Info("shutting down server")
		err := srv.Stop(context.Background())
		if err != nil {
			logger.Error("failed to stop server", slog.String("err", err.Error()))
		}

		logger.Info("flushing RAG database")
		err = ragL.Close()
		if err != nil {
			logger.Error("failed to persist the database", slog.String("err", err.Error()))
		}
		close(finishedCh)
	}()

	logger.Info("starting server", slog.String("addr", addr))
	if err := srv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server failed", slog.String("err", err.Error()))
	}
	<-finishedCh
}
