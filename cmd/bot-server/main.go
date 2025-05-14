package main

import (
	"context"
	"errors"
	"google.golang.org/genai"
	"hairy-botter/internal/ai/gemini"
	"hairy-botter/internal/server"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
)

func main() {

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

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

	// Initialize the AI logic
	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		APIKey:  geminiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		logger.Error("failed to create genai client", slog.String("err", err.Error()))

		return
	}

	aiLogic, err := gemini.New(logger, client, geminiModel, "history-gemini/")
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
