package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/squaredbusinessman/GophProfile/internal/app"
	"github.com/squaredbusinessman/GophProfile/internal/config"
)

// main запускает worker приложения
func main() {
	cfg := config.Load()
	logger := app.NewLogger(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.RunWorker(ctx, cfg, logger); err != nil {
		if !errors.Is(err, context.Canceled) {
			logger.Fatal().Err(err).Msg("worker stopped with error")
		}
	}
}
