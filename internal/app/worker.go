package app

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"github.com/squaredbusinessman/GophProfile/internal/config"
	queuekafka "github.com/squaredbusinessman/GophProfile/internal/queue/kafka"
)

// RunWorker запускает worker и периодически публикует pending outbox события
func RunWorker(ctx context.Context, cfg config.Config, logger zerolog.Logger, outboxPublisher *OutboxPublisherService, processConsumer ProcessMessageConsumer, avatarProcessor *AvatarProcessService) error {
	logger.Info().
		Strs("kafka_brokers", cfg.Kafka.Brokers).
		Str("consumer_group", cfg.Kafka.ConsumerGroup).
		Msg("worker started")

	errCh := make(chan error, 1)
	if processConsumer != nil && avatarProcessor != nil {
		go func() {
			errCh <- consumeAvatarProcess(ctx, processConsumer, avatarProcessor)
		}()
	}

	ticker := time.NewTicker(cfg.Worker.OutboxPollInterval)
	defer ticker.Stop()

	publishPendingOutbox(ctx, cfg, logger, outboxPublisher)

	for {
		select {
		case err := <-errCh:
			return err
		case <-ctx.Done():
			return shutdownWorker(ctx, cfg, logger)
		case <-ticker.C:
			publishPendingOutbox(ctx, cfg, logger, outboxPublisher)
		}
	}
}

type ProcessMessageConsumer interface {
	Consume(ctx context.Context, topics []string, handler func(context.Context, queuekafka.Message) error) error
}

// consumeAvatarProcess читает avatar.process topics и запускает обработчик
func consumeAvatarProcess(ctx context.Context, consumer ProcessMessageConsumer, processor *AvatarProcessService) error {
	topics := []string{
		queuekafka.TopicAvatarProcess,
		queuekafka.TopicAvatarProcessRetry1m,
		queuekafka.TopicAvatarProcessRetry5m,
		queuekafka.TopicAvatarProcessRetry30m,
	}
	return consumer.Consume(ctx, topics, func(ctx context.Context, message queuekafka.Message) error {
		return processor.HandleProcessMessage(ctx, message.Value)
	})
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
