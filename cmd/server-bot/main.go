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

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
	"github.com/firebase/genkit/go/plugins/googlegenai"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"

	"hairy-botter/internal/ai/gemini"
	gemini_embedding "hairy-botter/internal/ai/gemini-embedding"
	"hairy-botter/internal/history"
	"hairy-botter/internal/rag"
	"hairy-botter/internal/server"
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

// genkitSummarizer implements history.Summarizer using the genkit framework.
type genkitSummarizer struct {
	g     *genkit.Genkit
	model ai.Model
}

func (s *genkitSummarizer) Summarize(ctx context.Context, systemPrompt, text string) (string, error) {
	resp, err := genkit.Generate(ctx, s.g,
		ai.WithModel(s.model),
		ai.WithSystem(systemPrompt),
		ai.WithMessages(ai.NewUserTextMessage(text)),
	)
	if err != nil {
		return "", err
	}
	return resp.Text(), nil
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

	geminiModel := os.Getenv("GEMINI_MODEL")
	if geminiModel == "" {
		geminiModel = "gemini-2.5-flash" // Default to the latest stable flash model
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

	mcpClients := make([]*client.Client, 0)
	mcpServer := os.Getenv("MCP_SERVERS")
	if mcpServer != "" {
		// Parse the MCP server list
		servers := strings.Split(mcpServer, ",")
		for _, s := range servers {
			s = strings.TrimSpace(s)

			logger.Info("init Streamable HTTP MCP server", slog.String("server", mcpServer))
			streamableTransport, err := transport.NewStreamableHTTP(mcpServer, transport.WithHTTPHeaderFunc(func(ctx context.Context) map[string]string {
				res := make(map[string]string)
				if u := ctx.Value("x-session-id"); u != nil {
					res["x-session-id"] = u.(string)
				}

				return res
			}))
			if err != nil {
				logger.Error("failed to create Streamable HTTP transport", slog.String("err", err.Error()))

				return
			}

			if err = streamableTransport.Start(context.Background()); err != nil {
				logger.Error("failed to start Streamable HTTP transport", slog.String("err", err.Error()))

				return
			}

			mcpClients = append(mcpClients, client.NewClient(streamableTransport))
		}
	}

	var searchEnable bool
	searchEnabled := os.Getenv("SEARCH_ENABLE")
	if searchEnabled == "true" || searchEnabled == "1" {
		if len(mcpClients) != 0 {
			logger.Error("MCP clients are not supported with search enabled, please remove MCP_SERVERS environment variable")

			return
		}
		searchEnable = true
	}

	// Initialize the genkit framework with the Google AI (Gemini) plugin.
	// To use a different provider, swap this plugin (e.g. googlegenai.VertexAI{}, ollama, anthropic, etc.)
	ga := &googlegenai.GoogleAI{APIKey: geminiKey}
	g := genkit.Init(context.Background(), genkit.WithPlugins(ga))

	model, err := ga.DefineModel(g, geminiModel, nil)
	if err != nil {
		// Model name not in the pre-known list; define with generic multimodal options
		logger.Warn("model not in known list, defining with generic multimodal options",
			slog.String("model", geminiModel), slog.String("err", err.Error()))
		model, err = ga.DefineModel(g, geminiModel, &ai.ModelOptions{
			Supports: &googlegenai.Multimodal,
		})
		if err != nil {
			logger.Error("failed to define model", slog.String("err", err.Error()))
			return
		}
	}

	embedder, err := ga.DefineEmbedder(g, "gemini-embedding-001", &ai.EmbedderOptions{})
	if err != nil {
		logger.Error("failed to define embedder", slog.String("err", err.Error()))
		return
	}

	ragEmbedder := gemini_embedding.EmbeddingFunc(g, embedder)
	ragL, err := rag.New(logger, "bot-context/", ragEmbedder)
	if err != nil {
		logger.Error("failed to create RAG logic", slog.String("err", err.Error()))

		return
	}

	hist := history.New(logger, "history-gemini/", history.Config{
		HistorySummary: historySummary,
		Summarizer: &genkitSummarizer{
			g:     g,
			model: model,
		},
	})

	aiLogic, err := gemini.New(logger, g, model, hist, mcpClients, ragL, searchEnable)

	if err != nil {
		logger.Error("failed to create AI logic", slog.String("err", err.Error()))

		return
	}
	srv := server.New(addr, aiLogic)

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, os.Interrupt, os.Kill)
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
