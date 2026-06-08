package app

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"github.com/squaredbusinessman/GophProfile/internal/config"
)

// RunWorker запускает базовый worker и ожидает сигнал остановки
func RunWorker(ctx context.Context, cfg config.Config, logger zerolog.Logger) error {
	logger.Info().
		Strs("kafka_brokers", cfg.Kafka.Brokers).
		Str("consumer_group", cfg.Kafka.ConsumerGroup).
		Msg("worker started")

	<-ctx.Done()

	shutdownTimer := time.NewTimer(cfg.Worker.ShutdownTimeout)
	defer shutdownTimer.Stop()

	logger.Info().Dur("timeout", cfg.Worker.ShutdownTimeout).Msg("worker shutting down")

	select {
	case <-shutdownTimer.C:
		return nil
	default:
		return ctx.Err()
	}
}
