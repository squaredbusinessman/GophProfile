package app

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"github.com/squaredbusinessman/GophProfile/internal/config"
)

// RunWorker запускает worker и периодически публикует pending outbox события
func RunWorker(ctx context.Context, cfg config.Config, logger zerolog.Logger, outboxPublisher *OutboxPublisherService) error {
	logger.Info().
		Strs("kafka_brokers", cfg.Kafka.Brokers).
		Str("consumer_group", cfg.Kafka.ConsumerGroup).
		Msg("worker started")

	ticker := time.NewTicker(cfg.Worker.OutboxPollInterval)
	defer ticker.Stop()

	publishPendingOutbox(ctx, cfg, logger, outboxPublisher)

	for {
		select {
		case <-ctx.Done():
			return shutdownWorker(ctx, cfg, logger)
		case <-ticker.C:
			publishPendingOutbox(ctx, cfg, logger, outboxPublisher)
		}
	}
}

// shutdownWorker выполняет graceful shutdown worker
func shutdownWorker(ctx context.Context, cfg config.Config, logger zerolog.Logger) error {
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

// publishPendingOutbox публикует pending outbox события если publisher настроен
func publishPendingOutbox(ctx context.Context, cfg config.Config, logger zerolog.Logger, outboxPublisher *OutboxPublisherService) {
	if outboxPublisher == nil {
		return
	}

	published, err := outboxPublisher.PublishPending(ctx, cfg.Worker.OutboxBatchSize)
	if err != nil {
		logger.Error().Err(err).Msg("outbox publish failed")
		return
	}
	if published > 0 {
		logger.Info().Int("published", published).Msg("outbox events published")
	}
}
