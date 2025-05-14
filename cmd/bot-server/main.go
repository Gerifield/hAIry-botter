package main

import (
	"context"
	"errors"
	"hairy-botter/internal/logic"
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

	l := logic.New(logger)
	srv := server.New(addr, l)

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, os.Interrupt, os.Kill)
	go func() {
		<-stopCh
		err := srv.Stop(context.Background())
		if err != nil {
			logger.Error("failed to stop server", slog.String("err", err.Error()))
		}
	}()

	if err := srv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server failed", slog.String("err", err.Error()))
	}
}
