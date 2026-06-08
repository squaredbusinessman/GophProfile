package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/squaredbusinessman/GophProfile/internal/app"
	"github.com/squaredbusinessman/GophProfile/internal/config"
	"github.com/squaredbusinessman/GophProfile/internal/httpapi"
)

// main запускает HTTP-сервер приложения
func main() {
	cfg := config.Load()
	logger := app.NewLogger(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
