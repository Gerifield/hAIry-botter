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

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"google.golang.org/genai"

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
		geminiModel = "gemini-flash-latest" // Always use the latest flash model by default
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

	// Initialize the AI logic
	aiClient, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		APIKey:  geminiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		logger.Error("failed to create genai client", slog.String("err", err.Error()))

		return
	}

	ragEmbedder := gemini_embedding.GeminiEmbeddingFunc(aiClient, "gemini-embedding-001")
	ragL, err := rag.New(logger, "bot-context/", ragEmbedder)
	if err != nil {
		logger.Error("failed to create RAG logic", slog.String("err", err.Error()))

		return
	}

	hist := history.New(logger, "history-gemini/", history.Config{
		HistorySummary:  historySummary,
		Summarizer:      aiClient,
		SummarizerModel: geminiModel,
	})

	aiLogic, err := gemini.New(logger, aiClient, geminiModel, hist, mcpClients, ragL, searchEnable)

	if err != nil {
		logger.Error("failed to create gemini logic", slog.String("err", err.Error()))

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
