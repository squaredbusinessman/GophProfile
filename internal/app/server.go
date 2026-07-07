package app

import (
	"context"
	"errors"
	"net/http"

	"github.com/rs/zerolog"
	"github.com/squaredbusinessman/GophProfile/internal/config"
)

// RunHTTPServer запускает HTTP-сервер и корректно останавливает его по сигналу
func RunHTTPServer(ctx context.Context, cfg config.Config, handler http.Handler, logger zerolog.Logger) error {
	ctx = ContextWithLogger(ctx, logger)
	server := &http.Server{
		Addr:         cfg.HTTP.Addr,
		Handler:      handler,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
		IdleTimeout:  cfg.HTTP.IdleTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		LoggerFromContext(ctx).Info().Str("addr", cfg.HTTP.Addr).Msg("http server started")
		err := server.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
	defer cancel()

	LoggerFromContext(ctx).Info().Dur("timeout", cfg.HTTP.ShutdownTimeout).Msg("http server shutting down")
	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}

	return <-errCh
}
