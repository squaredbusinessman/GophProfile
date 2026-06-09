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
	"github.com/squaredbusinessman/GophProfile/internal/httpapi"
	queuekafka "github.com/squaredbusinessman/GophProfile/internal/queue/kafka"
	"github.com/squaredbusinessman/GophProfile/internal/storage/postgres"
	storages3 "github.com/squaredbusinessman/GophProfile/internal/storage/s3"
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

	db, err := sql.Open("pgx", cfg.Postgres.DSN)
	if err != nil {
		logger.Fatal().Err(err).Msg("open postgres connection pool")
	}
	defer db.Close()

	s3Client, err := storages3.NewClient(cfg.S3)
	if err != nil {
		logger.Fatal().Err(err).Msg("create s3 client")
	}

	kafkaClient, err := queuekafka.NewClient(cfg.Kafka.Brokers, cfg.Kafka.ClientID, cfg.Kafka.ConsumerGroup)
	if err != nil {
		logger.Fatal().Err(err).Msg("create kafka client")
	}
	defer kafkaClient.Close()

	userRepo := postgres.NewUserRepository(db)
	avatarRepo := postgres.NewAvatarRepository(db)
	avatarUploadService := app.NewAvatarUploadService(userRepo, avatarRepo, s3Client, kafkaClient)

	router := httpapi.NewRouter(httpapi.RouterConfig{
		ServiceName:    cfg.ServiceName,
		Version:        cfg.Version,
		Logger:         logger,
		AvatarUploader: avatarUploadService,
	})

	if err := app.RunHTTPServer(ctx, cfg, router, logger); err != nil {
		if !errors.Is(err, context.Canceled) {
			logger.Fatal().Err(err).Msg("server stopped with error")
		}
	}
}
