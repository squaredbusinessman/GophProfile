// Package main запускает фоновый обработчик приложения
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/squaredbusinessman/GophProfile/internal/app"
	"github.com/squaredbusinessman/GophProfile/internal/observability"
	queuekafka "github.com/squaredbusinessman/GophProfile/internal/queue/kafka"
	"github.com/squaredbusinessman/GophProfile/internal/storage/postgres"
	storages3 "github.com/squaredbusinessman/GophProfile/internal/storage/s3"
)

// main запускает worker приложения
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	cfg, err := app.LoadConfigForProcess(ctx, "worker")
	if err != nil {
		if _, writeErr := fmt.Fprintf(os.Stderr, "load config: %v\n", err); writeErr != nil {
			stop()
			os.Exit(1)
		}
		stop()
		os.Exit(1)
	}
	defer stop()

	logger := app.NewLogger(cfg)
	ctx = app.ContextWithLogger(ctx, logger)
	telemetry, err := observability.NewTelemetry(ctx, cfg)
	if err != nil {
		logger.Fatal().Str("error_type", app.ErrorType(err)).Msg("initialize telemetry")
	}
	if err := telemetry.StartMetricsServer(cfg.Observability.MetricsAddr); err != nil {
		logger.Fatal().Str("error_type", app.ErrorType(err)).Msg("start metrics server")
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Worker.ShutdownTimeout)
		defer cancel()
		if err := telemetry.Shutdown(shutdownCtx); err != nil {
			logger.Error().Str("error_type", app.ErrorType(err)).Msg("shutdown telemetry")
		}
	}()
	logger.Info().
		Bool("otel_enabled", cfg.Observability.Enabled).
		Str("otel_service", cfg.Observability.ServiceName).
		Str("metrics_addr", cfg.Observability.MetricsAddr).
		Msg("telemetry initialized")

	db, err := sql.Open("pgx", cfg.Postgres.DSN)
	if err != nil {
		logger.Fatal().Str("error_type", app.ErrorType(err)).Msg("open postgres connection pool")
	}
	if err := telemetry.RegisterDBPool(db, "postgres"); err != nil {
		logger.Fatal().Str("error_type", app.ErrorType(err)).Msg("register postgres pool metrics")
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Error().Str("error_type", app.ErrorType(err)).Msg("close postgres connection pool")
		}
	}()

	kafkaClient, err := queuekafka.NewClient(cfg.Kafka.Brokers, cfg.Kafka.ClientID, cfg.Kafka.ConsumerGroup)
	if err != nil {
		logger.Fatal().Str("error_type", app.ErrorType(err)).Msg("create kafka client")
	}
	defer func() {
		if err := kafkaClient.Close(); err != nil {
			logger.Error().Str("error_type", app.ErrorType(err)).Msg("close kafka client")
		}
	}()

	s3Client, err := storages3.NewClient(cfg.S3)
	if err != nil {
		logger.Fatal().Str("error_type", app.ErrorType(err)).Msg("create s3 client")
	}
	if err := app.EnsureLocalS3Bucket(ctx, cfg, s3Client); err != nil {
		logger.Fatal().Str("error_type", app.ErrorType(err)).Msg("ensure local s3 bucket")
	}

	avatarRepo, err := postgres.NewAvatarRepository(db)
	if err != nil {
		logger.Fatal().Str("error_type", app.ErrorType(err)).Msg("create avatar repository")
	}
	outboxRepo, err := postgres.NewOutboxRepository(db)
	if err != nil {
		logger.Fatal().Str("error_type", app.ErrorType(err)).Msg("create outbox repository")
	}
	if err := telemetry.RegisterBusinessMetrics(outboxRepo, avatarRepo); err != nil {
		logger.Fatal().Str("error_type", app.ErrorType(err)).Msg("register business metrics")
	}
	outboxPublisher, err := app.NewOutboxPublisherService(outboxRepo, kafkaClient)
	if err != nil {
		logger.Fatal().Str("error_type", app.ErrorType(err)).Msg("create outbox publisher")
	}
	avatarProcessor, err := app.NewAvatarProcessService(avatarRepo, s3Client, kafkaClient)
	if err != nil {
		logger.Fatal().Str("error_type", app.ErrorType(err)).Msg("create avatar processor")
	}
	avatarDeleter, err := app.NewAvatarDeleteWorkerService(avatarRepo, s3Client)
	if err != nil {
		logger.Fatal().Str("error_type", app.ErrorType(err)).Msg("create avatar deleter")
	}

	if err := app.RunWorker(ctx, cfg, logger, outboxPublisher, kafkaClient, avatarProcessor, avatarDeleter); err != nil {
		if !errors.Is(err, context.Canceled) {
			logger.Fatal().Str("error_type", app.ErrorType(err)).Msg("worker stopped with error")
		}
	}
}
