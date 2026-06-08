package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/squaredbusinessman/GophProfile/internal/app"
)

// main запускает worker приложения
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := app.LoadConfig(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	logger := app.NewLogger(cfg)

	if err := app.RunWorker(ctx, cfg, logger); err != nil {
		if !errors.Is(err, context.Canceled) {
			logger.Fatal().Err(err).Msg("worker stopped with error")
		}
	}
}
