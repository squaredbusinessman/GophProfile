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
	queuekafka "github.com/squaredbusinessman/GophProfile/internal/queue/kafka"
	"github.com/squaredbusinessman/GophProfile/internal/storage/postgres"
	storages3 "github.com/squaredbusinessman/GophProfile/internal/storage/s3"
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

	db, err := sql.Open("pgx", cfg.Postgres.DSN)
	if err != nil {
		logger.Fatal().Err(err).Msg("open postgres connection pool")
	}
	defer db.Close()

	kafkaClient, err := queuekafka.NewClient(cfg.Kafka.Brokers, cfg.Kafka.ClientID, cfg.Kafka.ConsumerGroup)
	if err != nil {
		logger.Fatal().Err(err).Msg("create kafka client")
	}
	defer kafkaClient.Close()

	s3Client, err := storages3.NewClient(cfg.S3)
	if err != nil {
		logger.Fatal().Err(err).Msg("create s3 client")
	}
	if err := app.EnsureLocalS3Bucket(ctx, cfg, s3Client); err != nil {
		logger.Fatal().Err(err).Msg("ensure local s3 bucket")
	}

	avatarRepo := postgres.NewAvatarRepository(db)
	outboxRepo := postgres.NewOutboxRepository(db)
	outboxPublisher := app.NewOutboxPublisherService(outboxRepo, kafkaClient)
	avatarProcessor := app.NewAvatarProcessService(avatarRepo, s3Client, kafkaClient)
	avatarDeleter := app.NewAvatarDeleteWorkerService(avatarRepo, s3Client)

	if err := app.RunWorker(ctx, cfg, logger, outboxPublisher, kafkaClient, avatarProcessor, avatarDeleter); err != nil {
		if !errors.Is(err, context.Canceled) {
			logger.Fatal().Err(err).Msg("worker stopped with error")
		}
	}
}
