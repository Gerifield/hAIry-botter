package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"google.golang.org/genai"

	"hairy-botter/internal/ai/gemini"
	gemini_embedding "hairy-botter/internal/ai/gemini-embedding"
	"hairy-botter/internal/rag"
	"hairy-botter/internal/server"
)

func main() {

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug, // TODO: set to configurable level
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
		geminiModel = "gemini-2.5-flash-preview-04-17" // For now
	}

	mcpClients := make([]*client.Client, 0)
	mcpServer := os.Getenv("MCP_SSE_SERVERS")
	if mcpServer != "" {
		// Parse the MCP server list
		servers := strings.Split(mcpServer, ",")
		for _, s := range servers {
			s = strings.TrimSpace(s)

			logger.Info("init SSE MCP server", slog.String("server", mcpServer))
			sseTransport, err := transport.NewSSE(mcpServer, transport.WithHeaderFunc(func(ctx context.Context) map[string]string {
				res := make(map[string]string)
				if u := ctx.Value("x-session-id"); u != nil {
					res["x-session-id"] = u.(string)
				}

				return res
			}))
			if err != nil {
				logger.Error("failed to create SSE transport", slog.String("err", err.Error()))

				return
			}

			if err = sseTransport.Start(context.Background()); err != nil {
				logger.Error("failed to start SSE transport", slog.String("err", err.Error()))

				return
			}

			mcpClients = append(mcpClients, client.NewClient(sseTransport))
		}
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

	ragEmbedder := gemini_embedding.GeminiEmbeddingFunc(aiClient, "text-embedding-004")
	ragL, err := rag.New(logger, "bot-context/", ragEmbedder)
	if err != nil {
		logger.Error("failed to create RAG logic", slog.String("err", err.Error()))

		return
	}

	aiLogic, err := gemini.New(logger, aiClient, geminiModel, "history-gemini/", mcpClients, ragL)
	if err != nil {
		logger.Error("failed to create gemini logic", slog.String("err", err.Error()))

		return
	}
	srv := server.New(addr, aiLogic)

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, os.Interrupt, os.Kill)
	go func() {
		<-stopCh
		err := srv.Stop(context.Background())
		if err != nil {
			logger.Error("failed to stop server", slog.String("err", err.Error()))
		}
	}()

	logger.Info("starting server", slog.String("addr", addr))
	if err := srv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server failed", slog.String("err", err.Error()))
	}
}
