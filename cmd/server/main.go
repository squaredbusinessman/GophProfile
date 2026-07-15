// Package main запускает HTTP-сервер приложения
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
	"github.com/squaredbusinessman/GophProfile/internal/observability"
	queuekafka "github.com/squaredbusinessman/GophProfile/internal/queue/kafka"
	"github.com/squaredbusinessman/GophProfile/internal/resilience"
	"github.com/squaredbusinessman/GophProfile/internal/storage/postgres"
	storages3 "github.com/squaredbusinessman/GophProfile/internal/storage/s3"
)

// main запускает HTTP-сервер приложения
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	cfg, err := app.LoadConfigForProcess(ctx, "server")
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		stop()
		os.Exit(1)
	}
	defer stop()

	logger := app.NewLogger(cfg)
	ctx = app.ContextWithLogger(ctx, logger)
	breakerCfg := resilience.CircuitBreakerConfig{
		Enabled:          cfg.CircuitBreaker.Enabled,
		FailureThreshold: cfg.CircuitBreaker.FailureThreshold,
		OpenTimeout:      cfg.CircuitBreaker.OpenTimeout,
	}
	telemetry, err := observability.NewTelemetry(ctx, cfg)
	if err != nil {
		logger.Fatal().Str("error_type", app.ErrorType(err)).Msg("initialize telemetry")
	}
	if err := telemetry.StartMetricsServer(cfg.Observability.MetricsAddr); err != nil {
		logger.Fatal().Str("error_type", app.ErrorType(err)).Msg("start metrics server")
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
		defer cancel()
		if err := telemetry.Shutdown(shutdownCtx); err != nil {
			logger.Error().Str("error_type", app.ErrorType(err)).Msg("shutdown telemetry")
		}
	}()
	logger.Info().
		Bool("otel_enabled", cfg.Observability.Enabled).
		Str("otel_service", cfg.Observability.ServiceName).
		Str("metrics_addr", cfg.Observability.MetricsAddr).
		Dur("telemetry_shutdown_timeout", cfg.HTTP.ShutdownTimeout).
		Msg("telemetry initialized")

	defaultAvatar, err := httpapi.LoadDefaultAvatar(cfg.HTTP.DefaultAvatarPath)
	if err != nil {
		logger.Fatal().Str("error_type", app.ErrorType(err)).Str("path", cfg.HTTP.DefaultAvatarPath).Msg("load default avatar")
	}

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

	s3Client, err := storages3.NewClient(cfg.S3, breakerCfg)
	if err != nil {
		logger.Fatal().Str("error_type", app.ErrorType(err)).Msg("create s3 client")
	}
	if err := app.EnsureLocalS3Bucket(ctx, cfg, s3Client); err != nil {
		logger.Fatal().Str("error_type", app.ErrorType(err)).Msg("ensure local s3 bucket")
	}

	kafkaClient, err := queuekafka.NewClient(cfg.Kafka.Brokers, cfg.Kafka.ClientID, cfg.Kafka.ConsumerGroup, breakerCfg)
	if err != nil {
		logger.Fatal().Str("error_type", app.ErrorType(err)).Msg("create kafka client")
	}
	defer func() {
		if err := kafkaClient.Close(); err != nil {
			logger.Error().Str("error_type", app.ErrorType(err)).Msg("close kafka client")
		}
	}()

	userRepo, err := postgres.NewUserRepository(db, breakerCfg)
	if err != nil {
		logger.Fatal().Str("error_type", app.ErrorType(err)).Msg("create user repository")
	}
	outboxRepo, err := postgres.NewOutboxRepository(db, breakerCfg)
	if err != nil {
		logger.Fatal().Str("error_type", app.ErrorType(err)).Msg("create outbox repository")
	}
	avatarRepo, err := postgres.NewAvatarRepository(db, breakerCfg)
	if err != nil {
		logger.Fatal().Str("error_type", app.ErrorType(err)).Msg("create avatar repository")
	}
	if err := telemetry.RegisterBusinessMetrics(outboxRepo, avatarRepo); err != nil {
		logger.Fatal().Str("error_type", app.ErrorType(err)).Msg("register business metrics")
	}
	userResolveService := app.NewUserResolveService(userRepo)
	avatarUploadService, err := app.NewAvatarUploadService(userRepo, outboxRepo, s3Client, kafkaClient)
	if err != nil {
		logger.Fatal().Str("error_type", app.ErrorType(err)).Msg("create avatar upload service")
	}
	avatarReadService := app.NewAvatarReadServiceWithUsers(userRepo, avatarRepo, s3Client)
	avatarDeleteService, err := app.NewAvatarDeleteService(avatarRepo, outboxRepo, kafkaClient)
	if err != nil {
		logger.Fatal().Str("error_type", app.ErrorType(err)).Msg("create avatar delete service")
	}

	router, err := httpapi.NewRouter(httpapi.RouterConfig{
		ServiceName:    cfg.ServiceName,
		Version:        cfg.Version,
		Logger:         logger,
		AllowedOrigins: cfg.HTTP.CORSAllowedOrigins,
		RateLimitRPS:   cfg.HTTP.RateLimitRPS,
		RateLimitBurst: cfg.HTTP.RateLimitBurst,
		DefaultAvatar:  defaultAvatar,
		HealthChecks: map[string]httpapi.HealthCheck{
			"postgres": db.PingContext,
			"s3":       s3Client.HealthCheck,
			"kafka":    kafkaClient.HealthCheck,
		},
		UserResolver:   userResolveService,
		AvatarUploader: avatarUploadService,
		AvatarReader:   avatarReadService,
		AvatarDeleter:  avatarDeleteService,
	})
	if err != nil {
		logger.Fatal().Str("error_type", app.ErrorType(err)).Msg("create HTTP router")
	}

	if err := app.RunHTTPServer(ctx, cfg, router, logger); err != nil {
		if !errors.Is(err, context.Canceled) {
			logger.Fatal().Str("error_type", app.ErrorType(err)).Msg("server stopped with error")
		}
	}
}
