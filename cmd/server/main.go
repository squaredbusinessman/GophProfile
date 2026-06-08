package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/squaredbusinessman/GophProfile/internal/app"
	"github.com/squaredbusinessman/GophProfile/internal/httpapi"
)

// main запускает HTTP-сервер приложения
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := app.LoadConfig(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	logger := app.NewLogger(cfg)

	router := httpapi.NewRouter(httpapi.RouterConfig{
		ServiceName: cfg.ServiceName,
		Version:     cfg.Version,
		Logger:      logger,
	})

	if err := app.RunHTTPServer(ctx, cfg, router, logger); err != nil {
		if !errors.Is(err, context.Canceled) {
			logger.Fatal().Err(err).Msg("server stopped with error")
		}
	}
}
